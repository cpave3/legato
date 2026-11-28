import { useEffect, useState } from "react"
import { Link, useNavigate, useSearchParams } from "react-router-dom"
import { useServer } from "../hooks/useServer"
import { ChevronRight, Plus, RefreshCw } from "lucide-react"
import { useWorkflowMap, useWorkflows, useWorkflowRuns, useWorkflowStats } from "../hooks/useWorkflow"
import { useNow } from "../hooks/useNow"
import { fetchWorkflowTemplates, type WorkflowDefinition, type WorkflowRun, type WorkflowTemplate } from "../lib/workflow"
import { ago, elapsed, parseTime, runState, runSubject, runSummary, sourceLabel, stepPosition, stepSegments } from "../lib/workflowView"
import { RunWorkflowDialog } from "../components/RunWorkflowDialog"
import { LazyWorkflowStart } from "../components/workflow-editor/LazyWorkflowStart"
import { WorkflowList } from "../components/workflows/WorkflowList"
import { WorkflowMapView } from "../components/workflow-map/WorkflowMapView"
import { StatePill, StepBar } from "../components/workflow-status/WorkflowRunStatus"
import { toneText } from "../lib/workflowTones"
import { Button } from "../components/ui/Button"
import { LoadingState } from "../components/ui/LoadingState"
import { cn } from "../lib/utils"

type WorkflowTab = "runs" | "workflows"
const TAB_KEY = "legato:workflows-tab"
// The Workflows tab shows its list or its map (LEG-710).
type WorkflowView = "list" | "map"
const VIEW_KEY = "legato:workflows-view"

function storedView(): WorkflowView {
  // codeeval-ignore lost-errors/error-to-success: storage can throw (a private window, blocked site data); the list is the default then
  try { return localStorage.getItem(VIEW_KEY) === "map" ? "map" : "list" } catch { return "list" }
}

function storeView(view: WorkflowView) {
  // codeeval-ignore lost-errors/empty-handler: storage can throw; the choice is then only not remembered
  try { localStorage.setItem(VIEW_KEY, view) } catch { /* remembered only where storage works */ }
}

// useWorkflowsView is the tab's list or map, remembered, with the map
// fetched only while it is shown; refreshAll refreshes it too then.
function useWorkflowsView(refreshes: (() => Promise<void>)[]) {
  const [view, setViewState] = useState<WorkflowView>(storedView)
  const setView = (next: WorkflowView) => { setViewState(next); storeView(next) }
  const map = useWorkflowMap(view === "map")
  const all = view === "map" ? [...refreshes, map.refresh] : refreshes
  const refreshAll = async () => { await Promise.all(all.map(refresh => refresh())) }
  return { view, setView, map, refreshAll }
}

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

const active = (run: WorkflowRun) => run.status === "running" || run.status === "waiting" || run.status === "suspended"

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

function SectionHeading({ children, count, tone }: { children: React.ReactNode; count?: number; tone?: "needs" }) {
  return <h2 className={cn("mb-2 mt-6 flex items-baseline gap-2 text-xs font-semibold uppercase tracking-wide", tone === "needs" ? "text-lw-needs" : "text-lw-text-2")}>{children}{count !== undefined && <span className={cn("font-medium", tone === "needs" ? "text-lw-needs" : "text-lw-faint")}>{count}</span>}</h2>
}

const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"]
const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]

// dayOf is the local date a run started on, and how the list heads it:
// Today, Yesterday, else "Tue 28 Jul".
function dayOf(time: string, now: number): { key: string; label: string } {
  const date = new Date(parseTime(time))
  const key = `${date.getFullYear()}-${date.getMonth()}-${date.getDate()}`
  const today = new Date(now)
  const yesterday = new Date(today.getFullYear(), today.getMonth(), today.getDate() - 1)
  if (key === `${today.getFullYear()}-${today.getMonth()}-${today.getDate()}`) return { key, label: "Today" }
  if (key === `${yesterday.getFullYear()}-${yesterday.getMonth()}-${yesterday.getDate()}`) return { key, label: "Yesterday" }
  return { key, label: `${WEEKDAYS[date.getDay()]} ${date.getDate()} ${MONTHS[date.getMonth()]}` }
}

