import { describe, expect, it } from "vitest"
import { NEVER_CACHED, SERVER_PAGES } from "../serverPages"

const servedByServer = (path: string) => SERVER_PAGES.some((pattern) => pattern.test(path))

describe("SERVER_PAGES", () => {
  it("sends the OAuth callback to the server, not the cached app", () => {
    expect(servedByServer("/oauth/callback?code=c&state=s")).toBe(true)
  })

  it("keeps the images agents and documents show", () => {
    for (const path of ["/agent-ui-images/t1/a.png", "/plan-images/p/a.png", "/review-images/r/a.png"]) {
      expect(servedByServer(path)).toBe(true)
    }
  })

  it("leaves the app's own pages to the app", () => {
    for (const path of ["/", "/settings", "/settings?integration=burnlite&result=connected", "/board", "/oauthish"]) {
      expect(servedByServer(path)).toBe(false)
    }
  })
})

describe("NEVER_CACHED", () => {
  const neverCached = (href: string) => NEVER_CACHED.some((pattern) => pattern.test(href))

  it("keeps authorization and MCP connection answers out of the service worker's caches", () => {
    for (const href of [
      "https://legato.local:3080/api/mcp/oauth/attempts/att-1",
      "https://legato.local:3080/api/mcp/connections/notion/oauth/begin",
      "https://legato.local:3080/api/settings/mcp/connections",
      "https://legato.local:3080/api/settings/integrations/burnlite",
    ]) {
      expect(neverCached(href)).toBe(true)
    }
  })

  it("leaves other API reads to the cache", () => {
    expect(neverCached("https://legato.local:3080/api/board")).toBe(false)
    expect(neverCached("https://legato.local:3080/api/agents")).toBe(false)
  })
})
