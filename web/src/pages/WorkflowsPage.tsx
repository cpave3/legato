import { useState } from "react"
import { Link, useSearchParams } from "react-router-dom"
import { Play, RefreshCw } from "lucide-react"
import { useWorkflows, useWorkflowRuns } from "../hooks/useWorkflow"
import { useNow } from "../hooks/useNow"
import type { WorkflowDefinition, WorkflowRun } from "../lib/workflow"
import { ago, inputsLabel, runState, runSubject, runSummary, sourceLabel, stepPosition, stepSegments, triggerText } from "../lib/workflowView"
import { RunWorkflowDialog } from "../components/RunWorkflowDialog"
import { StatePill, StepBar } from "../components/WorkflowRunStatus"
import { toneText } from "../lib/workflowTones"
import { Button } from "../components/ui/Button"
import { LoadingState } from "../components/ui/LoadingState"
import { cn } from "../lib/utils"

type WorkflowTab = "runs" | "workflows"
const TAB_KEY = "legato:workflows-tab"

function storedTab(): WorkflowTab | null {
  try {
    const value = localStorage.getItem(TAB_KEY)
    return value === "runs" || value === "workflows" ? value : null
  } catch {
    return null
  }
}

function storeTab(tab: WorkflowTab) {
  try { localStorage.setItem(TAB_KEY, tab) } catch { /* the tab is only remembered where storage works */ }
}

const active = (run: WorkflowRun) => run.status === "running" || run.status === "waiting"

function PageFrame({ tab, setTab, needsYou, refresh, children }: { tab: WorkflowTab; setTab: (tab: WorkflowTab) => void; needsYou: number; refresh: () => Promise<void>; children: React.ReactNode }) {
  const tabClass = (on: boolean) => cn("-mb-px flex min-h-11 items-center gap-1.5 border-b-2 px-3 text-sm", on ? "border-lw-accent text-lw-text" : "border-transparent text-lw-text-2 hover:text-lw-text")
  return <main className="flex min-h-0 flex-1 flex-col overflow-y-auto px-4 py-4 font-lw text-lw-text sm:px-6 md:py-6">
    <div className="mx-auto w-full max-w-5xl">
      <header className="flex items-center justify-between gap-4">
        <h1 className="text-xl font-semibold">Workflows</h1>
        <Button variant="ghost" size="icon" aria-label="Refresh workflows" onClick={() => { void refresh() }}><RefreshCw size={16} /></Button>
      </header>
      <div role="tablist" aria-label="Runs or workflows" className="mt-3 flex gap-1 border-b border-lw-line">
        <button role="tab" aria-selected={tab === "runs"} onClick={() => setTab("runs")} className={tabClass(tab === "runs")}>
          Runs{needsYou > 0 && <span aria-label={`${needsYou} need you`} className="rounded-full bg-lw-needs-soft px-1.5 text-[11px] text-lw-needs">{needsYou}</span>}
        </button>
        <button role="tab" aria-selected={tab === "workflows"} onClick={() => setTab("workflows")} className={tabClass(tab === "workflows")}>Workflows</button>
      </div>
      {children}
    </div>
  </main>
}

function SectionHeading({ children, count }: { children: React.ReactNode; count?: number }) {
  return <h2 className="mb-2 mt-6 flex items-baseline gap-2 text-xs font-semibold uppercase tracking-wide text-lw-text-2">{children}{count !== undefined && <span className="font-medium text-lw-faint">{count}</span>}</h2>
}

