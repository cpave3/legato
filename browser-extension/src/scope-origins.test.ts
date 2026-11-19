import { describe, expect, it, vi } from "vitest"
import { createScopeOrigins, scopeOriginOf, type ScopeOriginDeps } from "./scope-origins"

type Registered = chrome.scripting.RegisteredContentScript

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

function setup(options: { stored?: string[]; allowed?: string[]; registered?: Registered[] } = {}) {
  let stored = options.stored ?? []
  const allowed = new Set(options.allowed ?? [])
  const scripting = fakeScripting(options.registered)
  const deps: ScopeOriginDeps = {
    load: vi.fn(async () => stored),
    save: vi.fn(async (origins: string[]) => { stored = origins }),
    hasPermission: vi.fn(async (origin: string) => allowed.has(origin)),
    scripting,
  }
  return { deps, scripting, allowed, stored: () => stored }
}

const script = (origin: string): Registered => ({
  id: `legato-scope-${origin}`,
  matches: [`${origin}/*`],
  js: ["scope.js"],
  runAt: "document_idle",
  persistAcrossSessions: true,
})

describe("scopeOriginOf", () => {
  it("gives the origin of an http(s) page and nothing for other pages", () => {
    expect(scopeOriginOf("http://localhost:5173/settings?tab=2#x")).toBe("http://localhost:5173")
    expect(scopeOriginOf("https://app.example.com/")).toBe("https://app.example.com")
    expect(scopeOriginOf("chrome://extensions")).toBeNull()
    expect(scopeOriginOf("file:///tmp/a.html")).toBeNull()
    expect(scopeOriginOf(undefined)).toBeNull()
    expect(scopeOriginOf("not a url")).toBeNull()
  })
})

describe("Scope origins", () => {
  it("adds an origin, stores it and registers the Scope script there", async () => {
    const { deps, scripting, stored } = setup({ allowed: ["http://localhost:5173"] })
    const listed = await createScopeOrigins(deps).add("http://localhost:5173")
    expect(stored()).toEqual(["http://localhost:5173"])
    expect(listed).toEqual([{ origin: "http://localhost:5173", allowed: true }])
    expect([...scripting.scripts.values()]).toEqual([script("http://localhost:5173")])
  })

  it("keeps several origins, one script each, and adding one twice changes nothing", async () => {
    const { deps, scripting, stored } = setup({ allowed: ["http://localhost:5173", "https://staging.test"] })
    const origins = createScopeOrigins(deps)
    await origins.add("http://localhost:5173")
    await origins.add("https://staging.test")
    await origins.add("http://localhost:5173")
    expect(stored()).toEqual(["http://localhost:5173", "https://staging.test"])
    expect([...scripting.scripts.keys()]).toEqual(["legato-scope-http://localhost:5173", "legato-scope-https://staging.test"])
  })

  it("registers no script where Chrome's access is gone, and lists the origin as not allowed", async () => {
    const { deps, scripting } = setup({ stored: ["https://gone.test"], allowed: [], registered: [script("https://gone.test")] })
    const listed = await createScopeOrigins(deps).list()
    expect(listed).toEqual([{ origin: "https://gone.test", allowed: false }])
    expect(scripting.scripts.size).toBe(0)
  })

  it("removes an origin and its script, leaving other scripts alone", async () => {
    const unrelated: Registered = { id: "legato-tickets-jira", matches: ["https://x.atlassian.net/*"], js: ["tickets.js"] }
    const { deps, scripting, stored } = setup({
      stored: ["http://localhost:5173", "https://staging.test"],
      allowed: ["http://localhost:5173", "https://staging.test"],
      registered: [script("http://localhost:5173"), script("https://staging.test"), unrelated],
    })
    const listed = await createScopeOrigins(deps).remove("http://localhost:5173")
    expect(stored()).toEqual(["https://staging.test"])
    expect(listed).toEqual([{ origin: "https://staging.test", allowed: true }])
    expect([...scripting.scripts.keys()].sort()).toEqual(["legato-scope-https://staging.test", "legato-tickets-jira"])
  })

  it("says whether an origin is allowed: listed and still granted by Chrome", async () => {
    const { deps } = setup({ stored: ["http://localhost:5173", "https://gone.test"], allowed: ["http://localhost:5173", "https://other.test"] })
    const origins = createScopeOrigins(deps)
    expect(await origins.isAllowed("http://localhost:5173")).toBe(true)
    expect(await origins.isAllowed("https://gone.test")).toBe(false)
    expect(await origins.isAllowed("https://other.test")).toBe(false)
  })

  it("drops stored values that are not origins", async () => {
    const { deps } = setup({ stored: ["http://localhost:5173/path", "javascript:alert(1)", 4 as unknown as string, "http://localhost:5173"], allowed: ["http://localhost:5173"] })
    expect(await createScopeOrigins(deps).list()).toEqual([{ origin: "http://localhost:5173", allowed: true }])
  })

  it("runs one change at a time, so two adds at once keep both origins", async () => {
    const { deps, scripting, stored } = setup({ allowed: ["http://a.test", "http://b.test"] })
    const origins = createScopeOrigins(deps)
    await Promise.all([origins.add("http://a.test"), origins.add("http://b.test")])
    expect(stored()).toEqual(["http://a.test", "http://b.test"])
    expect(scripting.scripts.size).toBe(2)
  })
})
