import { describe, expect, it, vi } from "vitest"
import type { TicketProvider } from "./ticket-providers"
import { createTicketSiteSync, type TicketSiteDeps } from "./ticket-sites"

type Registered = chrome.scripting.RegisteredContentScript

const KEY_PATTERN = "^[A-Z][A-Z0-9_]*-[0-9]+$"
const jira: TicketProvider = { name: "jira", label: "Jira", web_origin: "https://example.atlassian.net", key_pattern: KEY_PATTERN }
const burnlite: TicketProvider = { name: "burnlite", label: "Burnlite", web_origin: "https://burnlite.test:8443", key_pattern: KEY_PATTERN }

// Chrome's registry of dynamic content scripts, in memory.
function fakeScripting(initial: Registered[] = []) {
  const scripts = new Map(initial.map((script) => [script.id, script]))
  return {
    scripts,
    getRegisteredContentScripts: vi.fn(async () => [...scripts.values()]),
    registerContentScripts: vi.fn(async (add: Registered[]) => {
      for (const script of add) {
        if (scripts.has(script.id)) throw new Error(`Duplicate script ID '${script.id}'`)
        scripts.set(script.id, script)
      }
    }),
    updateContentScripts: vi.fn(async (changes: Registered[]) => {
      for (const change of changes) scripts.set(change.id, { ...scripts.get(change.id)!, ...change })
    }),
    unregisterContentScripts: vi.fn(async (filter?: { ids?: string[] }) => {
      for (const id of filter?.ids ?? []) scripts.delete(id)
    }),
  }
}

function setup(options: { fetched?: TicketProvider[] | Error; stored?: TicketProvider[]; allowed?: string[]; registered?: Registered[] } = {}) {
  let stored = options.stored ?? []
  const scripting = fakeScripting(options.registered)
  const deps: TicketSiteDeps = {
    fetchProviders: vi.fn(async () => {
      if (options.fetched instanceof Error) throw options.fetched
      return options.fetched ?? []
    }),
    loadProviders: vi.fn(async () => stored),
    saveProviders: vi.fn(async (providers: TicketProvider[]) => { stored = providers }),
    hasPermission: vi.fn(async (origin: string) => (options.allowed ?? []).includes(origin)),
    scripting,
  }
  return { deps, scripting, stored: () => stored }
}

const script = (name: string, origin: string): Registered => ({
  id: `legato-tickets-${name}`,
  matches: [`${origin}/*`],
  js: ["tickets.js"],
  runAt: "document_idle",
  persistAcrossSessions: true,
})

describe("syncing ticket sites", () => {
  it("stores the providers Legato reports and registers the script on each permitted site", async () => {
    const { deps, scripting, stored } = setup({ fetched: [jira, burnlite], allowed: ["https://burnlite.test:8443"] })
    const result = await createTicketSiteSync(deps)()

    expect(stored()).toEqual([jira, burnlite])
    expect(result).toEqual({ sites: [{ ...jira, allowed: false }, { ...burnlite, allowed: true }] })
    expect(deps.hasPermission).toHaveBeenCalledWith("https://burnlite.test:8443")
    expect([...scripting.scripts.values()]).toEqual([script("burnlite", "https://burnlite.test:8443")])
  })

  it("moves a registered script when its provider's site changes", async () => {
    const { deps, scripting } = setup({
      fetched: [{ ...jira, web_origin: "https://new.atlassian.net" }],
      allowed: ["https://new.atlassian.net"],
      registered: [script("jira", "https://old.atlassian.net")],
    })
    await createTicketSiteSync(deps)()
    expect(scripting.updateContentScripts).toHaveBeenCalledOnce()
    expect(scripting.registerContentScripts).not.toHaveBeenCalled()
    expect(scripting.scripts.get("legato-tickets-jira")?.matches).toEqual(["https://new.atlassian.net/*"])
  })

  it("leaves a script that is already right alone", async () => {
    const { deps, scripting } = setup({ fetched: [jira], allowed: [jira.web_origin], registered: [script("jira", jira.web_origin)] })
    await createTicketSiteSync(deps)()
    expect(scripting.registerContentScripts).not.toHaveBeenCalled()
    expect(scripting.updateContentScripts).not.toHaveBeenCalled()
    expect(scripting.unregisterContentScripts).not.toHaveBeenCalled()
  })

  it("unregisters scripts for providers no longer listed or no longer permitted, and leaves other scripts alone", async () => {
    const unrelated: Registered = { id: "something-else", matches: ["https://github.com/*"], js: ["github.js"] }
    const { deps, scripting } = setup({
      fetched: [jira],
      allowed: [],
      registered: [script("jira", jira.web_origin), script("burnlite", burnlite.web_origin), unrelated],
    })
    await createTicketSiteSync(deps)()
    expect([...scripting.scripts.keys()]).toEqual(["something-else"])
  })

  it("skips providers it cannot read keys for and sites that are not web origins", async () => {
    const linear = { ...jira, name: "linear", label: "Linear", web_origin: "https://linear.app" }
    const bad = { ...burnlite, web_origin: "javascript:alert(1)" }
    const { deps, scripting } = setup({ fetched: [linear, bad, jira], allowed: ["https://linear.app", jira.web_origin] })
    const result = await createTicketSiteSync(deps)()
    expect(result.sites.map((site) => site.name)).toEqual(["jira"])
    expect([...scripting.scripts.keys()]).toEqual(["legato-tickets-jira"])
  })

  it("keeps the stored providers and reports why when Legato cannot be asked", async () => {
    const { deps, scripting, stored } = setup({
      fetched: new Error("Could not reach Legato"),
      stored: [burnlite],
      allowed: [burnlite.web_origin],
    })
    const result = await createTicketSiteSync(deps)()
    expect(result).toEqual({ sites: [{ ...burnlite, allowed: true }], error: "Could not reach Legato" })
    expect(stored()).toEqual([burnlite])
    expect([...scripting.scripts.keys()]).toEqual(["legato-tickets-burnlite"])
  })

  it("re-registers from the stored providers without asking Legato when told not to refresh", async () => {
    const { deps, scripting } = setup({ stored: [burnlite], allowed: [burnlite.web_origin] })
    await createTicketSiteSync(deps)({ refresh: false })
    expect(deps.fetchProviders).not.toHaveBeenCalled()
    expect([...scripting.scripts.keys()]).toEqual(["legato-tickets-burnlite"])
  })

  it("runs one sync at a time, so two at once do not register the same script twice", async () => {
    const { deps, scripting } = setup({ fetched: [burnlite], allowed: [burnlite.web_origin] })
    const sync = createTicketSiteSync(deps)
    const results = await Promise.all([sync(), sync({ refresh: false })])
    expect(results.every((result) => !result.error)).toBe(true)
    expect(scripting.registerContentScripts).toHaveBeenCalledOnce()
  })

  it("reports a registration failure without throwing", async () => {
    const { deps, scripting } = setup({ fetched: [burnlite], allowed: [burnlite.web_origin] })
    scripting.registerContentScripts.mockRejectedValueOnce(new Error("Invalid match pattern"))
    const result = await createTicketSiteSync(deps)()
    expect(result.error).toBe("Invalid match pattern")
  })
})
