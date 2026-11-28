import { isChimeraKind } from "./nativeAgentView"
import {
  reduceNativeEvent, subagentStatus, updatePermissionState, updateQuestionState,
  type NativeConversationItem, type NativeEvent, type NativeSyncState,
} from "./nativeAgentSync"

// How the native panel follows a session's events and snapshots: what each
// tells it to do. Nothing here holds state.

// How the request_id of a message from the box starts, so Legato's answer
// about it is told from one about a permission or a question.
export const MESSAGE_REQUEST = "message-"

// The session says a turn runs: Working is no longer only the panel's guess.
export function snapshotTurnRuns(status: string | undefined): boolean {
  return status === "running" || status === "waiting_for_permission" || status === "waiting_for_input"
}

// Events that arrived while a snapshot was loading, applied to the state it
// failed to replace.
export function replayEvents(state: NativeSyncState, events: NativeEvent[]): NativeSyncState {
  let next = state
  for (const event of events) {
    next = reduceNativeEvent(next, event)
  }
  return next
}

// Whether a subagent the agent spawned may still be running, so a change in
// the agent list may be its end.
export function subagentsRunning(items: NativeConversationItem[]): boolean {
  return items.some(item => item.tool?.toolName === "spawn_agent" && ["spawning", "running"].includes(subagentStatus(item.tool)))
}

// A turn has ended (or Chimera compacted), after which the snapshot is the
// authority on what happened.
export function endsTurn(event: NativeEvent, agentKind: string | undefined): boolean {
  return event.type === "run_finished" || (isChimeraKind(agentKind) && event.type === "compaction_finished")
}

// An answer to a permission request or a question could not be delivered:
// the request is pending again, with why.
export function reopenRequest(state: NativeSyncState, requestId: string, error: string | undefined): NativeSyncState {
  const reopened = updatePermissionState(state, requestId, (permission) => ({
    ...permission,
    status: "pending",
    error: error || "Permission request failed",
  }))
  return updateQuestionState(reopened, requestId, (question) => ({
    ...question,
    status: "pending",
    error: error || "The answer could not be delivered",
  }))
}

// Whether the agent still waits on this permission request or question; an
// answer to anything else is not sent.
export function pendingPermission(state: NativeSyncState, requestId: string): boolean {
  const current = state.items.find((item) => item.permission?.requestId === requestId)?.permission
  return current !== undefined && current.status === "pending"
}

export function pendingQuestion(state: NativeSyncState, requestId: string): boolean {
  const current = state.items.find((item) => item.question?.requestId === requestId)?.question
  return current !== undefined && current.status === "pending"
}
