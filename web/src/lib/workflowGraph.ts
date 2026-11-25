// The workflow graph as the server builds it (internal/engine/workflow
// graph.go): the drawing of a workflow's triggers and steps, built from its
// definition. Read-only; the YAML is the source of truth. This file holds
// the types and words only, so pages can use them without loading the
// graph library.
import { dump } from "js-yaml"
import type { WorkflowStepDefinition } from "./workflow"

export type GraphTrigger = { kind: string; summary: string; enabled: boolean; confirm?: boolean; max_runs_per_day?: number; definition?: Record<string, unknown> }
export type GraphGate = "manual" | "agent_ended"
// A loop step's loop (graph version 2): its form, its most passes, and what
// it goes over or ends on, in words.
export type GraphLoop = { form: "over" | "next" | "repeat"; max: number; summary?: string; until?: string }
// path is the step's path without a pass: its ID, or in a loop's body the
// loop's path, a slash and its ID (each/build). A loop (group "loop") holds
// its body's steps, whose parent is its node.
export type GraphStep = { id: string; path?: string; title: string; type: string; start: "automatic" | "manual" | "agent_ended"; gate?: GraphGate; group?: "review_loop" | "loop"; loop?: GraphLoop; key: { field?: string; value?: string }; definition: WorkflowStepDefinition }
export type GraphNode = { id: string; kind: "trigger" | "step" | "inner"; trigger?: GraphTrigger; step?: GraphStep; inner?: { role: string; title: string }; parent?: string }
// A branch edge leads into a step with if: (condition, in words), or past it
// (skip) to where the run goes when the step is skipped.
export type GraphEdge = { id: string; kind: "start" | "order" | "data" | "loop" | "branch" | string; source: string; target: string; outputs?: string[]; condition?: string; skip?: boolean }
export type WorkflowGraphModel = { version: number; nodes: GraphNode[]; edges: GraphEdge[] }

// GraphNodeExtra is what a page knows of a node beyond its definition: for
// a trigger, whether it is on in this Legato.
export type GraphNodeExtra = {
  enabledHere?: boolean
  // In a run: a step's state, how long it has run, its attempts, its agent
  // tool, and for a loop its pass ("Pass 2 of 5"); a trigger's part in it.
  state?: NodeRunState
  elapsed?: string
  attempts?: number
  agent?: string
  pass?: string
  // passes is, on a loop, how each of its passes went, oldest first.
  passes?: { pass: number; state: NodeRunState }[]
  trigger?: "started" | "other"
}

export type NodeRunState = "waiting" | "running" | "needs" | "failed" | "done" | "skipped" | "suspended"

export const runStateWords: Record<NodeRunState, string> = { waiting: "Waiting", running: "Running", needs: "Needs you", failed: "Failed", done: "Done", skipped: "Skipped", suspended: "Suspended" }

// The short label of each executor type, as a step node shows it.
const stepTypeLabels: Record<string, string> = {
  "agent.run": "Agent",
  "agent.dispatch": "Send to agent",
  "agent.message": "Message agent",
  "agent.interview": "Interview",
  "agent.stop": "Stop agent",
  "agent.compact": "Compact agent",
  "task.note": "Note",
  "task.move": "Move card",
  "task.context": "Read card",
  "task.metadata.set": "Mark card",
  "task.working_dir.resolve": "Find folder",
  "ticket.import": "Import ticket",
  "tickets.list": "List tickets",
  "tasks.list": "List cards",
  "github.pr.prepare": "Check out PR",
  "github.pr.review_passes": "Review in passes",
  "repo.ensure": "Clone repository",
  "worktree.remove": "Remove worktree",
  "file.remove": "Remove file",
  "chimera.session.remove": "End Chimera session",
  "learning.packet.prepare": "Gather learnings",
  "learning.proposals.submit": "Submit learnings",
  "chat.message.send": "Chat message",
  "chat.reaction.add": "Chat reaction",
  "chat.reaction.remove": "Remove reaction",
  "chat.thread.read": "Chat thread",
  "ticket.create": "Create ticket",
  "ticket.comment": "Ticket comment",
  gate: "Ask you",
  loop: "Loop",
  "workflow.run": "Run workflow",
}