// duration is how long a run has run, or ran.
function duration(run: WorkflowRun, now: number): string {
  const start = run.started_at || run.created_at
  if (run.completed_at) return elapsed(start, run.completed_at)
  return active(run) ? elapsed(start, now) : ""
}

// RunTree is each listed run's children: the runs its workflow.run steps
// started, oldest first.
type RunTree = Map<string, WorkflowRun[]>

function descendants(run: WorkflowRun, tree: RunTree): WorkflowRun[] {
  return (tree.get(run.id) ?? []).flatMap(child => [child, ...descendants(child, tree)])
}

// RunRow is a run, with its children folded under it. reveal says which
// runs the list is showing for: a row opens on one among its descendants,
// as it does on one still active.
function RunRow({ run, now, tree, reveal, child = false }: { run: WorkflowRun; now: number; tree: RunTree; reveal: (run: WorkflowRun) => boolean; child?: boolean }) {
  const children = tree.get(run.id) ?? []
  const [open, setOpen] = useState(() => descendants(run, tree).some(item => active(item) || reveal(item)))
  const state = runState(run)
  const steps = run.steps ?? []
  // A finished run's duration has its own column, so its line says only
  // what it left to note.
  const summary = run.status === "succeeded" ? { text: run.output_warning ?? "", tone: undefined } : runSummary(run)
  const position = stepPosition(run, steps)
  const subject = runSubject(run)
  const where = position ? `${position.number} of ${position.total}` : steps.length ? `${steps.length} ${steps.length === 1 ? "step" : "steps"}` : ""
  const took = duration(run, now)
  const failed = children.filter(item => item.status === "failed").length
  const inside = `${children.length} ${children.length === 1 ? "run" : "runs"} inside${failed ? ` · ${failed} failed` : ""}`
  return <li className={cn("border-t border-lw-line first:border-t-0", child && "bg-lw-ground/40")}>
    <Link to={`/workflows/runs/${encodeURIComponent(run.id)}`} className={cn("grid grid-cols-[minmax(0,1fr)_auto] gap-x-3 gap-y-1.5 px-4 py-3 hover:bg-lw-raised md:grid-cols-[6.5rem_minmax(0,1fr)_8rem_4.5rem_3rem] md:items-center md:gap-4", child && "pl-10")}>
      <StatePill tone={state.tone} label={state.label} className="justify-self-start" />
      <span className="col-span-2 row-start-2 min-w-0 md:col-span-1 md:row-start-auto">
        <span className="block truncate font-semibold">{run.workflow_title}{subject && <span className="hidden font-normal text-lw-text-2 md:inline"> · {subject}</span>}</span>
        {subject && <span className="block truncate text-lw-text-2 md:hidden">{subject}</span>}
        {summary.text && <span className={cn("mt-0.5 block text-[13px] md:truncate", summary.tone ? toneText[summary.tone] : "text-lw-text-2")}>{summary.text}</span>}
      </span>
      {steps.length > 0 ? <span className="col-span-2 row-start-3 flex items-center gap-2.5 md:col-span-1 md:row-start-auto md:flex-col md:items-stretch md:gap-1">
        <StepBar segments={stepSegments(run, steps)} label={position ? `Step ${position.number} of ${position.total}` : where} className="flex-1 md:flex-none" />
        <span aria-hidden="true" className="font-lw-mono text-[11px] text-lw-faint">{where}</span>
      </span> : <span className="hidden md:block" />}
      <span className="hidden text-right font-lw-mono text-xs text-lw-text md:block" title={active(run) ? "How long it has run" : "How long it ran"}>{took}</span>
      <span className="col-start-2 row-start-1 text-right font-lw-mono text-xs text-lw-faint md:col-start-auto md:row-start-auto" title={run.created_at}>
        <span className="md:hidden">{took && `${took} · `}</span>{ago(run.created_at, now)}
      </span>
    </Link>
    {children.length > 0 && <>
      <button type="button" aria-expanded={open} onClick={() => setOpen(value => !value)}
        className="flex min-h-10 w-full items-center gap-2 border-t border-dashed border-lw-line px-4 text-left text-xs text-lw-text-2 hover:text-lw-text md:pl-[8.5rem]">
        <ChevronRight size={12} aria-hidden="true" className={cn("transition-transform motion-reduce:transition-none", open && "rotate-90")} />{inside}
      </button>
      {open && <ul aria-label={`Runs inside ${run.workflow_title}`} className="border-t border-lw-line">
        {children.map(item => <RunRow key={item.id} run={item} now={now} tree={tree} reveal={reveal} child />)}
      </ul>}
    </>}
  </li>
}

