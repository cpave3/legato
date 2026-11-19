import type { Scripting } from "./ticket-sites"
import { hostPatternFor, webOrigin } from "./ticket-providers"

// The sites the person has let Scope onto, one origin at a time from the
// toolbar button. Kept in chrome.storage.local; the Scope content script is
// registered on each one Chrome still grants, as ticket-sites.ts does for
// ticket providers.
export const SCOPE_ORIGINS_KEY = "scopeOrigins"
export const SCOPE_SCRIPT_PREFIX = "legato-scope-"

export type ScopeOrigin = { origin: string; allowed: boolean }

export type ScopeOriginDeps = {
  load: () => Promise<unknown>
  save: (origins: string[]) => Promise<void>
  hasPermission: (origin: string) => Promise<boolean>
  scripting: Scripting
}

type Script = chrome.scripting.RegisteredContentScript

// The origin of an http(s) page, or null for any other page (chrome://,
// file://, the extension's own pages).
export function scopeOriginOf(url: string | undefined): string | null {
  if (!url) return null
  let parsed: URL
  try { parsed = new URL(url) } catch { return null }
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") return null
  return parsed.origin
}

// The stored list, without anything that is not exactly an origin.
export function normalizeScopeOrigins(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  const origins: string[] = []
  for (const entry of value) {
    const origin = webOrigin(entry)
    if (origin && origin === entry && !origins.includes(origin)) origins.push(origin)
  }
  return origins
}

function scriptFor(origin: string): Script {
  return {
    id: `${SCOPE_SCRIPT_PREFIX}${origin}`,
    matches: [hostPatternFor(origin)],
    js: ["scope.js"],
    runAt: "document_idle",
    persistAcrossSessions: true,
  }
}

async function reconcile(scripting: Scripting, origins: string[]): Promise<void> {
  const wanted = new Map(origins.map((origin) => [`${SCOPE_SCRIPT_PREFIX}${origin}`, scriptFor(origin)]))
  const registered = (await scripting.getRegisteredContentScripts()).filter((script) => script.id.startsWith(SCOPE_SCRIPT_PREFIX))
  const known = new Set(registered.map((script) => script.id))
  const stale = registered.filter((script) => !wanted.has(script.id)).map((script) => script.id)
  const added = [...wanted.values()].filter((script) => !known.has(script.id))
  if (stale.length) await scripting.unregisterContentScripts({ ids: stale })
  if (added.length) await scripting.registerContentScripts(added)
}

// Changes run one at a time: two at once would both read the old list.
export function createScopeOrigins(deps: ScopeOriginDeps) {
  let queue: Promise<unknown> = Promise.resolve()
  const serial = <T>(work: () => Promise<T>): Promise<T> => {
    const next = queue.then(work)
    queue = next.catch(() => undefined)
    return next
  }
  const load = async (): Promise<string[]> => normalizeScopeOrigins(await deps.load())
  const sync = async (origins: string[]): Promise<ScopeOrigin[]> => {
    const listed = await Promise.all(origins.map(async (origin) => ({ origin, allowed: await deps.hasPermission(origin) })))
    await reconcile(deps.scripting, listed.filter((entry) => entry.allowed).map((entry) => entry.origin))
    return listed
  }
  return {
    // The stored origins, with the scripts brought in line with them.
    list: (): Promise<ScopeOrigin[]> => serial(async () => sync(await load())),
    add: (origin: string): Promise<ScopeOrigin[]> => serial(async () => {
      const origins = await load()
      if (!origins.includes(origin)) {
        origins.push(origin)
        await deps.save(origins)
      }
      return sync(origins)
    }),
    remove: (origin: string): Promise<ScopeOrigin[]> => serial(async () => {
      const origins = (await load()).filter((entry) => entry !== origin)
      await deps.save(origins)
      return sync(origins)
    }),
    isAllowed: async (origin: string): Promise<boolean> => (await load()).includes(origin) && deps.hasPermission(origin),
  }
}

export const scopeOrigins = createScopeOrigins({
  load: async () => (await chrome.storage.local.get(SCOPE_ORIGINS_KEY))[SCOPE_ORIGINS_KEY],
  save: (origins) => chrome.storage.local.set({ [SCOPE_ORIGINS_KEY]: origins }),
  hasPermission: (origin) => chrome.permissions.contains({ origins: [hostPatternFor(origin)] }),
  get scripting() { return chrome.scripting },
})
