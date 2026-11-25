// What a workflow run waits on the operator for, as the needs-you rail's
// items: the rail's model filtered to the run, plus rows only the run
// knows, built in the same item shape. A run's Needs you list draws them
// with the rail's own row and approval card.
import type { Approval } from "./approvals"
import type { AttentionItem } from "./attention"
import type { GateDetails, MergeWaitDetails, ReviewRunDetails, WorkflowRunView } from "./workflow"
import { stepPathOf } from "./workflowRunTree"
import { templatePath } from "./workflowRunGraph"

// topNode is the graph node a step's path is drawn in: a loop's pass's
// step in its own node, inside its loop's (graph version 2, LEG-484).
const topNode = (path: string) => `step:${templatePath(path)}`

// The actions of the rows only the run knows; the run page carries them out.
// skip and stop are a paused step's; choose:<option> a gate's.
export type RunActionId = "start" | "end_now" | "prompt" | "retry" | "another_pass" | "resume_agent" | "skip" | "stop" | `choose:${string}`

// The step a run row acts on. A worktree removal has no plain button in the
// row: control names the page's existing control for it (trigger or
// retry), which asks first, shows the path and offers force.
export type RunRowStep = { step: string; control?: "trigger" | "retry"; label?: string }

export type RunNeedsYouRow =
  | { kind: "item"; item: AttentionItem; node: string | null; run?: RunRowStep }
  | { kind: "approval"; approval: Approval; node: string | null }

function parse(raw: string | undefined): Record<string, unknown> {
  try { const value = JSON.parse(raw ?? ""); return value && typeof value === "object" ? value : {} } catch { return {} }
}

function runItem(view: WorkflowRunView, step: string, signal: string, key: string, title: string, reason: string, actions: AttentionItem["actions"]): AttentionItem {
  const at = view.run.completed_at || view.run.started_at || view.run.created_at
  return {
    id: `run:${view.run.id}:${key}:${step}`, producer: "run", signal_kind: signal, source_key: `${view.run.id}:${step}`, source_revision: "",
    task_id: view.run.related_task_id || undefined, title, reason, category: "workflow", urgency: "normal",
    navigation: { kind: "workflow_run", target: view.run.id }, actions, status: "open", created_at: at, updated_at: at,
  }
}

const execute = (id: RunActionId, label: string, intent?: string) => ({ id, label, kind: "execute" as const, intent })

