import { getTextWidth } from "@evenrealities/pretext"
import { LIST_ITEMS, TEXT_UPDATE_LIMIT, ago, fitItem, stateOf } from "./glance"
import { CANVAS_WIDTH, ITEM_TEXT_WIDTH, TITLE_WIDTH, center, inner } from "./layout"
import type { Agent, Conversation, ConversationResult } from "./legato"

// An agent's pages share a header: the state in a badge of its own, so it
// reads first, and the task and title beside it, dimmer. Under it, the feed
// (one line an entry, in a frame) or one entry in full, and the touchpad's
// keys centred at the foot.
export type Header = { badge: string; title: string }
// A line of the feed, and the entry in full for when it is opened.
// `asks` marks the line of what the agent asks for, which opens its
// approval: a permission it asks for, or a question.
// `key` names what the line shows, the same from one read to the next while
// its text changes (a reply growing, a call ending), so an opened entry can
// follow it (followEntry).
export type FeedItem = { text: string; detail?: string; asks?: "permission" | "question"; key?: string }
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

// How fresh a page is: when what it shows was read, and why the latest read
// failed, if it did (the page then keeps what it last read).
export type Freshness = { readAt?: Date; now?: Date; problem?: string }

// A header's title: `name`, or what went wrong in its place after `who`,
// cut to the title's box, with how old the page is (once a minute or more)
// kept whole at its end, so a stale page never passes for a fresh one.
export function freshTitle(who: string, name: string, fresh: Freshness = {}): string {
  const old = fresh.readAt ? ago(fresh.readAt, fresh.now ?? new Date()) : ""
  const tail = old ? ` · ${old}` : ""
  const text = fresh.problem ? `${who} ▲ ${flat(fresh.problem)}` : name
  return fitItem(text, inner(TITLE_WIDTH) - getTextWidth(tail), Infinity) + tail
}

// What an agent's pages say once Legato no longer lists it as running: the
// app marks it so (status ENDED), and its last state would be stale.
export const ENDED = "ended"
const ended = { mark: "○", word: "ended" }

function header(agent: Agent, fresh?: Freshness): Header {
  const s = agent.status === ENDED ? ended : stateOf(agent)
  const name = agent.title.trim()
  return { badge: `${s.mark} ${s.word}`, title: freshTitle(agent.task_id, `${agent.task_id}${name ? ` ${name}` : ""}`, fresh) }
}

export const flat = (text: string) => text.replace(/\s+/g, " ").trim()
const line = (text: string): FeedItem["text"] => fitItem(flat(text), ITEM_TEXT_WIDTH)
export const whole = (text: string) => (text.length > TEXT_UPDATE_LIMIT ? `${text.slice(0, TEXT_UPDATE_LIMIT - 1).trimEnd()}…` : text)

// A user turn that opens with a [legato …] or [swarm event] tag is Legato's
// own message, or another agent's relayed by it, not the operator's
// (docs/ai/dev-notes.md); the web UI reads it the same way
// (web/src/lib/legatoMessage.ts).
const tagPattern = /^\[(legato|swarm event)([^\]]*)\]\s*/
const peerPattern = /^ peer(?: (\S+))?$/
const peerFooterPattern = /\n+---\nAgent-to-agent message[^\n]*$/

// The tags of the operator's own words, said through a device: on the
// glasses, or aloud (internal/service/reeve_glasses.go, reeve_voice_lines.go).
const ownTags = new Set([" glasses", " voice"])
// What a resumed Reeve is given after the message that woke it.
const resumeNote = /\n+\[legato\][\s\S]*$/

