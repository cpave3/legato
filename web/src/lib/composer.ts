import type { VoiceRecorder } from "../hooks/useVoiceRecorder"
import type { NativeConversationItem } from "./nativeAgentSync"
import { cn } from "./utils"

// currentStep says what the agent is doing right now: the newest tool call
// that has not finished, as one line. Null when no call is running.
export function currentStep(items: NativeConversationItem[]): string | null {
  for (let index = items.length - 1; index >= 0; index--) {
    const tool = items[index].tool
    if (!tool || tool.result) continue
    const args = tool.args ?? {}
    const what = typeof args.command === "string" ? args.command
      : typeof args.path === "string" ? args.path.split("/").pop()
      : tool.summary
    return what ? `${tool.toolName} ${what}` : tool.toolName
  }
  return null
}

// formatElapsed is how long a turn has run, to the second while that matters.
export function formatElapsed(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ${seconds % 60}s`
  return `${Math.floor(minutes / 60)}h ${minutes % 60}m`
}

// A message sent while the agent works: it goes to the agent at once and is
// read at its next step. Until then the composer lists it.
export interface QueuedMessage {
  text: string
  // How many messages with the same words the conversation had when this one
  // was sent, so an earlier one is not taken for it.
  before: number
}

function userMessagesWith(items: NativeConversationItem[], text: string): number {
  return items.filter((item) => item.role === "user" && item.text.trim() === text).length
}

export function queueMessage(queued: QueuedMessage[], text: string, items: NativeConversationItem[]): QueuedMessage[] {
  const earlier = queued.filter((entry) => entry.text === text).length
  return [...queued, { text, before: userMessagesWith(items, text) + earlier }]
}

// unreadQueued is what of the queue the agent has not read yet: a message is
// read once the conversation shows it.
export function unreadQueued(queued: QueuedMessage[], items: NativeConversationItem[]): QueuedMessage[] {
  return queued.filter((entry) => userMessagesWith(items, entry.text) <= entry.before)
}

// shortModelName is a model as the composer's chip names it: without its
// provider, and without "claude-" before a Claude model's family.
export function shortModelName(ref: string): string {
  return ref.slice(ref.lastIndexOf("/") + 1).replace(/^claude-/, "")
}

// The share of the context window from which the composer warns.
export const CONTEXT_NEARLY_FULL = 85

// The ghost button of the composer's foot: a word on a wide screen, its icon
// on a phone.
export const composerToolButton = (open = false) => cn(
  "flex h-7 shrink-0 items-center gap-1.5 rounded-md px-2 text-xs transition-colors max-sm:size-9 max-sm:justify-center max-sm:px-0",
  open ? "bg-lw-raised text-lw-text" : "text-lw-text-2 hover:bg-lw-raised hover:text-lw-text",
)

// Whether a recording has the composer's foot.
export function dictating(recorder: VoiceRecorder | null): boolean {
  return recorder !== null && (recorder.state === "recording" || recorder.state === "transcribing" || recorder.state === "error")
}
