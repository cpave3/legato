import { describe, expect, it } from "vitest"
import {
  createNativeSyncState,
  reconcileNativeSnapshot,
  reduceNativeEvent,
  stateFromNativeSnapshot,
  updateQuestionState,
  type NativeQuestion,
} from "../nativeAgentSync"

const color: NativeQuestion = {
  id: "q1",
  header: "Color",
  question: "Which color?",
  options: [{ label: "Red", description: "Warm" }, { label: "Blue" }],
  multiSelect: false,
  allowOther: true,
}

const asked = (requestId = "req-1") => reduceNativeEvent(createNativeSyncState(), {
  eventId: `asked-${requestId}`,
  type: "question_request",
  requestId,
  questions: [color],
})

describe("questions an agent puts to the person", () => {
  it("hydrates an open question from a snapshot and keeps the session busy", () => {
    const state = stateFromNativeSnapshot({
      status: "waiting_for_input",
      messages: [],
      toolCalls: [],
      pendingQuestions: [{ requestId: "req-1", questions: [color] }],
    })

    expect(state.busy).toBe(true)
    expect(state.items).toHaveLength(1)
    expect(state.items[0]?.question).toEqual({ requestId: "req-1", questions: [color], status: "pending" })
  })

  it("opens a question when asked and records the answers given", () => {
    let state = asked()
    expect(state.items[0]?.question).toMatchObject({ requestId: "req-1", status: "pending", questions: [color] })

    state = reduceNativeEvent(state, {
      eventId: "answered", type: "question_resolved", requestId: "req-1", answers: { q1: ["Blue"] },
    })

    expect(state.items).toHaveLength(1)
    expect(state.items[0]?.question).toMatchObject({ status: "answered", answers: { q1: ["Blue"] } })
  })

  it("tells a dismissed question from one answered somewhere else", () => {
    const dismissed = reduceNativeEvent(asked(), {
      eventId: "dismissed", type: "question_resolved", requestId: "req-1", dismissed: true,
    })
    const elsewhere = reduceNativeEvent(asked(), { eventId: "timeout", type: "question_timeout", requestId: "req-1" })

    expect(dismissed.items[0]?.question?.status).toBe("dismissed")
    expect(elsewhere.items[0]?.question?.status).toBe("expired")
  })

  it("settles only the question the event names", () => {
    let state = asked("req-1")
    state = reduceNativeEvent(state, { eventId: "asked-2", type: "question_request", requestId: "req-2", questions: [color] })

    state = reduceNativeEvent(state, { eventId: "timeout", type: "question_timeout", requestId: "req-2" })

    expect(state.items.map((item) => item.question?.status)).toEqual(["pending", "expired"])
  })

  it("keeps a question the snapshot still lists and drops one that was settled", () => {
    let state = asked("req-1")
    state = reduceNativeEvent(state, { eventId: "asked-2", type: "question_request", requestId: "req-2", questions: [color] })
    state = reduceNativeEvent(state, {
      eventId: "answered", type: "question_resolved", requestId: "req-1", answers: { q1: ["Red"] },
    })

    // The transcript's own tool call records a settled question from here on.
    state = reconcileNativeSnapshot({
      messages: [], toolCalls: [], pendingPermissions: [],
      pendingQuestions: [{ requestId: "req-2", questions: [color] }],
    }, state, [])

    expect(state.items.map((item) => item.question?.requestId)).toEqual(["req-2"])
    expect(state.items[0]?.question?.status).toBe("pending")
  })

  it("does not draw a settled question again when its events are replayed over a snapshot", () => {
    // A panel that opens on a session the server is already streaming gets the
    // snapshot and then the events the server kept, this question's among them.
    const state = reconcileNativeSnapshot({
      messages: [
        { role: "user", content: "system" },
        { role: "assistant", content: [{ type: "text", text: "Let me ask." }, { type: "tool-call" }] },
        { role: "assistant", content: [{ type: "text", text: "Blue it is." }] },
      ],
      toolCalls: [{ callId: "call-ask", name: "AskUserQuestion", args: {}, result: { answered: true } }],
      pendingPermissions: [],
      pendingQuestions: [],
    }, createNativeSyncState(), [
      { eventId: "1", type: "tool_call_start", callId: "call-ask", name: "AskUserQuestion", args: {} },
      { eventId: "2", type: "question_request", requestId: "req-1", questions: [color] },
      { eventId: "3", type: "question_resolved", requestId: "req-1", answers: { q1: ["Blue"] } },
      { eventId: "4", type: "assistant_text_done", id: "a", text: "Blue it is." },
    ])

    expect(state.items.map((item) => item.question ? "question card" : item.tool?.toolName ?? item.text)).toEqual([
      "Let me ask.",
      "AskUserQuestion",
      "Blue it is.",
    ])
  })

  it("draws a question that is still open once when its request is replayed over a snapshot", () => {
    const state = reconcileNativeSnapshot({
      messages: [], toolCalls: [], pendingPermissions: [],
      pendingQuestions: [{ requestId: "req-1", questions: [color] }],
    }, createNativeSyncState(), [
      { eventId: "1", type: "question_request", requestId: "req-1", questions: [color] },
    ])

    expect(state.items.map((item) => item.question?.status)).toEqual(["pending"])
  })

  it("keeps an open question when the snapshot does not say which are open", () => {
    const state = reconcileNativeSnapshot({ messages: [], toolCalls: [] }, asked(), [])

    expect(state.items[0]?.question).toMatchObject({ requestId: "req-1", status: "pending" })
  })

  it("marks one question as being sent without touching the others", () => {
    let state = asked("req-1")
    state = reduceNativeEvent(state, { eventId: "asked-2", type: "question_request", requestId: "req-2", questions: [color] })

    state = updateQuestionState(state, "req-2", (question) => ({ ...question, status: "submitting" }))

    expect(state.items.map((item) => item.question?.status)).toEqual(["pending", "submitting"])
  })
})
