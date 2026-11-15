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

// A message sent from the composer, listed until the conversation shows it.
// Its state is what Legato said of it: still sending, taken by the agent
// (which reads it at its next step, or as its next prompt), or not sent. An
// agent going idle says nothing of it: one the turn did not read becomes the
// agent's next prompt.
export interface QueuedMessage {
  // The request_id it was sent under; Legato's answers name it.
  id: string
  text: string
  // How many messages with the same words the conversation had when this one
  // was sent, so an earlier one is not taken for it.
  before: number
  state: "sending" | "sent" | "failed"
  error?: string
}

// How many times the conversation's user messages say text. Messages the agent
// had not read when its turn ended reach it as one prompt, one paragraph each,
// so each paragraph run that is the text counts.
function userMessagesWith(items: NativeConversationItem[], text: string): number {
  const needle = `\n\n${text}\n\n`
  let count = 0
  for (const item of items) {
    if (item.role !== "user") continue
    const said = `\n\n${item.text.trim()}\n\n`
    for (let at = said.indexOf(needle); at >= 0; at = said.indexOf(needle, at + needle.length - 2)) count++
  }
  return count
}

// isCommandText says whether a message is a command for the agent's session:
// a slash and a name, alone or before its arguments, the shape the Claude Code
// mod runs as one (`submit` in its register.ts). A path such as /tmp/a.png is
// not one: it is a message.
export function isCommandText(text: string): boolean {
  return /^\/[\w:.-]+(?:\s|$)/.test(text.trim())
}

export function queueMessage(queued: QueuedMessage[], text: string, items: NativeConversationItem[], id: string): QueuedMessage[] {
  const earlier = queued.filter((entry) => entry.text === text && entry.state !== "failed").length
  return [...queued, { id, text, before: userMessagesWith(items, text) + earlier, state: "sending" }]
}

// unreadQueued is what of the queue the agent has not read yet: a message is
// read once the conversation shows it. One that was not sent stays until the
// person takes it back.
export function unreadQueued(queued: QueuedMessage[], items: NativeConversationItem[]): QueuedMessage[] {
  return queued.filter((entry) => entry.state === "failed" || userMessagesWith(items, entry.text) <= entry.before)
}

// settleQueued records Legato's answer about the message sent under id.
export function settleQueued(queued: QueuedMessage[], id: string, error?: string): QueuedMessage[] {
  return queued.map((entry) => {
    if (entry.id !== id) return entry
    if (error !== undefined) return { ...entry, state: "failed", error }
    return entry.state === "sending" ? { ...entry, state: "sent" } : entry
  })
}

// failQueuedByWords marks as not sent the oldest message with these words that
// is not already: the agent's session reports a message it would not take by
// its words alone.
export function failQueuedByWords(queued: QueuedMessage[], text: string, error: string): QueuedMessage[] {
  const at = queued.findIndex((entry) => entry.text === text && entry.state !== "failed")
  if (at < 0) return queued
  return queued.map((entry, index) => index === at ? { ...entry, state: "failed", error } : entry)
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