function RunRow({ run, now }: { run: WorkflowRun; now: number }) {
  const state = runState(run)
  const steps = run.steps ?? []
  const summary = runSummary(run)
  const position = stepPosition(run, steps)
  const subject = runSubject(run)
  const where = position ? `${position.number} of ${position.total}` : steps.length ? `${steps.length} ${steps.length === 1 ? "step" : "steps"}` : ""
  return <li className="border-t border-lw-line first:border-t-0">
    <Link to={`/workflows/runs/${encodeURIComponent(run.id)}`} className="grid grid-cols-[minmax(0,1fr)_auto] gap-x-3 gap-y-1.5 px-4 py-3 hover:bg-lw-raised md:grid-cols-[6.5rem_minmax(0,1fr)_8rem_3rem] md:items-center md:gap-4">
      <StatePill tone={state.tone} label={state.label} className="justify-self-start" />
      <span className="col-span-2 row-start-2 min-w-0 md:col-span-1 md:row-start-auto">
        <span className="block truncate font-semibold">{run.workflow_title}{subject && <span className="hidden font-normal text-lw-text-2 md:inline"> · {subject}</span>}</span>
        {subject && <span className="block truncate text-lw-text-2 md:hidden">{subject}</span>}
        {summary.text && <span className={cn("mt-0.5 block text-[13px] md:truncate", summary.tone ? toneText[summary.tone] : "text-lw-text-2")}>{summary.text}</span>}
      </span>
      {steps.length > 0 && <span className="col-span-2 row-start-3 flex items-center gap-2.5 md:col-span-1 md:row-start-auto md:flex-col md:items-stretch md:gap-1">
        <StepBar segments={stepSegments(run, steps)} label={position ? `Step ${position.number} of ${position.total}` : where} className="flex-1 md:flex-none" />
        <span aria-hidden="true" className="font-lw-mono text-[11px] text-lw-faint">{where}</span>
      </span>}
      <span className="col-start-2 row-start-1 text-right font-lw-mono text-xs text-lw-faint md:col-start-auto md:row-start-auto" title={run.created_at}>{ago(run.created_at, now)}</span>
    </Link>
  </li>
}

function RunList({ label, runs }: { label: string; runs: WorkflowRun[] }) {
  const now = useNow(30_000)
  return <ul aria-label={label} className="overflow-hidden rounded-xl border border-lw-line bg-lw-surface">
    {runs.map(run => <RunRow key={run.id} run={run} now={now} />)}
  </ul>
}

function RunsTab({ setTab }: { setTab: (tab: WorkflowTab) => void }) {
  const runs = useWorkflowRuns()
  const all = runs.data ?? []
  const inProgress = all.filter(active).sort((a, b) => Number(Boolean(b.needs_attention)) - Number(Boolean(a.needs_attention)))
  const finished = all.filter(run => !active(run))
  const needsYou = all.filter(run => run.needs_attention).length
  return <PageFrame tab="runs" setTab={setTab} needsYou={needsYou} refresh={runs.refresh}>
    {runs.loading && <LoadingState label="Loading runs…" size="inline" className="mt-6" />}
    {runs.error && <p className="mt-6 text-sm text-lw-error">{runs.error}</p>}
    {inProgress.length > 0 && <><SectionHeading count={inProgress.length}>In progress</SectionHeading><RunList label="Runs in progress" runs={inProgress} /></>}
    {finished.length > 0 && <><SectionHeading>Finished</SectionHeading><RunList label="Finished runs" runs={finished} /></>}
    {!runs.loading && !runs.error && all.length === 0 && <p className="mt-6 text-sm text-lw-text-2">No runs yet. <button onClick={() => setTab("workflows")} className="text-lw-link">Run a workflow</button></p>}
  </PageFrame>
}

function StepTitles({ definition }: { definition: WorkflowDefinition }) {
  return <p className="text-[13px]">
    {definition.steps.map((step, index) => <span key={step.id}>
      {index > 0 && <span aria-hidden="true" className="px-1.5 text-lw-faint">→</span>}
      {step.title}
      {(step.trigger === "manual" || step.trigger === "agent_ended") && <span role="img" aria-label="waits for you" className="ml-1">✋</span>}
    </span>)}
  </p>
}