// Who said a user turn, and what.
export function speaker(text: string): { who: string; said: string } {
  const tag = tagPattern.exec(text)
  if (!tag) return { who: "You", said: text.trim() }
  const [all, family, rest] = tag
  const body = text.slice(all.length)
  if (family === "legato" && ownTags.has(rest)) return { who: "You", said: body.replace(resumeNote, "").trim() }
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
      // A call that started a subagent runs while the subagent does.
      const running = e.state === "running" || e.subagent?.status === "running"
      const mark = e.state === "failed" ? "▲" : running ? "▶" : "·"
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

// An entry's key is where it is in the conversation as read (the newest
// CONVERSATION_ENTRIES of it, so every entry moves up as the oldest leave)
// and its kind: "#<index> <kind>". entryShift says how far they moved between
// two reads, which followEntry takes off the index.
const entryKey = (i: number, e: Entry) => `#${i} ${e.kind}`
const ENTRY_KEY = /^#(\d+) (\S*?)(:sub|:thought)?$/
const SUB = ":sub"
const THOUGHT = ":thought"
const LIVE_THINKING = "live:thinking"
const LIVE_REPLY = "live:reply"
// Something the agent said, as an entry (not its thought).
const said = (key: string | undefined) => !!key && /^#\d+ assistant$/.test(key)

// What does not change while an entry updates: its kind, its tool, its text
// (which for a call says what it is for) and its command; not a call's state.
const signature = (e: Entry) => JSON.stringify([e.kind, e.tool ?? "", e.text, e.command ?? ""])

// How many of the oldest entries `before` had are gone from `after`: the
// least shift at which what is left of `before` reads as the start of
// `after` (entries are only ever added at the end). Undefined when none
// does, as when every entry `before` had is gone.
export function entryShift(before: Conversation, after: Conversation): number | undefined {
  const old = before.entries.map(signature)
  const now = after.entries.map(signature)
  for (let k = 0; k < old.length; k++) {
    const overlap = Math.min(old.length - k, now.length)
    if (overlap === 0) break
    let same = true
    for (let j = 0; j < overlap && same; j++) same = old[k + j] === now[j]
    if (same) return k
  }
  return old.length === 0 ? 0 : undefined
}

// An entry's lines, newest first: what a subagent it started does now, the
// entry, and the thought that led to its step.
function lines(e: Entry, key: string): FeedItem[] {
  const out: FeedItem[] = []
  const sub = e.subagent
  if (sub?.status === "running" && sub.tool) {
    const what = flat(sub.text ?? "")
    out.push({
      text: line(what ? `└ ${sub.tool}: ${what}` : `└ ${sub.tool}`),
      detail: [`Subagent: ${sub.purpose.trim()}`, `Now: ${[sub.tool, sub.text?.trim()].filter(Boolean).join(" ")}`].join("\n\n"),
      key: key + SUB,
    })
  }
  out.push({ ...item(e), key })
  if (e.thinking?.trim()) out.push({ text: line(`Thought: ${e.thinking}`), detail: `Thought\n\n${e.thinking.trim()}`, key: key + THOUGHT })
  return out
}

// The agent's feed: what it asks for, what it is writing now (a thought, a
// reply), then what it said and did, newest first. `read` is undefined until the first answer; it is the last one
// that worked when a later read failed (`fresh.problem`).
export function agentPage(agent: Agent, read: ConversationResult | undefined, fresh?: Freshness): FeedPage {
  const page = (items: FeedItem[]) => ({ ...header(agent, fresh), items, hint: hint("Tap: expand · Hold: dictate · Double tap: back") })
  if (!read) return page([{ text: "Reading the conversation…" }])
  if (!read.ok) return page([{ text: line(read.problem), detail: read.problem }])

  const { permission, question, entries } = read.conversation
  const items: FeedItem[] = []
  if (permission) {
    const what = [permission.tool, permission.command?.trim()].filter(Boolean).join(" ")
    const detail = [`Asks to use ${permission.tool}`, permission.command?.trim(), permission.reason?.trim(), permission.plan?.trim()]
    items.push({ text: line(`● Asks: ${what}`), detail: detail.filter(Boolean).join("\n\n"), asks: "permission", key: "asks:permission" })
  }
  if (question) {
    const options = question.options.map((o, i) => `${i + 1}. ${o.trim()}`).join("\n")
    items.push({ text: line(`● Asks: ${question.text}`), detail: [`Asks: ${question.text.trim()}`, options].filter(Boolean).join("\n\n"), asks: "question", key: "asks:question" })
  }
  const live = read.conversation.live
  if (live?.thinking?.trim()) items.push({ text: line(`Thinking: ${live.thinking}`), detail: `Thinking\n\n${live.thinking.trim()}`, key: LIVE_THINKING })
  if (live?.reply?.trim()) items.push({ text: line(`Writing: ${live.reply}`), detail: `Agent, still writing\n\n${live.reply.trim()}`, key: LIVE_REPLY })
  items.push(...entries.map((e, i) => lines(e, entryKey(i, e))).reverse().flat())
  return page(items.length ? items.slice(0, LIST_ITEMS) : [{ text: "Nothing said yet." }])
}

// An entry opened from the feed, as a newer read of the feed has it: the
// line of the same key, an entry's index less `shift` (entryShift between the
// read it was opened from and this one; undefined when they could not be
// lined up); for a subagent's line that has gone (the subagent is done), the
// call that started it; for a reply being written that has gone, the newest
// thing the agent said, which it became. Else the entry as it was, without
// its key, so no later read takes it for whatever is then in its place.
export function followEntry(opened: FeedItem, page: FeedPage, shift: number | undefined): FeedItem {
  let key = opened.key
  if (!key) return opened
  const at = ENTRY_KEY.exec(key)
  if (at) {
    if (shift === undefined) return detached(opened)
    key = `#${Number(at[1]) - shift} ${at[2]}${at[3] ?? ""}`
  }
  const find = (k: string) => page.items.find((i) => i.key === k)
  const found =
    find(key) ??
    (key.endsWith(SUB) ? find(key.slice(0, -SUB.length)) : undefined) ??
    (key === LIVE_REPLY ? page.items.find((i) => said(i.key)) : undefined)
  return found ?? detached(opened)
}

const detached = (entry: FeedItem): FeedItem => {
  const { text, detail, asks } = entry
  return asks ? { text, detail, asks } : { text, detail }
}

// One entry of the feed in full.
export function entryPage(agent: Agent, entry: FeedItem, fresh?: Freshness): TextPage {
  return { ...header(agent, fresh), body: whole(entry.detail ?? entry.text), hint: hint("Hold: dictate · Double tap: back") }
}

function minutes(seconds: number): string {
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`
}

// The pages of a message the operator dictates to the agent.
export function dictationPage(agent: Agent, d: Dictation): TextPage {
  return dictation(header(agent).title, { speak: "Speak your message to the agent.", sent: `Sent to ${agent.task_id}.` }, d)
}

// The pages of a dictated message, under `title`, with the words that say
// whom it goes to.
export function dictation(title: string, to: { speak: string; sent: string }, d: Dictation): TextPage {
  const page = (badge: string, body: string, keys: string) => ({ badge, title, body, hint: keys ? hint(keys) : "" })
  switch (d.step) {
    case "listening":
      return page("● Listening", `${to.speak}\n\n${minutes(d.seconds)}`, "Release: stop · Double tap: cancel")
    case "transcribing":
      return page("Message", "Transcribing…", "Double tap: cancel")
    case "review":
      return page("Message", `“${d.text}”`, "Tap: send · Double tap: drop it")
    case "sending":
      return page("Sending", `“${d.text}”`, "")
    case "sent":
      return page("Sent", to.sent, "")
    case "problem":
      return d.text
        ? page("▲ Not sent", `${d.problem}\n\n“${d.text}”`, "Tap: send again · Double tap: drop it")
        : page("▲ Problem", d.problem, "Hold: try again · Double tap: back")
  }
}
