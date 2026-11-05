import { describe, expect, it } from "vitest"
import { DEFAULT_CONFIG, hostPermissionFor, normalizeConfig } from "./config"

describe("normalizeConfig", () => {
  it("normalizes a complete configurable review action", () => {
    expect(normalizeConfig({
      ...DEFAULT_CONFIG,
      baseUrl: "http://localhost:3080/",
      token: " secret ",
    })).toEqual({
      ...DEFAULT_CONFIG,
      baseUrl: "http://localhost:3080",
      token: "secret",
    })
  })

  it.each(["", "localhost:3080", "file:///tmp/legato", "https://user@example.com"])(
    "rejects invalid Legato URL %j",
    (baseUrl) => expect(() => normalizeConfig({ ...DEFAULT_CONFIG, baseUrl })).toThrow(),
  )

  it("rejects URL paths and missing mapping names", () => {
    expect(() => normalizeConfig({ ...DEFAULT_CONFIG, baseUrl: "https://legato.test/app" })).toThrow("origin")
    expect(() => normalizeConfig({ ...DEFAULT_CONFIG, workflowName: " " })).toThrow("Review workflow name")
    expect(() => normalizeConfig({ ...DEFAULT_CONFIG, discussWorkflowName: " " })).toThrow("Discussion workflow name")
  })
})

describe("hostPermissionFor", () => {
  it("derives one exact-origin match pattern", () => {
    expect(hostPermissionFor("https://legato.example:3443")).toBe("https://legato.example:3443/*")
  })
})

describe("openImportsInDesktop", () => {
  it("is off unless chosen, and survives normalizing", () => {
    expect(DEFAULT_CONFIG.openImportsInDesktop).toBe(false)
    expect(normalizeConfig({ ...DEFAULT_CONFIG, openImportsInDesktop: true }).openImportsInDesktop).toBe(true)
    expect(normalizeConfig({ ...DEFAULT_CONFIG, openImportsInDesktop: "yes" as unknown as boolean }).openImportsInDesktop).toBe(false)
  })
})
