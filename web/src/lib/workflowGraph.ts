// The workflow graph as the server builds it (internal/engine/workflow
// graph.go): the drawing of a workflow's triggers and steps, built from its
// definition. Read-only; the YAML is the source of truth. This file holds
// the types and words only, so pages can use them without loading the
// graph library.
import { dump } from "js-yaml"
import type { WorkflowStepDefinition } from "./workflow"

export type GraphTrigger = { kind: string; summary: string; enabled: boolean; confirm?: boolean; max_runs_per_day?: number; definition?: Record<string, unknown> }
export type GraphStep = { id: string; title: string; type: string; trigger: "automatic" | "manual" | "agent_ended"; key: { field?: string; value?: string }; definition: WorkflowStepDefinition }
export type GraphNode = { id: string; kind: "trigger" | "step"; trigger?: GraphTrigger; step?: GraphStep }
export type GraphEdge = { id: string; kind: "start" | "order" | string; source: string; target: string }
export type WorkflowGraphModel = { version: number; nodes: GraphNode[]; edges: GraphEdge[] }

// GraphNodeExtra is what a page knows of a node beyond its definition: for
// a trigger, whether it is on in this Legato.
export type GraphNodeExtra = { enabledHere?: boolean }

// The short label of each executor type, as a step node shows it.
const stepTypeLabels: Record<string, string> = {
  "agent.run": "Agent",
  "agent.dispatch": "Send to agent",
  "agent.interview": "Interview",
  "agent.stop": "Stop agent",
  "task.note": "Note",
  "task.move": "Move card",
  "task.context": "Read card",
  "task.metadata.set": "Mark card",
  "task.working_dir.resolve": "Find folder",
  "github.pr.prepare": "Check out PR",
  "github.pr.review_passes": "Review in passes",
  "repo.ensure": "Clone repository",
  "worktree.remove": "Remove worktree",
  "file.remove": "Remove file",
  "chimera.session.remove": "End Chimera session",
  "learning.packet.prepare": "Gather learnings",
  "learning.proposals.submit": "Submit learnings",
}

export function stepTypeLabel(type: string): string {
  return stepTypeLabels[type] ?? type
}

const triggerNames: Record<string, string> = {
  manual: "Run by hand",
  "task.entered_column": "Card enters a column",
  schedule: "Schedule",
  "pr.changes_requested": "Changes requested on a PR",
}

export function triggerName(kind: string): string {
  return triggerNames[kind] ?? kind
}

// nodeName is a node's accessible name: "Step: Investigate the task".
export function nodeName(node: GraphNode, extra?: GraphNodeExtra): string {
  if (node.kind === "step" && node.step) return `Step: ${node.step.title}`
  const trigger = node.trigger
  const summary = trigger?.summary || triggerName(trigger?.kind ?? "")
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