// runNeedsYou gathers the rows of a run's Needs you list, each with the
// node of the graph it belongs to (step:<id>), and the nodes an open
// approval or item makes Needs you.
// secrets are the values of the run's secret inputs, hidden in every reason
// the run's own rows show.
export function runNeedsYou(view: WorkflowRunView, attention: AttentionItem[], approvals: Approval[], agentHref: (taskId: string) => string, secrets: string[] = []): { rows: RunNeedsYouRow[]; nodes: Set<string> } {
  const hide = (text: string) => secrets.reduce((current, secret) => current.split(secret).join("••••••"), text)
  const attempts = view.attempts ?? []
  const stepOfTask = new Map<string, string>()
  for (const attempt of attempts) if (attempt.task_id) stepOfTask.set(attempt.task_id, stepPathOf(attempt))
  // A waiting merge's card: its agent's approvals and items are the run's.
  for (const step of view.steps ?? []) {
    const wait = step.executor_type === "github.pr.wait" && step.status === "waiting" ? view.details?.[stepPathOf(step)] as MergeWaitDetails | undefined : undefined
    if (wait?.task_id) stepOfTask.set(wait.task_id, stepPathOf(step))
  }
  // A loop's pass's step is drawn in its loop's node.
  const nodeOf = (taskId?: string) => { const step = taskId ? stepOfTask.get(taskId) : undefined; return step ? topNode(step) : null }
  const rows: RunNeedsYouRow[] = []

  for (const step of view.steps ?? []) {
    const path = stepPathOf(step)
    const node = topNode(path)
    const latest = [...attempts].reverse().find(attempt => stepPathOf(attempt) === path)
    // A step says start; one a run kept before schema version 2 says trigger.
    const stepDefinition = parse(step.definition_json)
    const agentEnded = (stepDefinition.start ?? stepDefinition.trigger) === "agent_ended"
    const cleanup = step.executor_type === "worktree.remove"
    if (step.status === "waiting_manual") {
      const [action, label] = agentEnded ? ["end_now", "End now"] as const : ["start", "Start"] as const
      rows.push({ kind: "item", node, run: cleanup ? { step: path, control: "trigger", label: agentEnded ? label : undefined } : { step: path }, item: agentEnded
        ? runItem(view, path, "workflow_manual_gate", "gate", `${step.title} starts when the agent ends`, "End now to run it without waiting.", cleanup ? [] : [execute(action, label, "primary")])
        : runItem(view, path, "workflow_manual_gate", "gate", `${step.title} waits for you`, "Start it to move the run on.", cleanup ? [] : [execute(action, label, "primary")]) })
    }
    if (latest?.status === "waiting_agent" && latest.needs_attention) {
      const actions: AttentionItem["actions"] = [execute("prompt", "Prompt to continue")]
      if (latest.task_id) actions.unshift({ id: "open_agent", label: "Open agent", kind: "navigate", href: agentHref(latest.task_id) })
      rows.push({ kind: "item", node, run: { step: path }, item: runItem(view, path, "workflow_waiting_agent", "agent", `${step.title}: the agent stopped`, hide(latest.attention_reason || "The agent is waiting or idle."), actions) })
    }
    const details = view.details?.[path] as ReviewRunDetails | undefined
    // The server says whether the loop waits on the operator; a loop whose
    // agent author is fixing the findings does not.
    if (step.status === "waiting" && step.executor_type === "github.pr.review_passes" && details?.needs_you && (details.phase === "waiting_for_author" || details.phase === "stuck")) {
      const stuck = details.phase === "stuck"
      rows.push({ kind: "item", node, run: { step: path }, item: runItem(view, path, stuck ? "workflow_review_stuck" : "workflow_review_author", "review",
        stuck ? `${step.title} is stuck` : `${step.title} waits on its author`,
        hide(details.summary || (stuck ? details.reason ?? "" : `${details.blocking_open} blocking findings to fix or answer.`)), [execute("another_pass", "Run another pass")]) })
    }
    const merge = step.executor_type === "github.pr.wait" && step.status === "waiting" ? details as unknown as MergeWaitDetails | undefined : undefined
    if (merge?.notify && latest?.needs_attention) {
      const actions: AttentionItem["actions"] = [{ id: "open_pr", label: "Open PR", kind: "navigate", href: merge.url || `https://github.com/${merge.repo}/pull/${merge.pr_number}` }]
      if (merge.task_id && merge.agent !== "waiting") actions.push({ id: "resume_agent", label: merge.agent === "running" ? "Send a macro" : "Resume agent", kind: "execute", picks: "macro" })
      if (merge.task_id && merge.agent) actions.push({ id: "open_agent", label: "Open agent", kind: "navigate", href: agentHref(merge.task_id) })
      rows.push({ kind: "item", node, run: { step: path }, item: { ...runItem(view, path, "workflow_merge_waiting", "merge", `Review and merge PR #${merge.pr_number}`, hide(latest.attention_reason || merge.repo), actions), task_id: merge.task_id || view.run.related_task_id || undefined } })
    }
    // A failed step the run went on from (on_failure: continue) needs no one.
    if (step.status === "paused") {
      const link = step.error_link ? ` (${step.error_link})` : ""
      rows.push({ kind: "item", node, run: { step: path }, item: { ...runItem(view, path, "workflow_failed", "paused", `${step.title} failed`, hide((step.error || "") + link),
        [execute("retry", "Retry", "primary"), execute("skip", "Skip"), execute("stop", "Stop")]), urgency: "high" } })
    }
    const gate = step.executor_type === "gate" && step.status === "waiting" ? view.details?.[path] as GateDetails | undefined : undefined
    if (gate) {
      rows.push({ kind: "item", node, run: { step: path }, item: runItem(view, path, "workflow_gate", "gate", gate.title || step.title, hide(gate.body || "Choose to move the run on."),
        (gate.options ?? []).map((option, i) => execute(`choose:${option}`, option, i === 0 ? "primary" : undefined))) })
    }
    // A pass's failed step is its loop's failure: Retry is the loop's.
    if (step.status === "failed" && view.run.status === "failed" && !step.parent_path) {
      rows.push({ kind: "item", node, run: cleanup ? { step: path, control: "retry" } : { step: path }, item: runItem(view, path, "workflow_failed", "failed", `${step.title} failed`, hide(step.error || view.run.error || ""), cleanup ? [] : [execute("retry", "Retry", "primary")]) })
    }
  }

  const tasks = new Set(stepOfTask.keys())
  const nodes = new Set<string>()
  for (const approval of approvals) {
    if (!approval.task_id || !tasks.has(approval.task_id)) continue
    const node = nodeOf(approval.task_id)
    rows.push({ kind: "approval", approval, node })
    if (node) nodes.add(node)
  }
  for (const item of attention) {
    // Approvals are drawn from the approvals API, as the rail does; and the
    // run's own rows above stand for the workflows producer's items of it.
    if (item.producer === "approvals" || item.status !== "open") continue
    if (item.producer === "workflows" && item.source_key.startsWith(`${view.run.id}:`)) continue
    if (!item.task_id || !tasks.has(item.task_id)) continue
    const node = nodeOf(item.task_id)
    rows.push({ kind: "item", item, node })
    if (node) nodes.add(node)
  }
  return { rows, nodes }
}