function LastRun({ run, now }: { run?: WorkflowRun; now: number }) {
  if (!run) return <span className="text-xs text-lw-faint">No recent runs</span>
  const state = runState(run)
  return <Link to={`/workflows/runs/${encodeURIComponent(run.id)}`} className="flex items-center gap-2 text-xs text-lw-faint hover:text-lw-text-2">
    <StatePill tone={state.tone} label={state.label} /><span className="font-lw-mono">{ago(run.created_at, now)}</span>
  </Link>
}

function WorkflowsTab({ setTab }: { setTab: (tab: WorkflowTab) => void }) {
  const definitions = useWorkflows()
  const runs = useWorkflowRuns()
  const [selected, setSelected] = useState<WorkflowDefinition | null>(null)
  const now = useNow(30_000)
  const list = definitions.data?.definitions ?? []
  const lastRun = new Map<string, WorkflowRun>()
  for (const run of runs.data ?? []) if (!lastRun.has(run.workflow_name)) lastRun.set(run.workflow_name, run)
  const needsYou = (runs.data ?? []).filter(run => run.needs_attention).length
  return <PageFrame tab="workflows" setTab={setTab} needsYou={needsYou} refresh={async () => { await Promise.all([definitions.refresh(), runs.refresh()]) }}>
    {definitions.loading && <LoadingState label="Loading workflows…" size="inline" className="mt-6" />}
    {(definitions.data?.errors ?? []).map(error => <p key={error.path} className="mt-4 break-words rounded-lg border border-lw-danger-line bg-lw-error-soft p-3 text-sm text-lw-danger"><span className="font-lw-mono">{sourceLabel(error.path)}</span>: {error.error}</p>)}
    {list.length > 0 && <ul aria-label="Workflows" className="mt-5 overflow-hidden rounded-xl border border-lw-line bg-lw-surface">
      {list.map(definition => {
        const takes = inputsLabel(definition.inputs)
        const trigger = triggerText(definition.on)
        return <li key={definition.name} className="grid gap-x-5 gap-y-1.5 border-t border-lw-line p-4 first:border-t-0 md:grid-cols-[minmax(0,1fr)_auto]">
          <div className="flex flex-wrap items-baseline gap-x-2.5">
            <h2 className="text-[15px] font-semibold">{definition.title}</h2>
            <span title={definition.source} className="font-lw-mono text-xs text-lw-faint">{sourceLabel(definition.source)}</span>
          </div>
          <div className="flex items-center justify-between gap-3 md:col-start-2 md:row-span-4 md:row-start-1 md:flex-col md:items-end md:justify-start">
            <Button variant="outline" size="sm" aria-label={`Run ${definition.title}`} onClick={() => setSelected(definition)} className="order-2 md:order-1"><Play size={13} />Run</Button>
            {runs.data && <span className="order-1 md:order-2"><LastRun run={lastRun.get(definition.name)} now={now} /></span>}
          </div>
          {definition.description && <p className="text-[13px] text-lw-text-2">{definition.description}</p>}
          <StepTitles definition={definition} />
          {(takes || trigger) && <p className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-lw-text-2">
            {takes && <span><span className="text-lw-faint">Takes</span> {takes}</span>}
            {trigger && <span className="text-lw-link">↳ {trigger}</span>}
          </p>}
        </li>
      })}
    </ul>}
    {!definitions.loading && list.length === 0 && <p className="mt-6 text-sm text-lw-text-2">No workflow definitions found</p>}
    {selected && <RunWorkflowDialog definition={selected} repositories={definitions.data?.repositories ?? []} onClose={() => setSelected(null)} />}
  </PageFrame>
}

export function WorkflowsPage() {
  const [params, setParams] = useSearchParams()
  const asked = params.get("tab")
  const [tab, setTabState] = useState<WorkflowTab>(() => asked === "runs" || asked === "workflows" ? asked : storedTab() ?? "runs")
  const setTab = (next: WorkflowTab) => {
    setTabState(next)
    storeTab(next)
    if (params.has("tab")) setParams(current => { current.delete("tab"); return current }, { replace: true })
  }
  return tab === "runs" ? <RunsTab setTab={setTab} /> : <WorkflowsTab setTab={setTab} />
}
