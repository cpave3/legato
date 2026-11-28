import { isChimeraRecovery } from "./chimeraRecovery"
import type { PromptState } from "./agentWire"
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

// The agent has stopped on a permission prompt or a question of its own.
export function awaitsRequest(items: NativeConversationItem[]): boolean {
  return items.some((item) => (item.permission ?? item.question)?.status === "pending")
}

// The prompt types that ask the person to pick something.
const choicePromptTypes = new Set<PromptState["type"]>(["menu", "tool_approval", "plan_approval", "question"])

// Whether the agent's terminal, as read off its screen, asks the operator to
// pick something. A request the agent raised itself is drawn by its own
// card; the terminal shows the same request as a dialog, which is not shown
// twice.
export function terminalAsks(prompt: PromptState | null, awaitingRequest: boolean): prompt is PromptState {
  return !awaitingRequest && prompt !== null && choicePromptTypes.has(prompt.type) && (prompt.actions?.length ?? 0) > 0
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