export function stepTypeLabel(type: string): string {
  return stepTypeLabels[type] ?? type
}

const triggerNames: Record<string, string> = {
  manual: "Run by hand",
  "task.entered_column": "Card enters a column",
  schedule: "Schedule",
  "pr.changes_requested": "Changes requested on a PR",
  "chat.message": "Chat message",
  "chat.mention": "Bot mentioned",
  "chat.button": "Chat button",
}

export function triggerName(kind: string): string {
  return triggerNames[kind] ?? kind
}

// nodeName is a node's accessible name: "Step: Investigate the task".
export function nodeName(node: GraphNode, extra?: GraphNodeExtra): string {
  if (node.kind === "inner") return node.inner?.title ?? node.id
  if (node.kind === "step" && node.step) return `Step: ${node.step.title}${node.step.gate ? `, ${gateLabel(node.step.gate).toLowerCase()}` : ""}${extra?.state ? `, ${runStateWords[extra.state]}` : ""}`
  const trigger = node.trigger
  const summary = trigger?.summary || triggerName(trigger?.kind ?? "")
  if (extra?.trigger) return `Trigger: ${summary}${extra.trigger === "started" ? ", started this run" : ""}`
  if (!trigger || trigger.kind === "manual") return `Trigger: ${summary}`
  return `Trigger: ${summary}, ${triggerOn(trigger, extra) ? "on here" : "paused here"}`
}

// triggerOn is whether a trigger is on in this Legato: as the page's
// trigger states say, else as its file says.
export function triggerOn(trigger: GraphTrigger, extra?: GraphNodeExtra): boolean {
  return extra?.enabledHere ?? trigger.enabled
}

// yamlText is a value as YAML, for reading in a side panel.
export function yamlText(value: unknown): string {
  try { return dump(value, { lineWidth: 100, noRefs: true }).trimEnd() } catch { return JSON.stringify(value, null, 2) }
}

// keyValueText is a step's key value as a node shows it: each expression
// shortened to what it names, "${{ workflow.inputs.task.id }}" as
// "{task.id}" and "${{ steps.triage.outputs.report.path }}" as
// "{triage.report.path}".
export function keyValueText(value: string): string {
  return value.replace(/\$\{\{\s*(.+?)\s*\}\}/g, (_, expr: string) => `{${expr.replace(/^workflow\.inputs\./, "").replace(/^workflow\./, "").replace(/^steps\.([^.]+)\.outputs\./, "$1.")}}`)
}

// gateLabel says what a gated step waits for.
export function gateLabel(gate: GraphGate): string {
  return gate === "manual" ? "Waits for you" : "Waits for the agent to end"
}

// dataEdgeName is a data edge's accessible name: which outputs of which
// step another step uses.
export function dataEdgeName(edge: GraphEdge, title: (id: string) => string): string {
  return `Uses ${(edge.outputs ?? []).join(", ")} of ${title(edge.source)} in ${title(edge.target)}`
}

export const REDACTED = "••••••"

// secretValues is the values a page must never show: the defaults of the
// definition's secret inputs, and the values a run gave them.
export function secretValues(inputs: Record<string, { secret?: boolean; default?: unknown }> | null | undefined, given?: Record<string, unknown> | null): Set<string> {
  const out = new Set<string>()
  for (const [name, input] of Object.entries(inputs ?? {})) {
    if (!input.secret) continue
    for (const value of [input.default, given?.[name]]) if (typeof value === "string" && value !== "") out.add(value); else if (typeof value === "number") out.add(String(value))
  }
  return out
}

// redactSecrets copies a value with every secret replaced, wherever it is
// found in a string.
export function redactSecrets<T>(value: T, secrets: Set<string>): T {
  if (secrets.size === 0) return value
  const walk = (item: unknown): unknown => {
    if (typeof item === "string") { let text = item; for (const secret of secrets) text = text.split(secret).join(REDACTED); return text }
    if (Array.isArray(item)) return item.map(walk)
    if (item && typeof item === "object") return Object.fromEntries(Object.entries(item).map(([key, nested]) => [key, walk(nested)]))
    return item
  }
  return walk(value) as T
}
