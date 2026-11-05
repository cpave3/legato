import { LegatoClient } from "./legato"
import { hasOriginPermission } from "./permissions"
import { loadConfig } from "./storage"
import { hostPatternFor, normalizeTicketProviders, TICKET_PROVIDERS_KEY, type TicketProvider } from "./ticket-providers"

// A provider's site and whether the person has let the extension onto it.
export type TicketSite = TicketProvider & { allowed: boolean }
export type TicketSitesResult = { sites: TicketSite[]; error?: string }

type Script = chrome.scripting.RegisteredContentScript

// The part of chrome.scripting a sync uses.
type Scripting = {
  getRegisteredContentScripts: () => Promise<Script[]>
  registerContentScripts: (scripts: Script[]) => Promise<void>
  updateContentScripts: (scripts: Script[]) => Promise<void>
  unregisterContentScripts: (filter: { ids: string[] }) => Promise<void>
}

export type TicketSiteDeps = {
  fetchProviders: () => Promise<TicketProvider[]>
  loadProviders: () => Promise<TicketProvider[]>
  saveProviders: (providers: TicketProvider[]) => Promise<void>
  hasPermission: (origin: string) => Promise<boolean>
  scripting: Scripting
}

const SCRIPT_PREFIX = "legato-tickets-"

function scriptFor(provider: TicketProvider): Script {
  return {
    id: `${SCRIPT_PREFIX}${provider.name}`,
    matches: [hostPatternFor(provider.web_origin)],
    js: ["tickets.js"],
    runAt: "document_idle",
    persistAcrossSessions: true,
  }
}

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

// Brings the ticket content script's registrations in line with the providers
// Legato reports: one script for each provider whose site the person has
// allowed, none for the rest. Without refresh, the stored list is used, as
// when a permission changes. When Legato cannot be asked, the stored list
// stands and the result says why.
//
// Syncs run one at a time: two at once would both register a new script.
export function createTicketSiteSync(deps: TicketSiteDeps) {
  let queue: Promise<unknown> = Promise.resolve()
  const run = async (refresh: boolean): Promise<TicketSitesResult> => {
    let error: string | undefined
    let providers = normalizeTicketProviders(await deps.loadProviders())
    if (refresh) {
      try {
        providers = normalizeTicketProviders(await deps.fetchProviders())
        await deps.saveProviders(providers)
      } catch (failure) {
        error = errorText(failure)
      }
    }
    const sites = await Promise.all(providers.map(async (provider) => ({
      ...provider,
      allowed: await deps.hasPermission(provider.web_origin),
    })))
    try {
      await reconcileScripts(deps.scripting, sites.filter((site) => site.allowed))
    } catch (failure) {
      error ??= errorText(failure)
    }
    return error ? { sites, error } : { sites }
  }
  return (options: { refresh?: boolean } = {}): Promise<TicketSitesResult> => {
    const next = queue.then(() => run(options.refresh ?? true))
    queue = next.catch(() => undefined)
    return next
  }
}

async function reconcileScripts(scripting: Scripting, providers: TicketProvider[]): Promise<void> {
  const wanted = new Map(providers.map((provider) => [`${SCRIPT_PREFIX}${provider.name}`, scriptFor(provider)]))
  const registered = (await scripting.getRegisteredContentScripts()).filter((script) => script.id.startsWith(SCRIPT_PREFIX))
  const stale = registered.filter((script) => !wanted.has(script.id)).map((script) => script.id)
  const moved = registered.filter((script) => {
    const want = wanted.get(script.id)
    return want && (script.matches?.length !== 1 || script.matches[0] !== want.matches![0])
  }).map((script) => wanted.get(script.id)!)
  const known = new Set(registered.map((script) => script.id))
  const added = [...wanted.values()].filter((script) => !known.has(script.id))
  if (stale.length) await scripting.unregisterContentScripts({ ids: stale })
  if (moved.length) await scripting.updateContentScripts(moved)
  if (added.length) await scripting.registerContentScripts(added)
}

async function fetchProviders(): Promise<TicketProvider[]> {
  const config = await loadConfig()
  if (!(await hasOriginPermission(config.baseUrl))) {
    throw new Error("Open extension options and grant access to the configured Legato origin")
  }
  return new LegatoClient(config.baseUrl, config.token).listTicketProviders()
}

export const syncTicketSites = createTicketSiteSync({
  fetchProviders,
  loadProviders: async () => normalizeTicketProviders((await chrome.storage.local.get(TICKET_PROVIDERS_KEY))[TICKET_PROVIDERS_KEY]),
  saveProviders: (providers) => chrome.storage.local.set({ [TICKET_PROVIDERS_KEY]: providers }),
  hasPermission: (origin) => chrome.permissions.contains({ origins: [hostPatternFor(origin)] }),
  get scripting() { return chrome.scripting },
})
