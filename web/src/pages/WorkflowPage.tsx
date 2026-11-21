import { useMemo, useState } from "react"
import { Link, useParams } from "react-router-dom"
import { ArrowLeft, Play } from "lucide-react"
import { useWorkflows, useWorkflowRunsOf } from "../hooks/useWorkflow"
import { useNow } from "../hooks/useNow"
import { ago, fileWorkflowName, runState } from "../lib/workflowView"
import { RunWorkflowDialog } from "../components/RunWorkflowDialog"
import { StatePill } from "../components/WorkflowRunStatus"
import { LazyWorkflowGraph } from "../components/workflow-graph/LazyWorkflowGraph"
import { TriggerSwitch } from "../components/WorkflowTriggerSwitch"
import { gateLabel, redactSecrets, secretValues, stepTypeLabel, triggerName, yamlText, type GraphNode, type GraphNodeExtra } from "../lib/workflowGraph"
import { StepYaml } from "../components/StepYaml"
import type { WorkflowDefinition, WorkflowTriggerState } from "../lib/workflow"
import { Button } from "../components/ui/Button"
import { LoadingState } from "../components/ui/LoadingState"
import { cn } from "../lib/utils"
import { useMediaQuery } from "../hooks/useMediaQuery"
import { NodeSheet } from "../components/NodeSheet"

const RECENT_RUNS = 10

function Frame({ children }: { children: React.ReactNode }) {
  return <main className="flex min-h-0 flex-1 flex-col overflow-y-auto px-4 py-4 font-lw text-lw-text sm:px-6 md:py-6">
    <div className="mx-auto flex w-full max-w-6xl flex-col gap-4">
      <Link to="/workflows?tab=workflows" className="inline-flex items-center gap-1.5 self-start text-sm text-lw-text-2 hover:text-lw-text"><ArrowLeft size={14} />Workflows</Link>
      {children}
    </div>
  </main>
}

function TriggerPanel({ definition, node, state, onChanged }: { definition: WorkflowDefinition; node: GraphNode; state?: WorkflowTriggerState; onChanged: () => Promise<unknown> }) {
  const trigger = node.trigger!
  const summary = trigger.summary || triggerName(trigger.kind)
  return <aside aria-label={summary} className="flex flex-col gap-3 rounded-xl border border-lw-line bg-lw-surface p-4 text-sm">
    <div>
      <p className="text-[11px] font-medium uppercase tracking-wide text-lw-text-2">Trigger · {triggerName(trigger.kind)}</p>
      <h2 className="mt-0.5 text-[15px] font-semibold">{summary}</h2>
    </div>
    {trigger.kind === "manual"
      ? <p className="text-lw-text-2">Run this workflow yourself with <span className="text-lw-text">Run</span>, from a card or through the API.</p>
      : <>
        <ul className="flex flex-col gap-1 text-[13px] text-lw-text-2">
          <li>{trigger.confirm ? "Asks you before each run" : "Starts without asking"}</li>
          {trigger.max_runs_per_day ? <li>Loop stop: {trigger.max_runs_per_day} runs per item per day</li> : null}
          <li>The file says {trigger.enabled ? "on" : "paused"}</li>
        </ul>
        {state && <TriggerSwitch definition={definition} state={state} onChanged={onChanged} />}
        {trigger.definition && <pre className="overflow-x-auto rounded-lg border border-lw-line bg-lw-ground p-3 font-lw-mono text-xs text-lw-text">{yamlText(trigger.definition)}</pre>}
      </>}
  </aside>
}

function StepPanel({ definition, node }: { definition: WorkflowDefinition; node: GraphNode }) {
  const step = node.step!
  const yaml = yamlText(redactSecrets(step.definition, secretValues(definition.inputs)))
  return <aside aria-label={step.title} className="flex min-w-0 flex-col gap-3 rounded-xl border border-lw-line bg-lw-surface p-4 text-sm">
    <div>
      <p className="text-[11px] font-medium uppercase tracking-wide text-lw-text-2">Step · {stepTypeLabel(step.type)}</p>
      <h2 className="mt-0.5 text-[15px] font-semibold">{step.title}</h2>
      {step.gate && <p className="mt-1 text-[13px] text-lw-needs">{gateLabel(step.gate)}</p>}
    </div>
    <StepYaml yaml={yaml} source={definition.source} />
  </aside>
}

