import { describe, expect, it } from "vitest"
import { SERVER_PAGES } from "../serverPages"

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
