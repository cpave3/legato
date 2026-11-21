// What a run says of each node of its workflow graph: the run state of each
// step, the trigger that started it. The graph component draws it.
import type { ReviewRunDetails, WorkflowRunView } from "./workflow"
import type { GraphNodeExtra, NodeRunState, WorkflowGraphModel } from "./workflowGraph"
import { elapsed } from "./workflowView"

function parse(raw: string | undefined): Record<string, unknown> {
  try { const value = JSON.parse(raw ?? ""); return value && typeof value === "object" && !Array.isArray(value) ? value : {} } catch { return {} }
}

// stepRunState maps a step, its attempts and its executor's details to the
// state a node shows.
function stepRunState(view: WorkflowRunView, stepId: string): NodeRunState {
  const step = (view.steps ?? []).find(item => item.step_id === stepId)
  if (!step) return "waiting"
  const attempts = (view.attempts ?? []).filter(attempt => attempt.step_id === stepId)
  const latest = attempts[attempts.length - 1]
  switch (step.status) {
    case "pending": return "waiting"
    case "waiting_manual": return "needs"
    case "failed": return "failed"
    case "succeeded": return "done"
    case "skipped": case "cancelled": return "skipped"
  }
  if (latest?.status === "waiting_agent" && latest.needs_attention) return "needs"
  if (step.executor_type === "github.pr.review_passes") {
    const details = view.details?.[stepId] as ReviewRunDetails | undefined
    if (details?.phase === "waiting_for_author" || details?.phase === "stuck") return "needs"
  }
  return "running"
}

export function runNodeExtras(view: WorkflowRunView & { trigger_kind?: string }, graph: WorkflowGraphModel, now: number): Record<string, GraphNodeExtra> {
  const out: Record<string, GraphNodeExtra> = {}
  const started = `trigger:${view.trigger_kind || "manual"}`
  for (const node of graph.nodes) {
    if (node.kind === "trigger") { out[node.id] = { trigger: node.id === started ? "started" : "other" }; continue }
    if (node.kind !== "step" || !node.step) continue
    const stepId = node.step.id
    const step = (view.steps ?? []).find(item => item.step_id === stepId)
    const attempts = (view.attempts ?? []).filter(attempt => attempt.step_id === stepId)
    const agent = [...attempts].reverse().find(attempt => attempt.agent_kind)?.agent_kind
    const extra: GraphNodeExtra = { state: stepRunState(view, stepId), attempts: attempts.length, agent }
    if (step?.started_at) extra.elapsed = elapsed(step.started_at, step.completed_at ?? now)
    if (node.step.group === "review_loop") {
      const details = view.details?.[stepId] as ReviewRunDetails | undefined
      const most = Number(parse(step?.resolved_inputs_json).max_passes)
      if (details?.pass) extra.pass = most > 0 ? `Pass ${details.pass} of ${most}` : `Pass ${details.pass}`
    }
    out[node.id] = extra
  }
  return out
}
