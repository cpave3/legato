import { describe, expect, it } from "vitest"
import {
  createNativeSyncState,
  reconcileNativeSnapshot,
  reduceNativeEvent,
  stateFromNativeSnapshot,
  type NativeEvent,
  type NativeSyncState,
} from "../nativeAgentSync"

const reduce = (state: NativeSyncState, ...events: NativeEvent[]) => events.reduce(reduceNativeEvent, state)

const shape = (state: NativeSyncState) =>
  state.items.map((item) => item.tool ? `tool:${item.tool.toolName}` : `${item.role}:${item.text}${item.isStreaming ? "…" : ""}`)

describe("what an agent thinks", () => {
  it("is shown as it arrives and settles when the thought is done", () => {
    let state = reduce(createNativeSyncState(),
      { eventId: "1", type: "reasoning_text_delta", id: "r1", delta: "The flag " },
      { eventId: "2", type: "reasoning_text_delta", id: "r1", delta: "belongs in list.ts." },
    )
    expect(shape(state)).toEqual(["reasoning:The flag belongs in list.ts.…"])

    state = reduce(state, { eventId: "3", type: "reasoning_text_done", id: "r1", text: "The flag belongs in list.ts." })
    expect(shape(state)).toEqual(["reasoning:The flag belongs in list.ts."])
  })

  it("stays apart from the reply, also when both carry the same ID", () => {
    const state = reduce(createNativeSyncState(),
      { eventId: "1", type: "reasoning_text_delta", id: "0", delta: "Check the tests first." },
      { eventId: "2", type: "reasoning_text_done", id: "0", text: "Check the tests first." },
      { eventId: "3", type: "assistant_text_delta", id: "0", delta: "Running the tests." },
      { eventId: "4", type: "assistant_text_done", id: "0", text: "Running the tests." },
    )

    expect(shape(state)).toEqual(["reasoning:Check the tests first.", "assistant:Running the tests."])
  })

  it("is kept where it was thought, before the step's reply and its tool calls", () => {
    const state = reduce(createNativeSyncState(),
      { eventId: "1", type: "reasoning_text_done", id: "r1", text: "Read the file first." },
      { eventId: "2", type: "tool_call_start", callId: "c1", name: "read", args: { path: "list.ts" } },
      { eventId: "3", type: "reasoning_text_done", id: "r2", text: "Now change it." },
      { eventId: "4", type: "tool_call_start", callId: "c2", name: "edit", args: { path: "list.ts" } },
    )

    expect(shape(state)).toEqual(["reasoning:Read the file first.", "tool:read", "reasoning:Now change it.", "tool:edit"])
  })

  it("leaves nothing behind when a thought turns out to be empty", () => {
    const state = reduce(createNativeSyncState(), { eventId: "1", type: "reasoning_text_done", id: "r1", text: "" })

    expect(state.items).toEqual([])
  })

  it("is read from a snapshot, ahead of what the same message said and did", () => {
    const state = stateFromNativeSnapshot({
      messages: [
        { role: "user", content: "system" },
        { role: "user", content: "add a --json flag" },
        { role: "assistant", content: [
          { type: "reasoning", text: "The flag belongs in list.ts." },
          { type: "text", text: "Let me look." },
          { type: "tool-call" },
        ] },
        // A thought the agent did not put into words is no thought to show.
        { role: "assistant", content: [{ type: "reasoning", text: "" }, { type: "text", text: "Done." }] },
      ],
      toolCalls: [{ callId: "c1", name: "read", args: { path: "list.ts" } }],
    })

    expect(shape(state)).toEqual([
      "user:add a --json flag",
      "reasoning:The flag belongs in list.ts.",
      "assistant:Let me look.",
      "tool:read",
      "assistant:Done.",
    ])
  })

  it("is not shown twice when its events are replayed over a snapshot that has it", () => {
    const state = reconcileNativeSnapshot({
      messages: [
        { role: "user", content: "system" },
        { role: "assistant", content: [{ type: "reasoning", text: "The flag belongs in list.ts." }, { type: "text", text: "Done." }] },
      ],
      toolCalls: [],
      pendingPermissions: [],
      pendingQuestions: [],
    }, createNativeSyncState(), [
      { eventId: "1", type: "reasoning_text_delta", id: "r1", delta: "The flag belongs " },
      { eventId: "2", type: "reasoning_text_delta", id: "r1", delta: "in list.ts." },
      { eventId: "3", type: "reasoning_text_done", id: "r1", text: "The flag belongs in list.ts." },
      { eventId: "4", type: "assistant_text_done", id: "a1", text: "Done." },
    ])

    expect(shape(state)).toEqual(["reasoning:The flag belongs in list.ts.", "assistant:Done."])
  })

  it("is shown when it is newer than the snapshot it arrives over", () => {
    const state = reconcileNativeSnapshot({
      messages: [{ role: "user", content: "system" }, { role: "user", content: "go on" }],
      toolCalls: [],
    }, createNativeSyncState(), [
      { eventId: "1", type: "reasoning_text_delta", id: "r1", delta: "Next, the tests." },
    ])

    expect(shape(state)).toEqual(["user:go on", "reasoning:Next, the tests.…"])
  })
})
