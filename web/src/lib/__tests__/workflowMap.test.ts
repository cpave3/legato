import { describe, expect, it } from "vitest"
import { edgeLabel, mapTags, nodeStatus, related, splitStandalone, tagView, triggerMarkers, type WorkflowMap, type WorkflowMapNode } from "../workflowMap"

const node = (id: string, more: Partial<WorkflowMapNode> = {}): WorkflowMapNode => ({ id, name: id, title: id, ...more })
const edge = (source: string, target: string, more = {}) => ({ id: `${source}->${target}`, source, target, calls: 1, steps: ["s"], ...more })

// ship calls review-loop; deliver calls ship; alone stands alone.
const map: WorkflowMap = {
  nodes: [
    node("deliver", { tags: ["delivery"] }),
    node("ship", { tags: ["delivery", "reviews"] }),
    node("review-loop", { built_in: true, tags: ["reviews"] }),
    node("alone"),
    node("other", { tags: ["ops"] }),
    node("helper"),
  ],
  edges: [edge("deliver", "ship"), edge("ship", "review-loop"), edge("other", "helper")],
}

describe("the workflow map's model", () => {
  it("splits the workflows with calls from those that stand alone", () => {
    const { linked, standalone } = splitStandalone(map)
    expect(linked.map(n => n.id)).toEqual(["deliver", "ship", "review-loop", "other", "helper"])
    expect(standalone.map(n => n.id)).toEqual(["alone"])
  })

  it("lights a workflow's whole chain of callers and callees", () => {
    expect([...related(map, "ship")!].sort()).toEqual(["deliver", "review-loop", "ship"])
    expect([...related(map, "deliver")!].sort()).toEqual(["deliver", "review-loop", "ship"])
    expect([...related(map, "alone")!]).toEqual(["alone"])
    expect(related(map, null)).toBeNull()
  })

  it("lists every tag once, sorted", () => {
    expect(mapTags(map)).toEqual(["delivery", "ops", "reviews"])
  })

  it("keeps a tag's workflows, and those they call or are called by as context", () => {
    const view = tagView(map, "ops")
    expect(view.map.nodes.map(n => n.id)).toEqual(["other", "helper"])
    expect([...view.context]).toEqual(["helper"])
    expect(view.map.edges.map(e => e.id)).toEqual(["other->helper"])
    const all = tagView(map, "")
    expect(all.map).toBe(map)
    expect(all.context.size).toBe(0)
  })

  it("labels an edge that calls more than once, in a loop, or as one of an input's choices", () => {
    expect(edgeLabel(edge("a", "b"))).toBe("")
    expect(edgeLabel(edge("a", "b", { calls: 2 }))).toBe("2 calls")
    expect(edgeLabel(edge("a", "b", { in_loop: true }))).toBe("in a loop")
    expect(edgeLabel(edge("a", "b", { calls: 3, in_loop: true, dynamic: true }))).toBe("3 calls · in a loop · one choice")
  })

  it("marks each kind of trigger once, and a workflow with none and no callers as run by hand", () => {
    const triggered = node("t", { triggers: [
      { trigger: "schedule", enabled: true, file_enabled: true, overridden: false },
      { trigger: "chat.message", enabled: false, file_enabled: true, overridden: true },
      { trigger: "chat.mention", enabled: true, file_enabled: true, overridden: false },
      { trigger: "task.entered_column", enabled: false, file_enabled: false, overridden: false },
    ] })
    expect(triggerMarkers(triggered, false).map(m => [m.kind, m.enabled])).toEqual([["schedule", true], ["chat", true], ["column", false]])
    expect(triggerMarkers(node("m"), false).map(m => m.kind)).toEqual(["manual"])
    expect(triggerMarkers(node("c"), true)).toEqual([])
  })

  it("says whether a workflow's triggers are on, paused, or mixed", () => {
    const state = (enabled: boolean) => ({ trigger: "schedule", enabled, file_enabled: true, overridden: false })
    expect(nodeStatus(node("a"))).toBe("none")
    expect(nodeStatus(node("a", { triggers: [state(true)] }))).toBe("on")
    expect(nodeStatus(node("a", { triggers: [state(false)] }))).toBe("paused")
    expect(nodeStatus(node("a", { triggers: [state(true), { ...state(false), trigger: "chat.message" }] }))).toBe("mixed")
  })
})
