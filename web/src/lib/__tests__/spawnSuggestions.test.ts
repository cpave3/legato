import { describe, expect, it } from "vitest"
import {
  canSavePath,
  pathVisibility,
  whereName,
  directoryStart,
  initialPathField,
  listFrameStyle,
  listPlacement,
  suggestionMarks,
  noSuggestions,
  pathFieldReducer,
  pathKeyAction,
  presetsHiddenOnPhone,
  stepCursor,
  suggestionCursor,
  suggestionView,
} from "../spawnSuggestions"
import type { DirectorySuggestions } from "../spawnDirectories"

const item = (name: string) => ({ name, path: `/home/me/Dev/${name}`, display: `~/Dev/${name}/`, git: false, preset: 0 })
const inside: DirectorySuggestions = {
  query: "~/Dev/", resolved: "/home/me/Dev", exists: true, base: "~/Dev/", prefix: "", total: 3, items: [item("alpha"), item("beta")],
}

describe("directoryStart", () => {
  const listed = {
    presets: [{ name: "legato", path: "/home/me/legato", display: "~/legato", saved: true }],
    default: { path: "/home/me/legato", display: "~/legato" },
  }

  it("starts at the server's default, a preset, so the phone shows the presets", () => {
    expect(directoryStart(listed, undefined, "")).toEqual({ presets: listed.presets, path: "~/legato", serverKnowsDirectories: true, showAll: false })
  })

  it("starts at the folder given, showing a custom path when it is no preset", () => {
    expect(directoryStart(listed, undefined, "/srv/x")).toMatchObject({ path: "/srv/x", showAll: true })
  })

  it("falls back on the configured repositories with a folder, and the working directory", () => {
    const start = directoryStart(null, { working_dir: "/work", repositories: [{ name: "A", primary_dir: "/a" }, { name: "B", primary_dir: "" }] }, "")
    expect(start).toEqual({ presets: [{ name: "A", path: "/a", display: "/a", saved: false }], path: "/work", serverKnowsDirectories: false, showAll: true })
  })

  it("has nothing to start with when nothing is known", () => {
    expect(directoryStart(null, undefined, "")).toEqual({ presets: [], path: "", serverKnowsDirectories: false, showAll: true })
  })
})

describe("suggestionView", () => {
  it("lists nothing while the list is shut", () => {
    expect(suggestionView(inside, "~/Dev/", false)).toMatchObject({ shown: [], takeable: [], more: true, shownKey: "\n+" })
  })

  it("lists the answer for the path, and says when there are more", () => {
    const view = suggestionView(inside, "~/Dev/", true)
    expect(view.shown).toHaveLength(2)
    expect(view.takeable).toHaveLength(2)
    expect(view.more).toBe(true)
    expect(view.shownKey).toBe("/home/me/Dev/alpha\n/home/me/Dev/beta\n+")
  })

  it("holds the last answer out of reach for another directory", () => {
    const view = suggestionView(inside, "~/Other/", true)
    expect(view).toMatchObject({ held: true, takeable: [], more: false })
    expect(view.shown).toHaveLength(2)
  })

  it("narrows the answer as a name is typed", () => {
    const view = suggestionView(inside, "~/Dev/b", true)
    expect(view.shown.map((shown) => shown.name)).toEqual(["beta"])
    expect(view.prefix).toBe("b")
    expect(view.more).toBe(false)
  })

  it("lists nothing before an answer", () => {
    expect(suggestionView(null, "~/Dev/", true)).toMatchObject({ shown: [], more: false, shownKey: "" })
  })
})

describe("suggestionCursor", () => {
  const takeable = [item("alpha"), item("beta")]
  it("is on nothing inside a directory until chosen", () => {
    expect(suggestionCursor("~/Dev/", takeable, 0, false)).toEqual({ current: -1, ghost: "" })
    expect(suggestionCursor("~/Dev/", takeable, 1, true)).toEqual({ current: 1, ghost: "beta/" })
  })
  it("is on the first under a typed name, completing it", () => {
    expect(suggestionCursor("~/Dev/al", takeable, 0, false)).toEqual({ current: 0, ghost: "pha/" })
  })
  it("stays on the list", () => {
    expect(suggestionCursor("~/Dev/x", takeable, 9, false).current).toBe(1)
    expect(suggestionCursor("~/Dev/x", takeable, -3, false).current).toBe(0)
  })
  it("is on nothing with nothing to take", () => {
    expect(suggestionCursor("~/Dev/x", [], 0, true)).toEqual({ current: -1, ghost: "" })
  })
})

describe("pathKeyAction", () => {
  const key = (k: string, mods: Partial<{ shiftKey: boolean; metaKey: boolean; ctrlKey: boolean }> = {}) => ({ key: k, shiftKey: false, metaKey: false, ctrlKey: false, ...mods })
  it("closes a list on escape, and leaves escape alone without one", () => {
    expect(pathKeyAction(key("Escape"), true, false, false)).toBe("close")
    expect(pathKeyAction(key("Escape"), false, false, false)).toBeNull()
  })
  it("does nothing else without suggestions to take", () => {
    expect(pathKeyAction(key("ArrowDown"), true, false, false)).toBeNull()
  })
  it("moves with the arrows and completes with tab, but not shift+tab", () => {
    expect(pathKeyAction(key("ArrowDown"), true, true, false)).toBe("down")
    expect(pathKeyAction(key("ArrowUp"), true, true, false)).toBe("up")
    expect(pathKeyAction(key("Tab"), true, true, false)).toBe("complete")
    expect(pathKeyAction(key("Tab", { shiftKey: true }), true, true, false)).toBeNull()
  })
  it("takes on enter only what the arrows chose, and never on the spawn chord", () => {
    expect(pathKeyAction(key("Enter"), true, true, true)).toBe("take")
    expect(pathKeyAction(key("Enter"), true, true, false)).toBeNull()
    expect(pathKeyAction(key("Enter", { metaKey: true }), true, true, true)).toBeNull()
    expect(pathKeyAction(key("Enter", { ctrlKey: true }), true, true, true)).toBeNull()
    expect(pathKeyAction(key("a"), true, true, true)).toBeNull()
  })
})

