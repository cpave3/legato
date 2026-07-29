import { describe, expect, it } from "vitest"
import { createNativeSyncState, reconcileNativeSnapshot, reduceNativeEvent } from "../nativeAgentSync"

describe("native agent synchronization", () => {
  it("keeps interleaved assistant streams and tools in first-seen order", () => {
    let state = createNativeSyncState()
    state = reduceNativeEvent(state, { eventId: "1", type: "assistant_text_delta", id: "a", delta: "Alpha" })
    state = reduceNativeEvent(state, { eventId: "2", type: "tool_call_start", callId: "call-1", name: "read", args: {} })
    state = reduceNativeEvent(state, { eventId: "3", type: "assistant_text_delta", id: "b", delta: "Beta" })
    state = reduceNativeEvent(state, { eventId: "4", type: "assistant_text_delta", id: "a", delta: " continued" })

    expect(state.items.map((item) => item.tool?.toolName ?? item.text)).toEqual([
      "Alpha continued",
      "read",
      "Beta",
    ])
  })

  it("deduplicates only snapshot overlap and preserves later repeated replies", () => {
    let state = reconcileNativeSnapshot({
      messages: [
        { role: "user", content: "system" },
        { role: "assistant", content: [{ type: "text", text: "Same reply" }] },
      ],
      toolCalls: [],
    }, createNativeSyncState(), [
      { eventId: "overlap", type: "assistant_text_done", id: "a", text: "Same reply" },
    ])
    state = reduceNativeEvent(state, { eventId: "later", type: "assistant_text_done", id: "b", text: "Same reply" })

    expect(state.items.filter((item) => item.text === "Same reply")).toHaveLength(2)
  })

  it("attaches skill activation metadata to the most recent read tool", () => {
    let state = createNativeSyncState()
    state = reduceNativeEvent(state, {
      eventId: "read-1",
      type: "tool_call_start",
      callId: "call-1",
      name: "read",
      args: { path: "/skills/coworker-walkthrough/SKILL.md" },
    })
    state = reduceNativeEvent(state, {
      eventId: "skill-1",
      type: "skill_activated",
      skillName: "coworker-walkthrough",
      source: "user",
    })

    expect(state.items[0]?.tool).toMatchObject({
      skillName: "coworker-walkthrough",
      skillSource: "user",
    })
  })

  it("preserves skill metadata when an authoritative snapshot replaces live items", () => {
    let state = createNativeSyncState()
    state = reduceNativeEvent(state, {
      eventId: "read-1",
      type: "tool_call_start",
      callId: "call-1",
      name: "read",
      args: { path: "/skills/coworker-walkthrough/SKILL.md" },
    })
    state = reduceNativeEvent(state, {
      eventId: "skill-1",
      type: "skill_activated",
      skillName: "coworker-walkthrough",
      source: "user",
    })

    state = reconcileNativeSnapshot({
      messages: [
        { role: "user", content: "system" },
        { role: "assistant", content: [{ type: "tool-call" }] },
      ],
      toolCalls: [{
        callId: "call-1",
        name: "read",
        args: { path: "/skills/coworker-walkthrough/SKILL.md" },
      }],
    }, state, [])

    expect(state.items[0]?.tool?.skillName).toBe("coworker-walkthrough")
  })

  it("applies duplicate event and tool call IDs exactly once", () => {
    let state = createNativeSyncState()
    const message = { eventId: "1", type: "assistant_text_done", id: "a", text: "Done" }
    const tool = { eventId: "2", type: "tool_call_start", callId: "call-1", name: "write", args: {} }

    state = reduceNativeEvent(state, message)
    state = reduceNativeEvent(state, message)
    state = reduceNativeEvent(state, tool)
    state = reduceNativeEvent(state, { ...tool, eventId: "3" })

    expect(state.items).toHaveLength(2)
    expect(state.cursor).toBe("3")
  })
})
