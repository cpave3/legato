import type { AgentInfo, BackgroundJob } from "./agentWire"
import { apiFetch } from "./api"
import { legatoEventSummary, parseLegatoMessage } from "./legatoMessage"

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
      // An event (a change set ran, a restart) is its one-line summary.
      const text = relayed.event ? legatoEventSummary(relayed.label, relayed.body) : relayed.body
      items.push({ kind: "relayed", from: `Legato ${relayed.label}`, text })
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

// An agent that failed needs the person as much as one that asks, and so
// does one with an open question or a plan waiting, whatever it is doing:
// the server's glance says so (a done note does not). The rest follow what
// the agent is doing.
export function ensembleSection(agent: Pick<AgentInfo, "activity" | "glance">): EnsembleSection {
  if (agent.glance === "needs_you") return "needs-you"
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

// What an agent in the background waits on: its one job by kind and label;
// else its subagents (at any depth) as subagentsLabel counts them, and how
// many jobs of its own run. A job a listed subagent started is the
// subagent's, not the agent's.
export function backgroundSummary(jobs: BackgroundJob[] | undefined): string {
  if (!jobs || jobs.length === 0) return ""
  if (jobs.length === 1) return `${jobs[0].kind}: ${jobs[0].label}`
  const subagents = subagentsLabel(jobs)
  if (!subagents) return `${jobs.length} jobs`
  const ids = new Set(jobs.filter((job) => job.kind === "subagent").map((job) => job.id))
  const own = jobs.filter((job) => job.kind !== "subagent" && !(job.parent && ids.has(job.parent))).length
  return own > 0 ? `${subagents} · ${plural(own, "job")}` : subagents
}

function plural(n: number, noun: string): string {
  return n === 1 ? `1 ${noun}` : `${n} ${noun}s`
}

// SubagentCounts counts an agent's subagents at any depth by what they do:
// working, needing the operator, or waiting (their turn ended and work they
// started still runs).
export interface SubagentCounts {
  total: number
  working: number
  needsYou: number
  waiting: number
  // Whether every waiting one waits on at least one shell.
  waitingOnShells: boolean
}

export function subagentCounts(nodes: SubagentNode[]): SubagentCounts {
  const waiting = nodes.filter((node) => node.job.state === "background")
  return {
    total: nodes.length,
    working: nodes.filter((node) => node.job.state !== "background" && node.job.state !== "waiting").length,
    needsYou: nodes.filter((node) => node.job.state === "waiting").length,
    waiting: waiting.length,
    waitingOnShells: waiting.length > 0 && waiting.every((node) => node.shells.length > 0),
  }
}

// subagentsLabel says how many subagents an agent runs and what they do:
// "7 sub-agents · 1 working · 6 waiting on shells", or for one,
// "1 sub-agent · waiting on 3 shells"; "" for none.
export function subagentsLabel(jobs: BackgroundJob[] | undefined): string {
  const nodes = subagentTree(jobs)
  if (nodes.length === 0) return ""
  if (nodes.length === 1) return `1 sub-agent · ${subagentDoing(nodes[0])}`
  const counts = subagentCounts(nodes)
  const parts = [`${counts.total} sub-agents`]
  if (counts.working > 0) parts.push(`${counts.working} working`)
  if (counts.needsYou > 0) parts.push(`${counts.needsYou} ${counts.needsYou === 1 ? "needs" : "need"} you`)
  if (counts.waiting > 0) parts.push(`${counts.waiting} ${counts.waitingOnShells ? "waiting on shells" : "waiting"}`)
  return parts.join(" · ")
}

// subagentDoing is what one subagent does, in a few words.
export function subagentDoing(node: SubagentNode): string {
  switch (node.job.state) {
    case "background":
      return subagentWaitsOn(node)
    case "waiting":
      return "needs you"
    default:
      return "working"
  }
}

// subagentWaitsOn says what a subagent whose turn ended waits on: "waiting
// on 3 shells", "waiting on 1 shell · 1 sub-agent"; "finishing" when nothing
// it started is listed.
export function subagentWaitsOn(node: SubagentNode): string {
  const on = [
    node.shells.length > 0 && plural(node.shells.length, "shell"),
    node.subagents > 0 && plural(node.subagents, "sub-agent"),
    node.jobs > 0 && plural(node.jobs, "job"),
  ].filter(Boolean).join(" · ")
  return on ? `waiting on ${on}` : "finishing"
}

// SubagentNode is one subagent in an agent's tree, at depth 1 for one the
// agent started.
export interface SubagentNode {
  job: BackgroundJob
  depth: number
  // What it started that still runs: its shells, how many subagents, and
  // how many other jobs (monitors, crons).
  shells: BackgroundJob[]
  subagents: number
  jobs: number
}

// subagentTree lists an agent's subagents depth first, each under the one
// that started it; one whose parent is not listed is the agent's own.
export function subagentTree(jobs: BackgroundJob[] | undefined): SubagentNode[] {
  if (!jobs) return []
  const ids = new Set(jobs.map((job) => job.id))
  const parentOf = (job: BackgroundJob) => job.parent && ids.has(job.parent) && job.parent !== job.id ? job.parent : ""
  const out: SubagentNode[] = []
  const seen = new Set<string>()
  const walk = (parent: string, depth: number) => {
    for (const job of jobs) {
      if (job.kind !== "subagent" || parentOf(job) !== parent || seen.has(job.id)) continue
      seen.add(job.id)
      const children = jobs.filter((child) => parentOf(child) === job.id)
      out.push({
        job,
        depth,
        shells: children.filter((c) => c.kind === "bash"),
        subagents: children.filter((c) => c.kind === "subagent").length,
        jobs: children.filter((c) => c.kind !== "subagent" && c.kind !== "bash").length,
      })
      walk(job.id, depth + 1)
    }
  }
  walk("", 1)
  return out
}