describe("pathFieldReducer", () => {
  const answered = pathFieldReducer({ ...initialPathField, active: 2, chosen: true }, { type: "answer", found: inside })
  it("starts over on a new answer", () => {
    expect(answered).toEqual({ found: inside, listOpen: false, active: 0, chosen: false })
  })
  it("opens the list and starts over on an edit", () => {
    expect(pathFieldReducer({ ...answered, active: 1, chosen: true }, { type: "edit" })).toEqual({ found: inside, listOpen: true, active: 0, chosen: false })
  })
  it("marks a move as chosen", () => {
    expect(pathFieldReducer(answered, { type: "move", to: 1 })).toMatchObject({ active: 1, chosen: true })
  })
  it("opens, closes and forgets the answer on a reset", () => {
    const opened = pathFieldReducer(answered, { type: "open" })
    expect(opened.listOpen).toBe(true)
    expect(pathFieldReducer(opened, { type: "close" }).listOpen).toBe(false)
    expect(pathFieldReducer(opened, { type: "reset" })).toMatchObject({ found: null, listOpen: false })
  })
})

describe("the rest", () => {
  it("steps along the list without leaving it", () => {
    expect(stepCursor(-1, 1, 3)).toBe(0)
    expect(stepCursor(2, 1, 3)).toBe(2)
    expect(stepCursor(0, -1, 3)).toBe(0)
  })

  it("has an empty answer for a path", () => {
    expect(noSuggestions("~/x")).toEqual({ query: "~/x", resolved: "", exists: false, base: "", prefix: "", items: [], total: 0 })
  })

  it("places the list under the field, or over it when there is more room there", () => {
    expect(listPlacement({ left: 10, width: 200, top: 100, bottom: 140 }, 0, 800, 800)).toEqual({ left: 10, width: 200, top: 146, room: 264 })
    expect(listPlacement({ left: 10, width: 200, top: 600, bottom: 640 }, 0, 700, 800)).toEqual({ left: 10, width: 200, bottom: 206, room: 264 })
    expect(listPlacement({ left: 0, width: 1, top: 10, bottom: 690 }, 0, 700, 700).room).toBe(0)
    // The visual viewport is what a phone's keyboard leaves of the window.
    expect(listPlacement({ left: 0, width: 1, top: 300, bottom: 340 }, 100, 300, 800)).toEqual({ left: 0, width: 1, bottom: 506, room: 186 })
  })

  it("sizes the list's frame to what it lists, in the room it has", () => {
    expect(listFrameStyle(null, false, 100)).toEqual({ position: "fixed", left: 0, width: 0, top: undefined, bottom: undefined, height: 0 })
    expect(listFrameStyle({ left: 4, width: 9, top: 20, room: 50 }, true, 100)).toEqual({ position: "fixed", left: 4, width: 9, top: 20, bottom: undefined, height: 52 })
    expect(listFrameStyle({ left: 4, width: 9, bottom: 20, room: 264 }, true, 100).height).toBe(102)
  })

  it("marks a suggestion that is a repository or a preset", () => {
    expect(suggestionMarks(item("a"))).toBe("")
    expect(suggestionMarks({ ...item("a"), git: true, preset: 1 })).toBe("git · preset")
    expect(suggestionMarks({ ...item("a"), preset: 2 })).toBe("preset")
  })

  it("hides the path's parts in a clone, and on a phone until asked for", () => {
    expect(pathVisibility(true, true, "flex")).toBe("hidden")
    expect(pathVisibility(false, true, "flex")).toBe("flex")
    expect(pathVisibility(false, false, "block")).toBe("hidden sm:block")
  })

  it("saves only a typed path that is no preset, on a server that keeps them", () => {
    const preset = { name: "a", path: "/a", display: "~/a", saved: true }
    expect(canSavePath(false, true, "~/b", undefined)).toBe(true)
    expect(canSavePath(true, true, "~/b", undefined)).toBe(false)
    expect(canSavePath(false, false, "~/b", undefined)).toBe(false)
    expect(canSavePath(false, true, "  ", undefined)).toBe(false)
    expect(canSavePath(false, true, "~/a", preset)).toBe(false)
  })

  it("names where the agent starts", () => {
    const preset = { name: "legato", path: "/l", display: "~/l", saved: true }
    expect(whereName(true, "Legato", preset, "~/l")).toBe("Legato")
    expect(whereName(true, undefined, preset, "~/l")).toBe("")
    expect(whereName(false, "Legato", preset, "~/l")).toBe("legato")
    expect(whereName(false, undefined, undefined, "/srv/work/thing")).toBe("thing")
  })

  it("counts the presets a phone keeps back, never the one picked", () => {
    const presets = ["a", "b", "c", "d", "e"].map((name) => ({ name, path: name, display: name, saved: false }))
    expect(presetsHiddenOnPhone(presets, undefined, false)).toBe(2)
    expect(presetsHiddenOnPhone(presets, presets[4], false)).toBe(1)
    expect(presetsHiddenOnPhone(presets, undefined, true)).toBe(0)
  })
})
