import { describe, expect, it } from "vitest"
import { providerItem, providerLabel, sortProviders } from "../providers"

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

  it("names a worker's copy of a handed-off task by the provider it came from", () => {
    expect(providerLabel("jira-copy")).toBe("Jira")
    expect(providerLabel("burnlite-copy")).toBe("Burnlite")
    expect(providerLabel("linear-copy")).toBe("linear")
    expect(providerItem("burnlite-copy")).toBe("task")
    expect(providerItem("jira-copy")).toBe("issue")
  })
})

describe("sortProviders", () => {
  it("puts the known providers first in a fixed order, then the rest by name", () => {
    expect(sortProviders(["linear", "github", "asana", "burnlite", "jira"])).toEqual(["jira", "burnlite", "github", "asana", "linear"])
  })
})
