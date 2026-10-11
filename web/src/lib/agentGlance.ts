import type { AgentInfo } from "../hooks/useWebSocket"
import { apiFetch } from "./api"
import { parseLegatoMessage } from "./legatoMessage"

// How many entries of a conversation are read for a tile. It is more than a
// tile shows: long runs of tool calls are cut to their newest, and what was
// said before them is worth reaching.
export const GLANCE_ENTRIES = 40

// How many tool calls of one run a tile shows.
const SHOWN_OF_RUN = 3

// One thing said or done in a native agent's conversation, as
// GET /api/agents/{task_id}/conversation returns it.
export interface ConversationEntry {
  kind: "user" | "assistant" | "tool"
  // The message, or for a tool call one line that says what the call is for.
  text: string
  tool?: string
  // The command itself, when the call runs one.
  command?: string
  state?: "running" | "done" | "failed"
}

export interface AgentConversation {
  task_id: string
  status: string
  // Oldest first.
  entries: ConversationEntry[]
  permission: { tool: string; command?: string; reason?: string; plan?: string } | null
  question: { text: string; options: string[] } | null
  context_used: number
  context_window: number
}

// How a tool call stands. A call with no result yet is being run, waits to be
// allowed, or was cut short, depending on what its agent is doing.
export type ToolState = "done" | "failed" | "running" | "waiting" | "stopped"

// What a tile shows of a conversation.
export type GlanceItem =
  | { kind: "person"; text: string }
  // Sent by Legato itself or by another agent, not typed by the person.
  | { kind: "relayed"; from: string; text: string }
  | { kind: "said"; text: string; newest: boolean }
  | { kind: "tool"; tool: string; text: string; state: ToolState }
  // The tool calls of a run that are not shown. The count is null when the
  // run began before what was read.
  | { kind: "earlier"; count: number | null }

// A plugin's tool without the plugin's prefix.
function toolName(name: string): string {
  const cut = name.lastIndexOf("__")
  return cut >= 0 && cut + 2 < name.length ? name.slice(cut + 2) : name
}

function toolItem(entry: ConversationEntry, activity: string): GlanceItem {
  const item = { kind: "tool" as const, tool: toolName(entry.tool ?? ""), text: entry.text }
  if (entry.state !== "running") return { ...item, state: entry.state === "failed" ? "failed" : "done" }
  switch (activity) {
    case "working":
    case "background":
      return { ...item, state: "running" }
    case "waiting":
      // The call waits to be allowed. What is to be allowed is the command,
      // not the agent's words for it.
      return { ...item, state: "waiting", text: entry.command || entry.text }
    default:
      return { ...item, state: "stopped" }
  }
}

// glanceItems turns the end of a conversation into what a tile shows, oldest
// first. activity is what the agent is doing, which says what a call with no
// result is waiting for. A run of tool calls is cut to its newest few.
export function glanceItems(entries: ConversationEntry[], activity: string): GlanceItem[] {
  const newestSaid = entries.findLastIndex((entry) => entry.kind === "assistant")
  const items: GlanceItem[] = []
  for (let index = 0; index < entries.length; index++) {
    const entry = entries[index]
    if (entry.kind === "tool") {
      let end = index
      while (end < entries.length && entries[end].kind === "tool") end++
      const hidden = Math.max(end - index - SHOWN_OF_RUN, 0)
      if (hidden > 0) {
        // A run that began before what was read has calls nobody counted.
        const counted = index > 0 || entries.length < GLANCE_ENTRIES
        items.push({ kind: "earlier", count: counted ? hidden : null })
      }
      for (let call = index + hidden; call < end; call++) items.push(toolItem(entries[call], activity))
      index = end - 1
      continue
    }
    if (entry.kind === "assistant") {
      items.push({ kind: "said", text: entry.text, newest: index === newestSaid })
      continue
    }
    const relayed = parseLegatoMessage(entry.text)
    if (!relayed) {
      items.push({ kind: "person", text: entry.text })
    } else if (relayed.peer !== undefined) {
      items.push({ kind: "relayed", from: relayed.peer ? `Agent ${relayed.peer}` : "Agent", text: relayed.body })
    } else {
      items.push({ kind: "relayed", from: `Legato ${relayed.label}`, text: relayed.body })
    }
  }
  return items
}

// plainText is a message without what Markdown puts around its words: heading
// marks, code fences, emphasis and blank lines. A tile shows a few lines of a
// message, which is not room for a rendered document.
export function plainText(markdown: string): string {
  return markdown
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => !line.startsWith("```"))
    .map((line) => line.replace(/^#+\s*/, "").replaceAll("**", "").replaceAll("`", "").trim())
    .filter(Boolean)
    .join("\n")
}

// What an agent is doing, in the order the ensemble lists agents.
export type EnsembleSection = "needs-you" | "running" | "background" | "idle"

const sectionOrder: EnsembleSection[] = ["needs-you", "running", "background", "idle"]

// An agent that failed needs the person as much as one that asks.
export function ensembleSection(agent: Pick<AgentInfo, "activity">): EnsembleSection {
  switch (agent.activity) {
    case "waiting":
    case "error":
      return "needs-you"
    case "working":
      return "running"
    case "background":
      return "background"
    default:
      return "idle"
  }
}

// orderForEnsemble lists agents by what they are doing, the ones that need
// the person first. Within one state they keep the order they came in.
export function orderForEnsemble(agents: AgentInfo[]): AgentInfo[] {
  const rank = (agent: AgentInfo) => sectionOrder.indexOf(ensembleSection(agent))
  return agents
    .map((agent, index) => ({ agent, index }))
    .sort((a, b) => rank(a.agent) - rank(b.agent) || a.index - b.index)
    .map(({ agent }) => agent)
}

// contextShare is how full an agent's context is, as a whole percentage, or
// null when its endpoint did not say how large the context is.
export function contextShare(conversation: Pick<AgentConversation, "context_used" | "context_window">): number | null {
  if (conversation.context_window <= 0) return null
  return Math.min(Math.max(Math.floor((conversation.context_used * 100) / conversation.context_window), 0), 100)
}

// fetchAgentConversation reads the end of a native agent's conversation.
export async function fetchAgentConversation(baseUrl: string, taskId: string): Promise<AgentConversation> {
  const response = await apiFetch(baseUrl, `/api/agents/${encodeURIComponent(taskId)}/conversation?limit=${GLANCE_ENTRIES}`)
  if (!response.ok) throw new Error((await response.text()).trim() || "Could not read the agent's conversation")
  return await response.json() as AgentConversation
}