function RunList({ label, runs, tree, reveal, tone }: { label: string; runs: WorkflowRun[]; tree: RunTree; reveal: (run: WorkflowRun) => boolean; tone?: "needs" }) {
  const now = useNow(30_000)
  return <ul aria-label={label} className={cn("overflow-hidden rounded-xl border bg-lw-surface", tone === "needs" ? "border-lw-needs/40" : "border-lw-line")}>
    {runs.map(run => <RunRow key={run.id} run={run} now={now} tree={tree} reveal={reveal} />)}
  </ul>
}

function LoadFailure({ error, saved, label, refreshing, refresh }: { error: string; saved: boolean; label: string; refreshing: boolean; refresh: () => Promise<void> }) {
  return <div role="alert" className="mt-4 rounded-lg border border-lw-line bg-lw-surface p-3 text-sm">
    <p className={saved ? "text-lw-needs" : "text-lw-error"}>{saved ? `Showing saved ${label}. Refresh failed: ` : `Could not load ${label}: `}{error}</p>
    <Button variant="outline" size="sm" className="mt-2" disabled={refreshing} onClick={() => { void refresh() }}>Retry</Button>
  </div>
}

type StatusFilter = "all" | "needs" | "running" | "failed" | "done"
const statusFilters: { value: StatusFilter; label: string; test: (run: WorkflowRun) => boolean }[] = [
  { value: "all", label: "All", test: () => true },
  { value: "needs", label: "Needs you", test: run => Boolean(run.needs_attention) },
  { value: "running", label: "Running", test: run => active(run) && !run.needs_attention },
  { value: "failed", label: "Failed", test: run => run.status === "failed" },
  { value: "done", label: "Done", test: run => run.status === "succeeded" },
]

