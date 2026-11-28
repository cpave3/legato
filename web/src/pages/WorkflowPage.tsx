import { useCallback, useMemo, useState, type ReactNode } from "react"
import { Link, useNavigate, useParams } from "react-router-dom"
import { ArrowLeft, Copy, Pencil, Play } from "lucide-react"
import { useWorkflows, useWorkflowRunsOf } from "../hooks/useWorkflow"
import { useNow } from "../hooks/useNow"
import { ago, fileWorkflowName, inputText, runState, sourceLabel, triggersState } from "../lib/workflowView"
import { RunWorkflowDialog } from "../components/RunWorkflowDialog"
import { StatePill } from "../components/workflow-status/WorkflowRunStatus"
import { TriggerSwitch } from "../components/WorkflowTriggerSwitch"
import { gateLabel, redactSecrets, secretValues, stepTypeLabel, triggerName, yamlText, type GraphNodeExtra } from "../lib/workflowGraph"
import { type GraphNode } from "../lib/workflow"
import { StepYaml } from "../components/StepYaml"
import type { WorkflowDefinition, WorkflowRun, WorkflowTriggerState } from "../lib/workflow"
import { Button } from "../components/ui/Button"
import { LoadingState } from "../components/ui/LoadingState"
import { cn } from "../lib/utils"
import { useMediaQuery } from "../hooks/useMediaQuery"
import { useServer } from "../hooks/useServer"
import { copyBuiltInWorkflow } from "../lib/workflowFiles"
import { WorkflowTagLine } from "../components/workflows/WorkflowTagsDialog"
import { knownTags } from "../lib/workflowList"
import { DockCard, PanelClose } from "../components/workflow-canvas/DockCard"
import { cardTitle, floatingPanel } from "../components/workflow-canvas/canvasStyles"
import { useCardFolds, useLargeScreen } from "../components/workflow-canvas/useCardFolds"
import { CanvasHeader, WorkflowCanvas } from "../components/workflow-canvas/WorkflowCanvas"

const RECENT_RUNS = 10

function Frame({ children }: { children: ReactNode }) {
  return <main className="flex min-h-0 flex-1 flex-col overflow-y-auto px-4 py-4 font-lw text-lw-text sm:px-6 md:py-6">
    <div className="mx-auto flex w-full max-w-6xl flex-col gap-4">
      <Link to="/workflows?tab=workflows" className="inline-flex items-center gap-1.5 self-start text-sm text-lw-text-2 hover:text-lw-text"><ArrowLeft size={14} />Workflows</Link>
      {children}
    </div>
  </main>
}

const panelHead = "text-[11px] font-medium uppercase tracking-wide text-lw-text-2"

