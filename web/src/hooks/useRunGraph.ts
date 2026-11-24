// What a run's workflow graph shows of the run: the run page and the agents
// graph's run panel draw it from here, so the two never disagree.
import { useMemo } from "react"
import type { WorkflowRunView } from "../lib/workflow"
import type { GraphNodeExtra } from "../lib/workflowGraph"
import { runNodeExtras } from "../lib/workflowRunGraph"
import { runNeedsYou } from "../lib/runNeedsYou"
import { secretInputNames } from "../lib/workflowView"
import { useApprovals } from "./useApprovals"
import { useAttention } from "./useAttention"

// agentLink opens a run's agent on the agents page, which then links back.
export function agentLink(taskId: string, runId: string): string {
  return `/agents?task=${encodeURIComponent(taskId)}&workflow=${encodeURIComponent(runId)}`
}

// graphLink shows a run's agent on the agents graph, with its run's graph.
export function graphLink(taskId: string, runId: string): string {
  return `/agents?view=graph&task=${encodeURIComponent(taskId)}&workflow=${encodeURIComponent(runId)}`
}

function parseJSON(raw: string | undefined): unknown {
  try { return JSON.parse(raw ?? "") } catch { return null }
}

// The values of a run's secret inputs, which nothing shows, even where a
// step's inputs or outputs repeat them.
export function secretValues(view: WorkflowRunView): string[] {
  const names = secretInputNames(view.run.definition_json)
  const inputs = parseJSON(view.run.inputs_json)
  if (!names.size || !inputs || typeof inputs !== "object") return []
  return [...names].map(name => (inputs as Record<string, unknown>)[name]).filter((value): value is string => typeof value === "string" && value !== "")
}

// useRunGraphExtras is the run state of each node of a run's graph, and what
// of the run needs the operator: an open approval or item of a step's agent
// makes its step Needs you.
export function useRunGraphExtras(runId: string, data: WorkflowRunView | null | undefined, now: number) {
  const approvalList = useApprovals(true).data
  const attentionList = useAttention().data
  const graph = data?.graph ?? null
  const needsYou = useMemo(() => data ? runNeedsYou(data, attentionList ?? [], approvalList, taskId => agentLink(taskId, runId), secretValues(data)) : { rows: [], nodes: new Set<string>() }, [data, attentionList, approvalList, runId])
  const extras = useMemo((): Record<string, GraphNodeExtra> | undefined => {
    if (!data || !graph) return undefined
    const out = runNodeExtras(data, graph, now)
    for (const node of needsYou.nodes) if (out[node] && (out[node].state === "running" || out[node].state === "waiting")) out[node] = { ...out[node], state: "needs" }
    return out
  }, [data, graph, now, needsYou])
  return { graph, needsYou, extras }
}