function RunsTab({ setTab }: { setTab: (tab: WorkflowTab) => void }) {
  const runs = useWorkflowRuns()
  const now = useNow(30_000)
  const [status, setStatus] = useState<StatusFilter>("all")
  const [workflow, setWorkflow] = useState("")
  const all = runs.data ?? []
  // A child run is listed under its parent when the parent is listed too.
  const ids = new Set(all.map(run => run.id))
  const tree: RunTree = new Map()
  for (const run of [...all].reverse()) {
    if (run.parent_run_id && ids.has(run.parent_run_id)) tree.set(run.parent_run_id, [...(tree.get(run.parent_run_id) ?? []), run])
  }
  const top = all.filter(run => !(run.parent_run_id && ids.has(run.parent_run_id)))
  const workflows = [...new Set(all.map(run => run.workflow_name))].sort()
  const filter = statusFilters.find(item => item.value === status)!
  const filtered = status !== "all" || workflow !== ""
  const matches = (run: WorkflowRun) => filter.test(run) && (!workflow || run.workflow_name === workflow)
  // A run is listed when it or a run under it matches, so a child that
  // waits on the operator is found under its parent.
  const shown = top.filter(run => matches(run) || descendants(run, tree).some(matches))
  const reveal = (run: WorkflowRun) => filtered && matches(run)
  const waits = (run: WorkflowRun) => Boolean(run.needs_attention) || descendants(run, tree).some(item => Boolean(item.needs_attention))
  const needs = shown.filter(waits)
  const inProgress = shown.filter(run => active(run) && !waits(run))
  const days: { key: string; label: string; runs: WorkflowRun[] }[] = []
  for (const run of shown.filter(run => !active(run) && !waits(run))) {
    const day = dayOf(run.created_at, now)
    const last = days[days.length - 1]
    if (last?.key === day.key) last.runs.push(run)
    else days.push({ ...day, runs: [run] })
  }
  const needsYou = all.filter(run => run.needs_attention).length
  return <PageFrame tab="runs" setTab={setTab} needsYou={needsYou} refresh={runs.refresh}>
    {runs.loading && <LoadingState label="Loading runs…" size="inline" className="mt-6" />}
    {runs.error && <LoadFailure error={runs.error} saved={runs.data !== null} label="runs" refreshing={runs.refreshing} refresh={runs.refresh} />}
    {top.length > 0 && <div className="mt-4 flex flex-wrap items-center gap-2">
      <div role="group" aria-label="Status" className="flex flex-wrap gap-2">
        {statusFilters.map(item => {
          const count = all.filter(item.test).length
          return <button key={item.value} type="button" aria-pressed={status === item.value} onClick={() => setStatus(item.value)}
            className={cn("inline-flex min-h-9 items-center gap-1.5 rounded-full border px-3 text-[13px]", status === item.value ? "border-lw-accent bg-lw-selected text-lw-text" : "border-lw-line-strong bg-lw-surface text-lw-text-2 hover:text-lw-text")}>
            {item.label} <span className={cn("font-lw-mono text-xs", item.value === "needs" && count > 0 && "text-lw-needs")}>{count}</span>
          </button>
        })}
      </div>
      {workflows.length > 1 && <select aria-label="Workflow" value={workflow} onChange={event => setWorkflow(event.target.value)}
        className="min-h-9 rounded-md border border-lw-line-strong bg-lw-surface px-2.5 text-[13px] text-lw-text md:ml-auto">
        <option value="">Any workflow</option>
        {workflows.map(name => <option key={name} value={name}>{name}</option>)}
      </select>}
    </div>}
    {needs.length > 0 && <><SectionHeading count={needs.length} tone="needs">Needs you</SectionHeading><RunList label="Runs that need you" runs={needs} tree={tree} reveal={reveal} tone="needs" /></>}
    {inProgress.length > 0 && <><SectionHeading count={inProgress.length}>In progress</SectionHeading><RunList label="Runs in progress" runs={inProgress} tree={tree} reveal={reveal} /></>}
    {days.length > 0 && <><SectionHeading>Finished</SectionHeading>
      <div className="flex flex-col gap-3">
        {days.map(day => <div key={day.key}>
          <h3 className="mb-1.5 flex items-baseline gap-2 text-xs font-medium text-lw-text-2">{day.label}<span className="font-lw-mono text-lw-faint">{day.runs.length}</span></h3>
          <RunList label={`Finished runs, ${day.label}`} runs={day.runs} tree={tree} reveal={reveal} />
        </div>)}
      </div>
    </>}
    {top.length > 0 && shown.length === 0 && <p className="mt-6 text-sm text-lw-text-2">No runs match. <button onClick={() => { setStatus("all"); setWorkflow("") }} className="text-lw-link">Show all runs</button></p>}
    {!runs.loading && !runs.error && all.length === 0 && <p className="mt-6 text-sm text-lw-text-2">No runs yet. <button onClick={() => setTab("workflows")} className="text-lw-link">Run a workflow</button></p>}
  </PageFrame>
}

