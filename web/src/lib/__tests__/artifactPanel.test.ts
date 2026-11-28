import { describe, expect, it } from "vitest"
import type { AgentUIItem } from "../agentUi"
import { activeTabOf, artifactFileName, artifactLabel, artifactMediaType, groupArtifacts, nextRevision, pickArtifact, pinnedRevision, revisionWindow } from "../artifactPanel"

const base = { task_id: "t", created_at: "", updated_at: "", resource_domains: [], revision: 1, oldest_revision: 1 }
const panel = (id: string, extra: Partial<AgentUIItem> = {}): AgentUIItem => ({ ...base, id, placement: "panel", format: "markdown", title: id, ...extra })
const card = (id: string): AgentUIItem => ({ ...base, id, placement: "inline", format: "components", title: id })
const studio = (id: string): AgentUIItem => ({ ...base, id, placement: "panel", format: "studio", title: id })
const page = (path: string) => ({ path, folder: "f", label: "Overview", version: "v1" })

describe("artifactPanel", () => {
  it("names a saved file after the item's title and format", () => {
    expect(artifactFileName(panel("x", { title: " Rough / plan! ", format: "html" }))).toBe("Rough-plan.html")
    expect(artifactFileName(panel("ui-1", { title: "", format: "video" }))).toBe("ui-1.manifest.json")
    expect(artifactFileName(panel("ui-2", { title: "///" }))).toBe("ui-2.md")
    expect(artifactMediaType(panel("x", { format: "markdown" }))).toBe("text/markdown")
  })

  it("labels an untitled item by where it was shown", () => {
    expect(artifactLabel(panel("x", { title: "" }))).toBe("Artifact")
    expect(artifactLabel({ ...card("x"), title: "" })).toBe("Card")
  })

  it("puts artifacts before cards and keeps studios apart", () => {
    const groups = groupArtifacts([card("c1"), panel("a1"), studio("s1"), panel("a2"), studio("s2")])
    expect(groups.ordered.map((item) => item.id)).toEqual(["a1", "a2", "c1"])
    expect(groups.cards.map((item) => item.id)).toEqual(["c1"])
    expect(groups.newestStudio?.id).toBe("s2")
  })

  it("picks the focused item, else the newest artifact, card or studio", () => {
    const items = [card("c1"), panel("a1"), panel("a2"), studio("s1")]
    const groups = groupArtifacts(items)
    expect(pickArtifact(items, groups, [], { itemId: "c1", revision: 0 }).selected?.id).toBe("c1")
    expect(pickArtifact(items, groups, [], null).selected?.id).toBe("a2")
    const cardsOnly = [card("c1"), card("c2")]
    expect(pickArtifact(cardsOnly, groupArtifacts(cardsOnly), [], null).selected?.id).toBe("c2")
    expect(pickArtifact([studio("s1")], groupArtifacts([studio("s1")]), [], null).selected?.id).toBe("s1")
    expect(pickArtifact([], groupArtifacts([]), [], null)).toEqual({ selected: undefined })
  })

  it("picks a briefing page when it is focused, or when there is no artifact to show", () => {
    const pages = [page(".review/a.html"), page(".review/b.html")]
    const items = [panel("a1")]
    expect(pickArtifact(items, groupArtifacts(items), pages, { itemId: "review:.review/b.html", revision: 0 })).toEqual({ briefingPage: pages[1] })
    expect(pickArtifact(items, groupArtifacts(items), pages, null).selected?.id).toBe("a1")
    const cards = [card("c1")]
    expect(pickArtifact(cards, groupArtifacts(cards), pages, null)).toEqual({ briefingPage: pages[0] })
    expect(pickArtifact(cards, groupArtifacts(cards), pages, { itemId: "c1", revision: 0 }).selected?.id).toBe("c1")
  })

  it("places a revision among those kept", () => {
    const item = panel("a", { revision: 5, oldest_revision: 4 })
    expect(revisionWindow(item, null)).toEqual({ revision: 5, latest: 5, oldest: 4, available: 2 })
    expect(revisionWindow(item, { ...item, revision: 4 }).revision).toBe(4)
    expect(nextRevision(3, 5)).toBe(4)
    expect(nextRevision(4, 5)).toBe(0)
  })

  it("reads the focused revision only of the item in focus", () => {
    const item = panel("a")
    expect(pinnedRevision(item, { itemId: "a", revision: 3 })).toBe(3)
    expect(pinnedRevision(item, { itemId: "b", revision: 3 })).toBe(0)
    expect(pinnedRevision(undefined, { itemId: "a", revision: 3 })).toBe(0)
    expect(pinnedRevision(item, null)).toBe(0)
  })

  it("names the tab in front", () => {
    expect(activeTabOf({ briefingPage: page(".review/a.html") })).toBe("review:.review/a.html")
    expect(activeTabOf({ selected: panel("a") })).toBe("a")
    expect(activeTabOf({})).toBeUndefined()
  })
})