function TriggerPanel({ definition, node, state, onChanged, onClose }: { definition: WorkflowDefinition; node: GraphNode; state?: WorkflowTriggerState; onChanged: () => Promise<unknown>; onClose?: () => void }) {
  const trigger = node.trigger!
  const summary = trigger.summary || triggerName(trigger.kind)
  return <aside aria-label={summary} className={cn("flex flex-col gap-3 rounded-xl border border-lw-line bg-lw-surface p-4 text-sm", onClose && floatingPanel)}>
    <div className="flex items-start gap-2">
      <div className="min-w-0 flex-1">
        <p className={panelHead}>Trigger · {triggerName(trigger.kind)}</p>
        <h2 className="mt-0.5 text-[15px] font-semibold">{summary}</h2>
      </div>
      <PanelClose onClose={onClose} />
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

function StepPanel({ definition, node, onClose }: { definition: WorkflowDefinition; node: GraphNode; onClose?: () => void }) {
  const step = node.step!
  const yaml = yamlText(redactSecrets(step.definition, secretValues(definition.inputs)))
  return <aside aria-label={step.title} className={cn("flex min-w-0 flex-col gap-3 rounded-xl border border-lw-line bg-lw-surface p-4 text-sm", onClose && floatingPanel)}>
    <div className="flex items-start gap-2">
      <div className="min-w-0 flex-1">
        <p className={panelHead}>Step · {stepTypeLabel(step.type)}</p>
        <h2 className="mt-0.5 text-[15px] font-semibold">{step.title}</h2>
        {step.gate && <p className="mt-1 text-[13px] text-lw-needs">{gateLabel(step.gate)}</p>}
      </div>
      <PanelClose onClose={onClose} />
    </div>
    <StepYaml yaml={yaml} source={definition.source} />
  </aside>
}

const count = (n: number) => <span className="font-medium text-lw-faint">{n}</span>
const rowButton = (picked: boolean) => cn("flex min-h-8 w-full min-w-0 items-center gap-2 rounded-md px-2 py-1 text-left text-[13px] hover:bg-lw-raised", picked && "bg-lw-selected")

// TriggerList is the workflow's triggers, each with its switch here; a
// row shows its node.
function TriggerList({ definition, triggers, states, selected, onPick, onChanged }: { definition: WorkflowDefinition; triggers: GraphNode[]; states: WorkflowTriggerState[]; selected: string | null; onPick: (node: string) => void; onChanged: () => Promise<unknown> }) {
  return <ul aria-label="Triggers" className="space-y-1 px-1.5 pb-2">
    {triggers.map(node => {
      const trigger = node.trigger!
      const state = states.find(item => `trigger:${item.trigger}` === node.id)
      return <li key={node.id} className="flex flex-wrap items-center gap-x-2">
        <button type="button" onClick={() => onPick(node.id)} className={cn(rowButton(node.id === selected), "flex-1 basis-40")}>
          <span className="min-w-0 flex-1 truncate">{trigger.summary || triggerName(trigger.kind)}</span>
          {trigger.kind !== "manual" && !state && <span className="text-[11px] text-lw-faint">{trigger.enabled ? "on" : "paused"}</span>}
        </button>
        {state && <span className="pb-1 pl-2"><TriggerSwitch definition={definition} state={state} onChanged={onChanged} /></span>}
      </li>
    })}
  </ul>
}

// InputList is what a run of the workflow asks for. A secret's default is
// never shown.
function InputList({ definition }: { definition: WorkflowDefinition }) {
  return <dl aria-label="Inputs" className="space-y-2.5 px-4 pb-3 text-[13px]">
    {Object.entries(definition.inputs ?? {}).map(([name, input]) => <div key={name}>
      <dt className="flex flex-wrap items-baseline gap-x-2">
        <span className="font-lw-mono text-xs text-lw-text">{name}</span>
        <span className="text-[11px] text-lw-faint">{input.type}{input.required ? " · required" : ""}{input.secret ? " · secret" : ""}</span>
      </dt>
      {input.default !== undefined && input.default !== "" && !input.secret && <dd className="break-words text-lw-text-2">Default <span className="font-lw-mono text-xs text-lw-text">{inputText(input.default)}</span></dd>}
      {input.description && <dd className="text-lw-text-2">{input.description}</dd>}
    </div>)}
  </dl>
}

// OutputList is what a run of the workflow gives back (schema version 2).
function OutputList({ definition }: { definition: WorkflowDefinition }) {
  return <dl aria-label="Outputs" className="space-y-2.5 px-4 pb-3 text-[13px]">
    {Object.entries(definition.outputs ?? {}).map(([name, output]) => <div key={name}>
      <dt className="flex flex-wrap items-baseline gap-x-2"><span className="font-lw-mono text-xs text-lw-text">{name}</span><span className="text-[11px] text-lw-faint">{output.type}</span></dt>
      {output.description && <dd className="text-lw-text-2">{output.description}</dd>}
      {output.value !== undefined && <dd className="break-all font-lw-mono text-[11px] text-lw-faint">{inputText(output.value)}</dd>}
    </div>)}
  </dl>
}

// StepList is the workflow's steps in order, a loop's under it; a row
// selects its node and centres it.
function StepList({ steps, selected, onPick }: { steps: GraphNode[]; selected: string | null; onPick: (node: string) => void }) {
  return <ul aria-label="Steps outline" className="max-h-[50vh] overflow-y-auto px-1.5 pb-2">
    {steps.map(node => {
      const step = node.step!
      const depth = (step.path ?? step.id).split("/").length - 1
      return <li key={node.id}>
        <button type="button" onClick={() => onPick(node.id)} style={{ paddingLeft: `${0.5 + depth}rem` }} className={rowButton(node.id === selected)}>
          <span className="min-w-0 flex-1 truncate">{step.title || step.id}</span>
          {step.gate && <span className="shrink-0 text-[11px] text-lw-needs">{gateLabel(step.gate)}</span>}
          <span className="shrink-0 font-lw-mono text-[11px] text-lw-faint">{stepTypeLabel(step.type)}</span>
        </button>
      </li>
    })}
  </ul>
}

function RecentRuns({ runs, loading, now }: { runs: WorkflowRun[]; loading: boolean; now: number }) {
  if (runs.length === 0) return <p className="px-4 pb-3 text-[13px] text-lw-text-2">{loading ? "Loading runs…" : "No runs yet"}</p>
  return <ul aria-label="Recent runs" className="px-1.5 pb-2">
    {runs.map(run => {
      const state = runState(run)
      return <li key={run.id}>
        <Link to={`/workflows/runs/${encodeURIComponent(run.id)}`} className="flex min-h-9 items-center gap-2 rounded-md px-2 py-1 hover:bg-lw-raised">
          <StatePill tone={state.tone} label={state.label} />
          <span className="min-w-0 flex-1 truncate text-[13px]">{run.workflow_title}</span>
          <span className="font-lw-mono text-[11px] text-lw-faint" title={run.created_at}>{ago(run.created_at, now)}</span>
        </Link>
      </li>
    })}
  </ul>
}

// WorkflowPage is one workflow by name (LEG-655): its graph is the page,
// under a header with Edit, Copy and Run, and its triggers, inputs,
// outputs, steps, recent runs and file are cards over it.
export function WorkflowPage() {
  const { name = "" } = useParams()
  const definitions = useWorkflows()
  const runs = useWorkflowRunsOf(name, RECENT_RUNS)
  const now = useNow(30_000)
  const [running, setRunning] = useState(false)
  const [selected, setSelected] = useState<string | null>(null)
  const [centerOn, setCenterOn] = useState<{ id: string; n: number } | null>(null)
  const { baseUrl } = useServer()
  const navigate = useNavigate()
  const [copying, setCopying] = useState(false)
  const [copyError, setCopyError] = useState("")
  const copy = async () => {
    setCopying(true)
    setCopyError("")
    try {
      const saved = await copyBuiltInWorkflow(baseUrl, name, "global")
      await definitions.refresh()
      // A reserved built-in (review-loop) stays: its copy has a name of
      // its own, which opens for editing.
      if (saved.name !== name) navigate(`/workflows/${encodeURIComponent(saved.name)}/edit`)
    } catch (reason) {
      setCopyError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setCopying(false)
    }
  }
  // At phone width the graph runs top to bottom, the cards are a sheet at
  // its foot and a node's details open in another.
  const narrow = useMediaQuery("(max-width: 767px)")
  const large = useLargeScreen()
  // Inputs is open by default on a large screen only, as on a run's page;
  // the rest that the graph or the header already say start folded.
  const folds = useCardFolds("legato:workflow-page:folds", { inputs: !large, outputs: true, steps: true, about: true })
  const triggerStates = definitions.data?.trigger_states?.[name]
  const extras = useMemo(() => {
    const out: Record<string, GraphNodeExtra> = {}
    for (const state of triggerStates ?? []) out[`trigger:${state.trigger}`] = { enabledHere: state.enabled }
    return out
  }, [triggerStates])
  const closePanel = useCallback(() => setSelected(null), [])
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
  const graph = definition.graph
  const nodes = graph?.nodes ?? []
  const triggers = nodes.filter(node => node.kind === "trigger" && node.trigger)
  const stepNodes = nodes.filter(node => node.kind === "step" && node.step)
  const states = triggerStates ?? []
  const own = triggers.filter(node => node.trigger!.kind !== "manual")
  const state = triggersState(own.map(node => states.find(item => `trigger:${item.trigger}` === node.id)?.enabled ?? node.trigger!.enabled))
  const focusNode = (node: string) => {
    setSelected(node)
    setCenterOn(current => ({ id: node, n: (current?.n ?? 0) + 1 }))
  }
  const selectedNode = nodes.find(node => node.id === selected)
  const onClose = narrow ? undefined : closePanel
  const panel = selectedNode?.kind === "step"
    ? <StepPanel definition={definition} node={selectedNode} onClose={onClose} />
    : selectedNode?.kind === "trigger"
      ? <TriggerPanel definition={definition} node={selectedNode} state={states.find(item => `trigger:${item.trigger}` === selectedNode.id)} onChanged={definitions.refresh} onClose={onClose} />
      : null
  const inputs = Object.keys(definition.inputs ?? {})
  const outputs = Object.keys(definition.outputs ?? {})
  const recent = runs.data ?? []
  const builtin = definition.source.startsWith("builtin:")
  const readOnly = !definition.editable && definition.read_only_reason && !builtin
  const shadows = definition.shadows ?? []
  const warnings = definition.warnings ?? []
  const cardOf = (key: string, label: string, size: number | null, body: ReactNode) => <DockCard key={key} label={label} folded={folds.folded(key)} onFold={() => folds.toggle(key)}
    heading={<h2 className={cardTitle}>{label}{size !== null && count(size)}</h2>}>{body}</DockCard>
  const cards = <>
    {triggers.length > 0 && cardOf("triggers", "Triggers", null, <TriggerList definition={definition} triggers={triggers} states={states} selected={selected} onPick={focusNode} onChanged={definitions.refresh} />)}
    {inputs.length > 0 && cardOf("inputs", "Inputs", inputs.length, <InputList definition={definition} />)}
    {outputs.length > 0 && cardOf("outputs", "Outputs", outputs.length, <OutputList definition={definition} />)}
    {stepNodes.length > 0 && cardOf("steps", "Steps", stepNodes.length, <StepList steps={stepNodes} selected={selected} onPick={focusNode} />)}
    {cardOf("runs", "Recent runs", recent.length, <RecentRuns runs={recent} loading={runs.loading} now={now} />)}
    {cardOf("about", "About", null, <div className="space-y-2 px-4 pb-3 text-[13px] text-lw-text-2">
      {definition.description && <p className="whitespace-pre-wrap text-lw-text">{definition.description}</p>}
      <p className="break-all font-lw-mono text-xs text-lw-faint">{definition.source}</p>
      {shadows.length > 0 && <p>This file replaces {shadows.map(path => <code key={path} className="mx-0.5 break-all font-lw-mono text-xs">{path}</code>)}, a workflow of the same name.</p>}
      {readOnly && <p>Read-only here: {definition.read_only_reason}</p>}
    </div>)}
  </>
  const box = "rounded-lg border border-lw-line bg-lw-surface p-3 text-[13px] shadow-[var(--shadow-lw-panel)]"
  const notices = (copyError || warnings.length > 0) && <>
    {copyError && <p role="alert" className={cn(box, "text-lw-error")}>{copyError}</p>}
    {warnings.length > 0 && <ul aria-label="Warnings" className={cn(box, "flex flex-col gap-1 text-lw-needs")}>
      {warnings.map((warning, i) => <li key={i}><span className="font-lw-mono text-xs text-lw-faint">{warning.line}:{warning.column}</span> {warning.message}</li>)}
    </ul>}
  </>
  return <main className="flex min-h-0 flex-1 flex-col overflow-hidden font-lw text-lw-text">
    <CanvasHeader back={{ to: "/workflows?tab=workflows", label: "Workflows" }} title={definition.title}
      subtitle={<span className="font-lw-mono text-[11px] text-lw-faint">{definition.name} · <span title={definition.source}>{sourceLabel(definition.source)}</span></span>}>
      <WorkflowTagLine definition={definition} known={knownTags(definitions.data?.definitions ?? [])} onChanged={definitions.refresh} className="shrink-0" />
      <span className="shrink-0 text-[13px] text-lw-text-2">{state}</span>
      <div className="flex flex-wrap gap-2">
        {definition.editable && <Button asChild variant="outline" size="sm"><Link to={`/workflows/${encodeURIComponent(definition.name)}/edit`}><Pencil size={13} />Edit</Link></Button>}
        {builtin && <Button variant="outline" size="sm" disabled={copying} onClick={() => void copy()}><Copy size={13} />Copy to my workflows</Button>}
        <Button variant="primary" size="sm" aria-label={`Run ${definition.title}`} onClick={() => setRunning(true)}><Play size={13} />Run</Button>
      </div>
    </CanvasHeader>
    {graph
      ? <WorkflowCanvas label="Workflow graph" narrow={narrow} cards={cards} notices={notices}
        graph={{ graph, extras, selectedId: selected, onSelect: setSelected, centerOn, direction: narrow ? "TB" : "LR" }}
        panel={panel} panelTitle={selectedNode?.step?.title ?? selectedNode?.trigger?.summary ?? ""} selected={selected !== null} onClosePanel={closePanel}
        hiddenPill={state} />
      : <section aria-label="Workflow graph" className="min-h-0 flex-1 overflow-y-auto bg-lw-ground p-4">
        <p className="text-sm text-lw-text-2">This Legato sent no graph for this workflow.</p>
        <div className="mt-4 flex max-w-md flex-col gap-2">{notices}{cards}</div>
      </section>}
    {running && <RunWorkflowDialog definition={definition} repositories={definitions.data?.repositories ?? []} onClose={() => setRunning(false)} />}
  </main>
}