// usesInlineReview says whether a workflow runs the deprecated inline
// review step, github.pr.review_passes, a loop's body's included.
function usesInlineReview(steps: unknown[] | undefined): boolean {
  return (steps ?? []).some(step => {
    const s = step as { type?: string; steps?: unknown[] }
    return s.type === "github.pr.review_passes" || usesInlineReview(s.steps)
  })
}

// ReviewLoopNotice says that the built-in review-loop replaces an old
// review-pr-passes copy (spec LEG-559 story 10; LEG-599), and names each
// workflow still on the deprecated inline step, with the way to rewrite it.
function ReviewLoopNotice({ definitions }: { definitions: WorkflowDefinition[] }) {
  // Only a file still on the inline step: a copy already rewritten, which
  // keeps its name, has nothing left to do.
  const old = definitions.filter(d => !d.source.startsWith("builtin:") && usesInlineReview(d.steps))
  if (old.length === 0) return null
  return <section role="note" aria-label="review-loop replaces review-pr-passes" className="mt-4 rounded-lg border border-lw-line bg-lw-surface p-3 text-sm">
    <p><span className="font-medium"><code className="font-lw-mono">review-loop</code> replaces <code className="font-lw-mono">review-pr-passes</code>.</span>{" "}
      <span className="text-lw-text-2">The built-in <Link to="/workflows/review-loop" className="text-lw-link hover:underline">Review loop</Link> runs every review loop now, and every Legato has it. {old.length === 1 ? "This workflow still uses" : "These workflows still use"} the deprecated review step, which runs unchanged until a later release removes it. Rewrite {old.length === 1 ? "it" : "each"} to call review-loop: the editor's quick fix, or <code className="font-lw-mono">legato workflow upgrade &lt;file&gt;</code>, makes the change.</span></p>
    <ul className="mt-2 flex flex-col gap-1">
      {old.map(d => <li key={d.name} className="flex flex-wrap items-baseline gap-x-2">
        <span>{d.title}</span><span title={d.source} className="font-lw-mono text-xs text-lw-faint">{sourceLabel(d.source)}</span>
        {d.editable !== false && <Link to={`/workflows/${encodeURIComponent(d.name)}/edit`} aria-label={`Rewrite ${d.title}`} className="text-xs text-lw-link hover:underline">Rewrite</Link>}
      </li>)}
    </ul>
  </section>
}

function WorkflowsTab({ setTab }: { setTab: (tab: WorkflowTab) => void }) {
  const definitions = useWorkflows()
  const runs = useWorkflowRuns()
  const stats = useWorkflowStats()
  const [selected, setSelected] = useState<WorkflowDefinition | null>(null)
  const now = useNow(30_000)
  const list = definitions.data?.definitions ?? []
  const needsYou = (runs.data ?? []).filter(run => run.needs_attention).length
  const { view, setView, map, refreshAll } = useWorkflowsView([definitions.refresh, runs.refresh, stats.refresh])
  const newWorkflow = <Link to="/workflows/new" className="inline-flex min-h-9 items-center gap-1.5 rounded-md bg-lw-accent px-3 text-xs font-medium text-[var(--theme-canvas,white)] hover:opacity-90"><Plus size={14} aria-hidden="true" />New workflow</Link>
  return <PageFrame tab="workflows" setTab={setTab} needsYou={needsYou} refresh={refreshAll}>
    {definitions.loading && <LoadingState label="Loading workflows…" size="inline" className="mt-6" />}
    {definitions.error && <LoadFailure error={definitions.error} saved={definitions.data !== null} label="workflows" refreshing={definitions.refreshing} refresh={definitions.refresh} />}
    {stats.error && list.length > 0 && <LoadFailure error={stats.error} saved={stats.data !== null} label="run counts" refreshing={stats.refreshing} refresh={stats.refresh} />}
    <ReviewLoopNotice definitions={list} />
    <DiscoveryErrors errors={definitions.data?.errors} />
    {list.length > 0 && <>
      <ViewSwitch view={view} setView={setView} />
      {view === "map" && <MapTab map={map} />}
      {view === "list" && <WorkflowList definitions={list} triggerStates={definitions.data?.trigger_states} stats={stats.data} now={now} onRun={setSelected} refresh={refreshAll} actions={newWorkflow} />}
    </>}
    {!definitions.loading && !definitions.error && list.length === 0 && <section aria-label="Start a workflow" className="mt-6 flex flex-col gap-3 text-sm">
      <div className="flex justify-end">{newWorkflow}</div>
      <p className="text-lw-text-2">No workflow definitions found</p>
      <p className="text-lw-text-2">A workflow is a YAML file of steps. <a href="https://cpave3.github.io/legato/using/workflows/" target="_blank" rel="noreferrer" className="text-lw-link hover:underline">Write your first workflow</a>, or start from one of these.</p>
      <EmptyStart />
    </section>}
    {selected && <RunWorkflowDialog definition={selected} repositories={definitions.data?.repositories ?? []} onClose={() => setSelected(null)} />}
  </PageFrame>
}

