import { LIST_ITEMS, TEXT_UPDATE_LIMIT, fitItem, stateOf } from "./glance"
import { CANVAS_WIDTH, ITEM_TEXT_WIDTH, TITLE_WIDTH, center, inner } from "./layout"
import type { Agent, Conversation, ConversationResult } from "./legato"

// An agent's pages share a header: the state in a badge of its own, so it
// reads first, and the task and title beside it, dimmer. Under it, the feed
// (one line an entry, in a frame) or one entry in full, and the touchpad's
// keys centred at the foot.
export type Header = { badge: string; title: string }
// A line of the feed, and the entry in full for when it is opened.
export type FeedItem = { text: string; detail?: string }
export type FeedPage = Header & { items: FeedItem[]; hint: string }
export type TextPage = Header & { body: string; hint: string }

export type Dictation =
  | { step: "listening"; seconds: number }
  | { step: "transcribing" }
  | { step: "review"; text: string }
  | { step: "sending"; text: string }
  | { step: "sent" }
  // A problem; with the text of a message that failed to send, which a tap sends again.
  | { step: "problem"; problem: string; text?: string }

type Entry = Conversation["entries"][number]

const hint = (keys: string) => center(keys, CANVAS_WIDTH)

function header(agent: Agent): Header {
  const s = stateOf(agent)
  const name = agent.title.trim()
  return { badge: `${s.mark} ${s.word}`, title: fitItem(`${agent.task_id}${name ? ` ${name}` : ""}`, inner(TITLE_WIDTH), Infinity) }
}

const flat = (text: string) => text.replace(/\s+/g, " ").trim()
const line = (text: string): FeedItem["text"] => fitItem(flat(text), ITEM_TEXT_WIDTH)
const whole = (text: string) => (text.length > TEXT_UPDATE_LIMIT ? `${text.slice(0, TEXT_UPDATE_LIMIT - 1).trimEnd()}…` : text)

// A user turn that opens with a [legato …] or [swarm event] tag is Legato's
// own message, or another agent's relayed by it, not the operator's
// (docs/ai/dev-notes.md); the web UI reads it the same way
// (web/src/lib/legatoMessage.ts).
const tagPattern = /^\[(legato|swarm event)([^\]]*)\]\s*/
const peerPattern = /^ peer(?: (\S+))?$/
const peerFooterPattern = /\n+---\nAgent-to-agent message[^\n]*$/

// Who said a user turn, and what.
function speaker(text: string): { who: string; said: string } {
  const tag = tagPattern.exec(text)
  if (!tag) return { who: "You", said: text.trim() }
  const [all, family, rest] = tag
  const body = text.slice(all.length)
  const peer = family === "legato" ? peerPattern.exec(rest) : null
  if (peer) return { who: peer[1] ? `Agent ${peer[1]}` : "Agent", said: body.replace(peerFooterPattern, "").trim() }
  const kind = family === "legato" ? rest.trim() : family
  return { who: kind ? `Legato ${kind}` : "Legato", said: body.trim() }
}

// Each entry is "<label>: <what>", a tool call marked by how it stands (▶
// running, ▲ failed, · done) and named without an MCP server's prefix.
function item(e: Entry): FeedItem {
  switch (e.kind) {
    case "user": {
      const { who, said } = speaker(e.text)
      return { text: line(`${who}: ${said}`), detail: `${who}\n\n${said}` }
    }
    case "tool": {
      const mark = e.state === "failed" ? "▲" : e.state === "running" ? "▶" : "·"
      const tool = (e.tool ?? "tool").split("__").pop()!
      const what = flat(e.text || e.command || "")
      const state = e.state ? ` (${e.state})` : ""
      return {
        text: line(what ? `${mark} ${tool}: ${what}` : `${mark} ${tool}`),
        detail: [`${tool}${state}`, e.text.trim(), e.command?.trim() ?? ""].filter(Boolean).join("\n\n"),
      }
    }
    default:
      return { text: line(`Agent: ${e.text}`), detail: `Agent\n\n${e.text.trim()}` }
  }
}

// The agent's feed: what it asks for, then what it said and did, newest
// first. `read` is undefined until the first answer.
export function agentPage(agent: Agent, read: ConversationResult | undefined): FeedPage {
  const page = (items: FeedItem[]) => ({ ...header(agent), items, hint: hint("Tap: expand · Hold: dictate · Double tap: back") })
  if (!read) return page([{ text: "Reading the conversation…" }])
  if (!read.ok) return page([{ text: line(read.problem), detail: read.problem }])

  const { permission, question, entries } = read.conversation
  const items: FeedItem[] = []
  if (permission) {
    const what = [permission.tool, permission.command?.trim()].filter(Boolean).join(" ")
    const detail = [`Asks to use ${permission.tool}`, permission.command?.trim(), permission.reason?.trim(), permission.plan?.trim()]
    items.push({ text: line(`● Asks: ${what}`), detail: detail.filter(Boolean).join("\n\n") })
  }
  if (question) {
    const options = question.options.map((o, i) => `${i + 1}. ${o.trim()}`).join("\n")
    items.push({ text: line(`● Asks: ${question.text}`), detail: [`Asks: ${question.text.trim()}`, options].filter(Boolean).join("\n\n") })
  }
  items.push(...entries.slice().reverse().map(item))
  return page(items.length ? items.slice(0, LIST_ITEMS) : [{ text: "Nothing said yet." }])
}

// One entry of the feed in full.
export function entryPage(agent: Agent, entry: FeedItem): TextPage {
  return { ...header(agent), body: whole(entry.detail ?? entry.text), hint: hint("Hold: dictate · Double tap: back") }
}

function minutes(seconds: number): string {
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`
}

// The pages of a message the operator dictates to the agent.
export function dictationPage(agent: Agent, d: Dictation): TextPage {
  const { title } = header(agent)
  const page = (badge: string, body: string, keys: string) => ({ badge, title, body, hint: keys ? hint(keys) : "" })
  switch (d.step) {
    case "listening":
      return page("● Listening", `Speak your message to the agent.\n\n${minutes(d.seconds)}`, "Release: stop · Double tap: cancel")
    case "transcribing":
      return page("Message", "Transcribing…", "Double tap: cancel")
    case "review":
      return page("Message", `“${d.text}”`, "Tap: send · Double tap: drop it")
    case "sending":
      return page("Sending", `“${d.text}”`, "")
    case "sent":
      return page("Sent", `Sent to ${agent.task_id}.`, "")
    case "problem":
      return d.text
        ? page("▲ Not sent", `${d.problem}\n\n“${d.text}”`, "Tap: send again · Double tap: drop it")
        : page("▲ Problem", d.problem, "Hold: try again · Double tap: back")
  }
}
