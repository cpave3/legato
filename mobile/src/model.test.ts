import { allowNavigation, bootstrapScript } from "./compat"
import { normalizeServerUrl, parsePairUri } from "./model"

test("normalizes HTTP and HTTPS origins", () => {
  expect(normalizeServerUrl(" https://desktop.example/ ")).toBe("https://desktop.example")
  expect(normalizeServerUrl(" http://192.168.1.20:3080/ ")).toBe("http://192.168.1.20:3080")
  expect(() => normalizeServerUrl("https://desktop.example/path")).toThrow()
})

test("parses encoded Legato pairing URIs", () => {
  expect(parsePairUri("legato://pair?url=https%3A%2F%2Fmac.example%3A3080&token=a%20b")).toEqual({
    baseUrl: "https://mac.example:3080", token: "a b", suggestedName: "mac.example", caCertificate: undefined,
  })
  expect(() => parsePairUri("https://pair?url=https://x&token=y")).toThrow()
  expect(() => parsePairUri("legato://pair?url=https://x&url=https://y&token=z")).toThrow()
})

test("builds escaped bootstrap and contains navigation", () => {
  const script = bootstrapScript("https://mac.example", `a"b`)
  expect(script).toContain("legato:token:local")
  expect(script).toContain("legato:token:https://mac.example")
  expect(script).toContain(`a\\\"b`)
  expect(allowNavigation("https://mac.example", "https://mac.example/board")).toBe(true)
  expect(allowNavigation("https://mac.example", "https://evil.example")).toBe(false)
})
