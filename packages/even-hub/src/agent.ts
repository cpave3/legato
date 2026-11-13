import { CANVAS_WIDTH, TEXT_UPDATE_LIMIT, fitItem, stateOf } from "./glance"
import type { Agent, ConversationResult } from "./legato"

// The width of the agent page's title line, inside its padding.
export const TITLE_TEXT_WIDTH = CANVAS_WIDTH - 16
// The most characters one message takes on the page.
const ENTRY_CHARS = 360

// What the agent page shows: a title line, the body (which scrolls) and a
// line that says what the touchpad does.
export type Page = { title: string; body: string; hint: string }

export type Dictation =
  | { step: "listening"; seconds: number }
  | { step: "transcribing" }
  | { step: "review"; text: string }
  | { step: "sending"; text: string }
  | { step: "sent" }
  | { step: "problem"; problem: string }

function title(agent: Agent): string {
  const s = stateOf(agent)
  const name = agent.title.trim()
  return fitItem(`${s.mark} ${s.word}  ${agent.task_id}${name ? ` ${name}` : ""}`, TITLE_TEXT_WIDTH, Infinity)
}

// One line of prose: newlines become spaces, and a long message is cut.
function prose(text: string): string {
  const flat = text.replace(/\s+/g, " ").trim()
  return flat.length > ENTRY_CHARS ? `${flat.slice(0, ENTRY_CHARS - 1).trimEnd()}…` : flat
}

function entry(e: { kind: string; text: string; tool?: string; command?: string; state?: string }): string {
  switch (e.kind) {
    case "user":
      return `You: ${prose(e.text)}`
    case "tool": {
      const mark = e.state === "failed" ? "▲" : e.state === "running" ? "▶" : "›"
      // An MCP tool is named mcp__<server>__<tool>: the tool's own name says enough.
      const tool = (e.tool ?? "tool").split("__").pop()
      const what = prose(e.text || e.command || "")
      return what ? `${mark} ${tool}: ${what}` : `${mark} ${tool}`
    }
    default:
      return prose(e.text)
  }
}

// Blocks joined with blank lines, as many as one text update takes.
function within(blocks: string[]): string {
  let body = ""
  for (const block of blocks) {
    const next = body ? `${body}\n\n${block}` : block
    if (next.length > TEXT_UPDATE_LIMIT) break
    body = next
  }
  return body
}

// The agent page: what the agent asks for, then what it said and did,
// newest first. `read` is undefined until the first answer.
export function agentPage(agent: Agent, read: ConversationResult | undefined): Page {
  const hint = "Tap: dictate a message · Double tap: back"
  if (!read) return { title: title(agent), body: "Reading the conversation…", hint }
  if (!read.ok) return { title: title(agent), body: read.problem, hint }

  const { permission, question, entries } = read.conversation
  const blocks: string[] = []
  if (permission) {
    const what = permission.command ? `${permission.tool}: ${prose(permission.command)}` : permission.tool
    blocks.push([`● Asks to use ${what}`, permission.reason ? prose(permission.reason) : ""].filter(Boolean).join("\n"))
  }
  if (question) {
    blocks.push([`● Asks: ${prose(question.text)}`, ...question.options.map((o, i) => `  ${i + 1}. ${prose(o)}`)].join("\n"))
  }
  blocks.push(...entries.slice().reverse().map(entry))
  return { title: title(agent), body: within(blocks) || "Nothing said yet.", hint }
}

function minutes(seconds: number): string {
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`
}

// The agent page while the operator dictates a message to the agent.
export function dictationPage(agent: Agent, d: Dictation): Page {
  const cancel = "Double tap: cancel"
  switch (d.step) {
    case "listening":
      return {
        title: `● Listening · ${agent.task_id}`,
        body: `Speak your message to the agent.\n\n${minutes(d.seconds)}`,
        hint: `Tap: stop · ${cancel}`,
      }
    case "transcribing":
      return { title: `Message · ${agent.task_id}`, body: "Transcribing…", hint: cancel }
    case "review":
      return { title: `Message · ${agent.task_id}`, body: `“${d.text}”`, hint: `Tap: send · ${cancel}` }
    case "sending":
      return { title: `Message · ${agent.task_id}`, body: `“${d.text}”\n\nSending…`, hint: "" }
    case "sent":
      return { title: `Message · ${agent.task_id}`, body: `Sent to ${agent.task_id}.`, hint: "" }
    case "problem":
      return { title: `Message · ${agent.task_id}`, body: d.problem, hint: "Tap: try again · Double tap: back" }
  }
}
