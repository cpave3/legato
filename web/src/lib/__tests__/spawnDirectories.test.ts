import { describe, expect, it } from "vitest"
import { completionOf, directoryName, presetFor, suggestionParts, suggestionsWhile, type DirectoryPreset, type DirectorySuggestions } from "../spawnDirectories"

const presets: DirectoryPreset[] = [
  { name: "autopilot", path: "/home/me/Development/autopilot", display: "~/Development/autopilot", saved: false },
  { name: "srv", path: "/srv/app", display: "/srv/app", saved: true },
]

describe("presetFor", () => {
  it("finds the preset a typed path names, with or without a tilde or a trailing slash", () => {
    expect(presetFor("~/Development/autopilot", presets)?.name).toBe("autopilot")
    expect(presetFor("~/Development/autopilot/", presets)?.name).toBe("autopilot")
    expect(presetFor("/home/me/Development/autopilot", presets)?.name).toBe("autopilot")
    expect(presetFor(" /srv/app ", presets)?.name).toBe("srv")
  })

  it("finds nothing for another directory or for no path", () => {
    expect(presetFor("~/Development/auto", presets)).toBeUndefined()
    expect(presetFor("", presets)).toBeUndefined()
  })
})

describe("completionOf", () => {
  const suggestion = { name: "Projects", path: "/home/me/Projects", display: "~/Projects/", git: false, preset: 0 }

  it("is what a suggestion adds to the typed path", () => {
    expect(completionOf("~/Pro", suggestion)).toBe("jects/")
    expect(completionOf("~/pro", suggestion)).toBe("jects/")
  })

  it("is empty when the suggestion does not continue what was typed", () => {
    expect(completionOf("~/Projects/", suggestion)).toBe("")
    expect(completionOf("/elsewhere", suggestion)).toBe("")
    expect(completionOf("~/Pro", undefined)).toBe("")
  })
})

describe("suggestionParts", () => {
  const found: DirectorySuggestions = {
    query: "~/Development/auto", resolved: "", exists: false, base: "~/Development/", prefix: "auto", total: 1,
    items: [{ name: "Autopilot", path: "/home/me/Development/Autopilot", display: "~/Development/Autopilot/", git: true, preset: 1 }],
  }

  it("splits a suggestion into its directory, what was typed of its name, and the rest", () => {
    expect(suggestionParts(found.base, found.prefix, found.items[0])).toEqual({ base: "~/Development/", typed: "Auto", rest: "pilot/" })
    expect(suggestionParts(found.base, "other", found.items[0])).toEqual({ base: "~/Development/", typed: "", rest: "Autopilot/" })
    expect(suggestionParts("~/elsewhere/", "auto", found.items[0])).toEqual({ base: "", typed: "", rest: "~/Development/Autopilot/" })
  })
})

describe("directoryName", () => {
  it("is the last part of a path", () => {
    expect(directoryName("~/Development/autopilot/")).toBe("autopilot")
    expect(directoryName("/srv/app")).toBe("app")
    expect(directoryName("~")).toBe("~")
    expect(directoryName(" ")).toBe("")
  })
})

describe("suggestionsWhile", () => {
  const item = (name: string) => ({ name, path: `/home/me/dev/${name}`, display: `~/dev/${name}/`, git: false, preset: 0 })
  const found: DirectorySuggestions = {
    query: "~/dev/auto", resolved: "", exists: false, base: "~/dev/", prefix: "auto", total: 2,
    items: [item("autopilot"), item("automation")],
  }

  it("gives the answer as it is for the path it was asked for", () => {
    expect(suggestionsWhile(found, "~/dev/auto")).toEqual({ items: found.items, held: false, prefix: "auto" })
  })

  it("narrows the last answer to what a longer path still matches", () => {
    expect(suggestionsWhile(found, "~/dev/AUTOP")).toEqual({ items: [found.items[0]], held: false, prefix: "AUTOP" })
  })

  it("holds the last answer when none of it matches, so the list does not come and go", () => {
    expect(suggestionsWhile(found, "~/other/")).toEqual({ items: found.items, held: true, prefix: "auto" })
    // A directory typed in full is not a completion of itself.
    expect(suggestionsWhile(found, "~/dev/autopilot/")).toEqual({ items: found.items, held: true, prefix: "auto" })
  })

  it("has nothing before the first answer", () => {
    expect(suggestionsWhile(null, "~/dev/auto")).toEqual({ items: [], held: false, prefix: "" })
  })
})