// DiscoveryErrors are the workflow files that could not be read.
function DiscoveryErrors({ errors }: { errors: { path: string; error: string }[] | null | undefined }) {
  return <>{(errors ?? []).map(error => <p key={error.path} className="mt-4 break-words rounded-lg border border-lw-danger-line bg-lw-error-soft p-3 text-sm text-lw-danger"><span className="font-lw-mono">{sourceLabel(error.path)}</span>: {error.error}</p>)}</>
}

// ViewSwitch picks the Workflows tab's list or its map.
function ViewSwitch({ view, setView }: { view: WorkflowView; setView: (view: WorkflowView) => void }) {
  const options: { value: WorkflowView; label: string }[] = [{ value: "list", label: "List" }, { value: "map", label: "Map" }]
  return <div role="group" aria-label="View" className="mt-4 inline-flex self-start overflow-hidden rounded-md border border-lw-line-strong">
    {options.map(option => <button key={option.value} type="button" aria-pressed={view === option.value} onClick={() => setView(option.value)}
      className={cn("min-h-9 border-r border-lw-line px-3 text-[13px] last:border-r-0",
        view === option.value ? "bg-lw-selected text-lw-text shadow-[inset_0_-2px_0_var(--color-lw-accent)]" : "bg-lw-surface text-lw-text-2 hover:text-lw-text")}>{option.label}</button>)}
  </div>
}

// MapTab is the map of the workflows' calls, or why it is not there.
function MapTab({ map }: { map: ReturnType<typeof useWorkflowMap> }) {
  return <>
    {map.loading && <LoadingState label="Loading the map…" size="inline" className="mt-6" />}
    {map.error && <LoadFailure error={map.error} saved={map.data !== null} label="the map" refreshing={map.refreshing} refresh={map.refresh} />}
    {map.data && <WorkflowMapView map={map.data} />}
  </>
}

// EmptyStart is the start page on an empty Workflows tab (LEG-514): what
// is chosen opens the editor on it.
function EmptyStart() {
  const navigate = useNavigate()
  const { baseUrl } = useServer()
  const [templates, setTemplates] = useState<WorkflowTemplate[] | null>(null)
  useEffect(() => {
    let current = true
    fetchWorkflowTemplates(baseUrl).then(list => { if (current) setTemplates(list.templates ?? []) }, () => { if (current) setTemplates([]) })
    return () => { current = false }
  }, [baseUrl])
  return <LazyWorkflowStart templates={templates} yours={[]} taken={[]} loadYours={() => Promise.reject(new Error("none"))}
    onStart={text => navigate("/workflows/new", { state: { start: text } })} />
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