// WorkflowPage is one workflow by name: what it is, its workflow graph, Run,
// and its recent runs.
export function WorkflowPage() {
  const { name = "" } = useParams()
  const definitions = useWorkflows()
  const runs = useWorkflowRunsOf(name, RECENT_RUNS)
  const now = useNow(30_000)
  const [running, setRunning] = useState(false)
  const [selected, setSelected] = useState<string | null>(null)
  // At phone width the graph runs top to bottom and details open in a sheet.
  const narrow = useMediaQuery("(max-width: 767px)")
  const triggerStates = definitions.data?.trigger_states?.[name]
  const extras = useMemo(() => {
    const out: Record<string, GraphNodeExtra> = {}
    for (const state of triggerStates ?? []) out[`trigger:${state.trigger}`] = { enabledHere: state.enabled }
    return out
  }, [triggerStates])
  if (definitions.loading && !definitions.data) return <Frame><LoadingState label="Loading the workflow…" size="inline" /></Frame>
  if (definitions.error && !definitions.data) return <Frame><p role="alert" className="text-sm text-lw-error">Could not load workflows: {definitions.error}</p></Frame>
  const definition = (definitions.data?.definitions ?? []).find(item => item.name === name)
  if (!definition) {
    const failure = (definitions.data?.errors ?? []).find(error => fileWorkflowName(error.path) === name)
    if (failure) return <Frame>
      <h1 className="text-xl font-semibold">{name}</h1>
      <p className="font-lw-mono text-xs text-lw-faint">{failure.path}</p>
      <p role="alert" className="break-words rounded-lg border border-lw-danger-line bg-lw-error-soft p-3 text-sm text-lw-danger">This workflow's file can't be parsed, so there is no graph: {failure.error}</p>
    </Frame>
    return <Frame><p className="text-sm text-lw-text-2">No workflow of that name</p></Frame>
  }
  const selectedNode = definition.graph?.nodes.find(node => node.id === selected)
  const panel = selectedNode?.kind === "step"
    ? <StepPanel definition={definition} node={selectedNode} />
    : selectedNode?.kind === "trigger"
      ? <TriggerPanel definition={definition} node={selectedNode} state={triggerStates?.find(state => `trigger:${state.trigger}` === selectedNode.id)} onChanged={definitions.refresh} />
      : null
  const panelTitle = selectedNode?.step?.title ?? selectedNode?.trigger?.summary ?? ""
  const recent = runs.data ?? []
  return <Frame>
    <header className="flex flex-wrap items-start justify-between gap-3">
      <div className="min-w-0">
        <h1 className="text-xl font-semibold">{definition.title}</h1>
        {definition.description && <p className="mt-1 text-sm text-lw-text-2">{definition.description}</p>}
        <p className="mt-1 break-all font-lw-mono text-xs text-lw-faint">{definition.source}</p>
      </div>
      <Button variant="outline" size="sm" aria-label={`Run ${definition.title}`} onClick={() => setRunning(true)}><Play size={13} />Run</Button>
    </header>
    <div className={cn("grid gap-4", selectedNode && !narrow && "lg:grid-cols-[minmax(0,1fr)_22rem]")}>
      <section aria-label="Workflow graph" className={cn("overflow-hidden rounded-xl border border-lw-line bg-lw-ground", narrow ? "h-[32rem]" : "h-[28rem]")}>
        {definition.graph ? <LazyWorkflowGraph graph={definition.graph} selectedId={selected} onSelect={setSelected} extras={extras} direction={narrow ? "TB" : "LR"} /> : <p className="p-4 text-sm text-lw-text-2">This Legato sent no graph for this workflow.</p>}
      </section>
      {selectedNode && !narrow && panel}
    </div>
    {selectedNode && narrow && <NodeSheet title={panelTitle} onClose={() => setSelected(null)}>{panel}</NodeSheet>}
    <section>
      <h2 className="mb-2 text-xs font-semibold uppercase tracking-wide text-lw-text-2">Recent runs</h2>
      {recent.length === 0
        ? <p className="text-sm text-lw-text-2">{runs.loading ? "Loading runs…" : "No runs yet"}</p>
        : <ul aria-label="Recent runs" className="overflow-hidden rounded-xl border border-lw-line bg-lw-surface">
          {recent.map(run => {
            const state = runState(run)
            return <li key={run.id} className="border-t border-lw-line first:border-t-0">
              <Link to={`/workflows/runs/${encodeURIComponent(run.id)}`} className="flex items-center gap-3 px-4 py-2.5 hover:bg-lw-raised">
                <StatePill tone={state.tone} label={state.label} />
                <span className="min-w-0 flex-1 truncate text-sm">{run.workflow_title}</span>
                <span className="font-lw-mono text-xs text-lw-faint" title={run.created_at}>{ago(run.created_at, now)}</span>
              </Link>
            </li>
          })}
        </ul>}
    </section>
    {running && <RunWorkflowDialog definition={definition} repositories={definitions.data?.repositories ?? []} onClose={() => setRunning(false)} />}
  </Frame>
}
