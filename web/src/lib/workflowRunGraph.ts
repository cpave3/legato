// What a run says of each node of its workflow graph: the run state of each
// step, the trigger that started it. The graph component draws it.
import type { ReviewRunDetails, WorkflowRunView, WorkflowStepRun } from "./workflow"
import type { GraphNodeExtra, NodeRunState, WorkflowGraphModel } from "./workflowGraph"
import { elapsed } from "./workflowView"
import { stepPathOf } from "./workflowRunTree"

function parse(raw: string | undefined): Record<string, unknown> {
  try { const value = JSON.parse(raw ?? ""); return value && typeof value === "object" && !Array.isArray(value) ? value : {} } catch { return {} }
}

/** templatePath is a step instance's path without its passes, the path of
 * its graph node (graph version 2): each/1/write is each/write. A nested
 * scope adds a pass and an ID, so the passes are every other part. */
export function templatePath(path: string): string {
  return path.split("/").filter((_, i) => i % 2 === 0).join("/")
}

/** instancesOf are the run's step instances of a graph node's path: one
 * for a step of the run's own, one per pass for a loop's body's, in the
 * order they ran (every pass on the path, outermost first). */
export function instancesOf(view: WorkflowRunView, nodePath: string): WorkflowStepRun[] {
  return (view.steps ?? []).filter(step => templatePath(stepPathOf(step)) === nodePath).sort((a, b) => comparePasses(passesOf(stepPathOf(a)), passesOf(stepPathOf(b))))
}

/** passesOf are the passes on a step instance's path, outermost first:
 * outer/1/inner/0/build is [1, 0]. */
function passesOf(path: string): number[] {
  return path.split("/").filter((_, i) => i % 2 === 1).map(Number)
}

// comparePasses orders two instances as they ran: by the outermost pass,
// then the next, so a nested loop's newest instance comes last.
function comparePasses(a: number[], b: number[]): number {
  for (let i = 0; i < Math.max(a.length, b.length); i++) {
    const d = (a[i] ?? -1) - (b[i] ?? -1)
    if (d !== 0) return d
  }
  return 0
}

// instanceState maps a step instance, its attempts and its executor's
// details to the state a node shows.
function instanceState(view: WorkflowRunView, step: WorkflowStepRun | undefined): NodeRunState {
  if (!step) return "waiting"
  const path = stepPathOf(step)
  const attempts = (view.attempts ?? []).filter(attempt => stepPathOf(attempt) === path)
  const latest = attempts[attempts.length - 1]
  switch (step.status) {
    case "pending": return "waiting"
    case "waiting_manual": return "needs"
    case "failed": return "failed"
    case "succeeded": return "done"
    case "skipped": case "cancelled": return "skipped"
    case "suspended": return "suspended"
    case "paused": return "needs"
  }
  if (latest?.status === "waiting_agent" && latest.needs_attention) return "needs"
  if (step.executor_type === "github.pr.review_passes") {
    const details = view.details?.[path] as ReviewRunDetails | undefined
    if (details?.needs_you && (details.phase === "waiting_for_author" || details.phase === "stuck")) return "needs"
  }
  return "running"
}

// passState is how a pass went, from its steps' states: what needs the
// operator or failed first, then what runs, else done.
function passState(states: NodeRunState[]): NodeRunState {
  for (const state of ["needs", "failed", "running", "suspended"] as const) if (states.includes(state)) return state
  if (states.length > 0 && states.every(state => state === "skipped")) return "skipped"
  if (states.some(state => state === "waiting")) return "running"
  return "done"
}

export function runNodeExtras(view: WorkflowRunView & { trigger_kind?: string }, graph: WorkflowGraphModel, now: number): Record<string, GraphNodeExtra> {
  const out: Record<string, GraphNodeExtra> = {}
  const started = `trigger:${view.trigger_kind || "manual"}`
  for (const node of graph.nodes) {
    if (node.kind === "trigger") { out[node.id] = { trigger: node.id === started ? "started" : "other" }; continue }
    if (node.kind !== "step" || !node.step) continue
    const nodePath = node.step.path || node.step.id
    const instances = instancesOf(view, nodePath)
    const step = instances[instances.length - 1]
    const path = step ? stepPathOf(step) : nodePath
    const attempts = (view.attempts ?? []).filter(attempt => stepPathOf(attempt) === path)
    const agent = [...attempts].reverse().find(attempt => attempt.agent_kind)?.agent_kind
    const extra: GraphNodeExtra = { state: instanceState(view, step), attempts: attempts.length, agent }
    if (step?.started_at) extra.elapsed = elapsed(step.started_at, step.completed_at ?? now)
    if (step && step.pass != null) extra.pass = `Pass ${step.pass + 1}`
    if (node.step.group === "review_loop") {
      const details = view.details?.[path] as ReviewRunDetails | undefined
      const most = Number(parse(step?.resolved_inputs_json).max_passes)
      if (details?.pass) extra.pass = most > 0 ? `Pass ${details.pass} of ${most}` : `Pass ${details.pass}`
    }
    if (node.step.group === "loop" && step) {
      // Each pass's state, from its steps' (LEG-484).
      const byPass = new Map<number, NodeRunState[]>()
      for (const row of view.steps ?? []) {
        if (row.pass == null || !row.parent_path || row.parent_path !== `${path}/${row.pass}`) continue
        byPass.set(row.pass, [...(byPass.get(row.pass) ?? []), instanceState(view, row)])
      }
      extra.passes = [...byPass.keys()].sort((a, b) => a - b).map(pass => ({ pass, state: passState(byPass.get(pass)!) }))
      const most = node.step.loop?.max
      if (extra.passes.length > 0) extra.pass = most ? `Pass ${extra.passes.length} of ${most}` : `Pass ${extra.passes.length}`
    }
    out[node.id] = extra
  }
  return out
}
