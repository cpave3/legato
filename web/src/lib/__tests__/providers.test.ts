import { describe, expect, it } from "vitest"
import { providerLabel, sortProviders } from "../providers"

describe("providerLabel", () => {
  it("names the providers Legato knows, and others by their own name", () => {
    expect(providerLabel("jira")).toBe("Jira")
    expect(providerLabel("burnlite")).toBe("Burnlite")
    expect(providerLabel("github")).toBe("GitHub")
    // A manual sync on a main also pulls its workers.
    expect(providerLabel("workers")).toBe("Workers")
    expect(providerLabel("linear")).toBe("linear")
    expect(providerLabel("")).toBe("")
  })
})

describe("sortProviders", () => {
  it("puts the known providers first in a fixed order, then the rest by name", () => {
    expect(sortProviders(["linear", "github", "asana", "burnlite", "jira"])).toEqual(["jira", "burnlite", "github", "asana", "linear"])
  })
})
