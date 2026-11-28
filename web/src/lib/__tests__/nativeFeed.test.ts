import { describe, expect, it } from "vitest"
import { createNativeSyncState, type NativeConversationItem, type NativeEvent } from "../nativeAgentSync"
import { endsTurn, pendingRequest, reopenRequest, replayEvents, snapshotTurnRuns, subagentsRunning } from "../nativeFeed"

const permission = (requestId: string, status: string) => ({
  key: `p-${requestId}`, role: "system", text: "",
  permission: { requestId, tool: "bash", target: "host", command: "ls", status },
}) as unknown as NativeConversationItem
const question = (requestId: string, status: string) => ({
  key: `q-${requestId}`, role: "system", text: "",
  question: { requestId, questions: [], status },
}) as unknown as NativeConversationItem

describe("nativeFeed", () => {
  it("takes a running or waiting session for a turn in progress", () => {
    expect(snapshotTurnRuns("running")).toBe(true)
    expect(snapshotTurnRuns("waiting_for_permission")).toBe(true)
    expect(snapshotTurnRuns("waiting_for_input")).toBe(true)
    expect(snapshotTurnRuns("idle")).toBe(false)
    expect(snapshotTurnRuns(undefined)).toBe(false)
  })

  it("replays the events that arrived while a snapshot failed to load", () => {
    const state = createNativeSyncState()
    expect(replayEvents(state, [])).toBe(state)
    const next = replayEvents(state, [{ eventId: "e1", type: "session_started" } as NativeEvent])
    expect(next.busy).toBe(true)
  })

  it("knows a subagent may be running only from a spawn that has not ended", () => {
    const spawn = (status: string) => ({
      key: "s", role: "tool", text: "",
      tool: { key: "s", toolName: "spawn_agent", args: {}, subagent: { subagentId: "x", status, activities: [] } },
    }) as unknown as NativeConversationItem
    expect(subagentsRunning([spawn("running")])).toBe(true)
    expect(subagentsRunning([spawn("finished")])).toBe(false)
    expect(subagentsRunning([])).toBe(false)
  })

  it("refreshes after a turn, and after a compaction only in Chimera", () => {
    const event = (type: string) => ({ eventId: "e", type }) as NativeEvent
    expect(endsTurn(event("run_finished"), "claude-code")).toBe(true)
    expect(endsTurn(event("compaction_finished"), undefined)).toBe(true)
    expect(endsTurn(event("compaction_finished"), "chimera")).toBe(true)
    expect(endsTurn(event("compaction_finished"), "codex")).toBe(false)
    expect(endsTurn(event("message_delta"), undefined)).toBe(false)
  })

  it("reopens a request whose answer was not delivered, saying why", () => {
    const state = createNativeSyncState([permission("p1", "submitting"), question("q1", "submitting")])
    const permissionReopened = reopenRequest(state, "p1", undefined)
    expect(permissionReopened.items[0].permission).toMatchObject({ status: "pending", error: "Permission request failed" })
    const questionReopened = reopenRequest(state, "q1", "gone")
    expect(questionReopened.items[1].question).toMatchObject({ status: "pending", error: "gone" })
    expect(reopenRequest(state, "q1", undefined).items[1].question).toMatchObject({ error: "The answer could not be delivered" })
  })

  it("answers only a request the agent still waits on", () => {
    const state = createNativeSyncState([permission("p1", "pending"), permission("p2", "allowed"), question("q1", "pending"), question("q2", "answered")])
    expect(pendingRequest(state, "p1", "permission")).toBe(true)
    expect(pendingRequest(state, "p2", "permission")).toBe(false)
    expect(pendingRequest(state, "missing", "permission")).toBe(false)
    expect(pendingRequest(state, "q1", "question")).toBe(true)
    expect(pendingRequest(state, "q2", "question")).toBe(false)
    expect(pendingRequest(state, "p1", "question")).toBe(false)
  })
})
