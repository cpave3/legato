import { dictation, flat, freshTitle, speaker, whole, type Dictation, type Freshness, type TextPage } from "./agent"
import { fitItem, stateOf } from "./glance"
import { CANVAS_WIDTH, FRAME_WIDTH, center, inner } from "./layout"
import type { Agent, Conversation, ConversationResult } from "./legato"

// The Reeve's chat: the instance's own agent, talked to directly. Its page
// has the agents' header (the state in a badge, bright; "Reeve · <instance>"
// beside it, dim), then what the operator said last on a dim line of its
// own, the Reeve's reply in the frame under it, bright, and the touchpad's
// keys at the foot.
export type ChatPage = { badge: string; title: string; asked: string; reply: string; hint: string }

// How wide the operator's line may be: the frame's width less its padding.
export const CHAT_WIDTH = inner(FRAME_WIDTH)

export type ChatOptions = Freshness & {
  // The instance's name, for the title.
  instance: string
  // A message just sent, which the conversation may not have yet.
  pending?: string
}

type Entry = Conversation["entries"][number]

// The Reeve's state in its own words: ready rather than idle, and asleep
// when it is not running (a message wakes it).
function badge(reeve: Agent | undefined): string {
  if (!reeve) return "○ asleep"
  const s = stateOf(reeve)
  if (s.activity === "") return "○ ready"
  if (s.activity === "working") return "▶ working"
  return `${s.mark} ${s.word}`
}

const title = (instance: string) => fitItem(`Reeve · ${instance}`, Infinity, Infinity)

// The Reeve is asked to answer the glasses in plain text; what Markdown it
// writes anyway is taken out, since the glasses would show the marks.
export function plain(text: string): string {
  return text
    .replace(/^```.*$\n?/gm, "")
    .replace(/^#{1,6}\s+/gm, "")
    .replace(/^\s*[-*]\s+/gm, "• ")
    .replace(/\*\*(.+?)\*\*|__(.+?)__/g, "$1$2")
    .replace(/`([^`]+)`/g, "$1")
    .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")
    .trim()
}

// The newest exchange: the last user turn, and what came after it.
function lastTurn(entries: Entry[]): { turn?: { who: string; said: string }; after: Entry[] } {
  const at = entries.map((e) => e.kind).lastIndexOf("user")
  if (at < 0) return { after: entries }
  return { turn: speaker(entries[at].text), after: entries.slice(at + 1) }
}

// Whether the conversation has a message sent from the glasses as its last
// user turn yet.
export function heard(read: ConversationResult | undefined, text: string): boolean {
  return read?.ok === true && lastTurn(read.conversation.entries).turn?.said === text.trim()
}

function tool(e: Entry): string {
  const mark = e.state === "failed" ? "▲" : e.state === "running" ? "▶" : "·"
  const what = flat(e.text || e.command || "")
  return `${mark} ${(e.tool ?? "tool").split("__").pop()}${what ? `: ${what}` : ""}`
}

function asks(c: Conversation): string {
  if (c.permission) return [`Asks to use ${c.permission.tool}`, c.permission.command?.trim(), c.permission.reason?.trim()].filter(Boolean).join("\n")
  if (c.question) return [`Asks: ${c.question.text.trim()}`, ...c.question.options.map((o, i) => `${i + 1}. ${o.trim()}`)].join("\n")
  return ""
}

// The chat page for the instance's Reeve (`reeve`, its running agent, if
// any) and its conversation as last read.
export function reeveChat(reeve: Agent | undefined, read: ConversationResult | undefined, options: ChatOptions): ChatPage {
  const page = (asked: string, reply: string): ChatPage => ({
    badge: badge(reeve),
    title: freshTitle("Reeve", title(options.instance), options),
    asked: asked ? fitItem(flat(asked), CHAT_WIDTH, Infinity) : "",
    reply: whole(reply),
    hint: center("Hold: talk · Tap: history · Double tap: back", CANVAS_WIDTH),
  })
  const pending = options.pending?.trim()
  const working = reeve?.activity === "working" || reeve?.activity === "background"

  if (pending && !heard(read, pending)) return page(`You: ${pending}`, "Thinking…")
  if (!reeve) return page("", "The Reeve is asleep. Hold to talk and it wakes.")
  if (!read) return page("", "Reading…")
  if (!read.ok) return page("", read.problem)

  const { turn, after } = lastTurn(read.conversation.entries)
  const asked = turn ? `${turn.who}: ${turn.said}` : ""
  const said = after.filter((e) => e.kind === "assistant" && e.text.trim()).map((e) => plain(e.text))
  // The reply it is writing now, which no entry holds yet.
  const writing = read.conversation.live?.reply?.trim()
  if (writing) said.push(`${plain(writing)}…`)
  const question = asks(read.conversation)
  if (question) return page(asked, [question, ...said].join("\n\n"))
  if (said.length) return page(asked, said.join("\n\n"))
  if (working) {
    const tools = after.filter((e) => e.kind === "tool")
    return page(asked, tools.length ? `Thinking…\n\n${tool(tools[tools.length - 1])}` : "Thinking…")
  }
  return page(asked, turn ? "No reply." : "Hold to talk to the Reeve.")
}

// The pages of a message dictated to the Reeve.
export function reeveDictation(instance: string, d: Dictation): TextPage {
  return dictation(title(instance), { speak: "Speak to the Reeve.", sent: "Sent to the Reeve." }, d)
}
