import { useCallback, type RefObject } from "react"
import type { WSMessage } from "../lib/agentWire"
import { updatePermissionState, updateQuestionState, type NativeSyncState } from "../lib/nativeAgentSync"
import { pendingPermission, pendingQuestion } from "../lib/nativeFeed"

// useRequestAnswers sends the operator's answers to what the agent asked:
// a permission request, or a question. Each is shown as submitting until
// Legato says otherwise.
export function useRequestAnswers(agentId: string, send: (message: WSMessage) => unknown, sync: { syncStateRef: RefObject<NativeSyncState>; replaceSyncState: (next: NativeSyncState) => void }) {
  const { syncStateRef, replaceSyncState } = sync

  const handlePermission = useCallback(
    (requestId: string, decision: "allow" | "deny", feedback?: string, option?: string) => {
      if (!pendingPermission(syncStateRef.current, requestId)) return
      replaceSyncState(updatePermissionState(syncStateRef.current, requestId, (permission) => ({
        ...permission,
        status: "submitting",
        error: undefined,
      })))
      send({
        type: "agent_permission",
        agent_id: agentId,
        request_id: requestId,
        decision,
        ...(feedback ? { feedback } : {}),
        ...(option ? { option } : {}),
      })
    },
    [agentId, replaceSyncState, send, syncStateRef],
  )

  // Answers a question the agent asked, or closes it unanswered when
  // `answers` is left out.
  const handleQuestion = useCallback(
    (requestId: string, answers?: Record<string, string[]>) => {
      if (!pendingQuestion(syncStateRef.current, requestId)) return
      replaceSyncState(updateQuestionState(syncStateRef.current, requestId, (question) => ({
        ...question,
        status: "submitting",
        error: undefined,
      })))
      send({
        type: "agent_question",
        agent_id: agentId,
        request_id: requestId,
        ...(answers ? { answers } : { dismissed: true }),
      })
    },
    [agentId, replaceSyncState, send, syncStateRef],
  )

  return { handlePermission, handleQuestion }
}
