import { isChimeraRecovery } from "./chimeraRecovery"
import type { NativeConversationItem } from "./nativeAgentSync"

// What the native agent panel shows of a session, derived from what it
// knows. Nothing here draws anything.

// The state a native agent's header shows: waiting on the operator first,
// then an error its hooks reported, then whether a turn runs.
export type NativeHeaderState = "needs-you" | "error" | "running" | "background" | "idle"

export function agentChatState(awaitingPerson: boolean, activity: string | undefined, busy: boolean): NativeHeaderState {
  if (awaitingPerson) return "needs-you"
  if (activity === "error") return "error"
  if (busy) return "running"
  if (activity === "background") return "background"
  return "idle"
}

// The words of a failure, whatever was thrown.
export function reasonText(reason: unknown): string {
  return reason instanceof Error ? reason.message : String(reason)
}

// A session of Chimera's own, which is what a panel without an agent tool
// serves.
export function isChimeraKind(agentKind: string | undefined): boolean {
  return !agentKind || agentKind === "chimera"
}

// A shell command the operator writes in the message box: "!" and the
// command.
export function isBangText(text: string): boolean {
  return text.startsWith("!") && text.length > 1
}

// A finished reply of Chimera's that is a compaction recovery excerpt, which
// is drawn folded.
export function isRecoveryReply(item: NativeConversationItem, agentKind: string | undefined): boolean {
  return isChimeraKind(agentKind) && item.role === "assistant" && !item.isStreaming && isChimeraRecovery(item.text)
}
