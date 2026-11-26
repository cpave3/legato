import { useMemo, useState, type ReactNode } from "react"
import { Link, useNavigate } from "react-router-dom"
import { ArrowDown, ArrowUp, ChevronRight, Clock, Columns3, CornerDownRight, GitPullRequest, MessageSquare, MousePointerClick, Play, Search, SlidersHorizontal } from "lucide-react"
import { useServer } from "../../hooks/useServer"
import { setWorkflowTrigger, stepStart, type WorkflowDefinition, type WorkflowStats, type WorkflowTriggerState } from "../../lib/workflow"
import { ago, inputsLabel, runState, sourceLabel, triggerText } from "../../lib/workflowView"
import { allTags, errorText, groupRows, knownTags, matches, noFilters, sortRows, startKinds, switchLabel, workflowRows, type Filters, type SortBy, type WorkflowGroup, type WorkflowRow } from "../../lib/workflowList"
import { copyBuiltInWorkflow } from "../../lib/workflowFiles"
import { cn } from "../../lib/utils"
import { StatePill } from "../WorkflowRunStatus"
import { TriggerSwitch } from "../WorkflowTriggerSwitch"
import { AgentMoreMenu } from "../AgentChatHeader"
import { Button } from "../ui/Button"
import { TagChip, WorkflowTagsDialog } from "./WorkflowTagsDialog"

type GroupBy = "tags" | "none"
const GROUP_KEY = "legato:workflows-group"

function storedGroup(): GroupBy {
  try { return localStorage.getItem(GROUP_KEY) === "none" ? "none" : "tags" } catch { return "tags" }
}

function storeGroup(group: GroupBy) {
  try { localStorage.setItem(GROUP_KEY, group) } catch { /* remembered only where storage works */ }
}

// Desktop columns: details, workflow, starts, triggers, last run, 7 days,
// actions. On a phone a row is two lines.
const columns = "md:grid md:grid-cols-[2rem_minmax(0,1fr)_13.5rem_7.5rem_9rem_8.5rem_6.5rem] md:items-center md:gap-x-3"

function Segmented<T extends string>({ label, value, options, onChange }: { label: string; value: T; options: { value: T; label: string }[]; onChange: (value: T) => void }) {
  return <div className="flex items-center gap-2">
    <span className="text-xs text-lw-text-2" aria-hidden="true">{label}</span>
    <div role="group" aria-label={label} className="inline-flex flex-wrap overflow-hidden rounded-md border border-lw-line-strong">
      {options.map(option => <button key={option.value} type="button" aria-pressed={value === option.value} onClick={() => onChange(option.value)}
        className={cn("min-h-9 border-r border-lw-line px-2.5 text-[13px] last:border-r-0",
          value === option.value ? "bg-lw-selected text-lw-text shadow-[inset_0_-2px_0_var(--color-lw-accent)]" : "bg-lw-surface text-lw-text-2 hover:text-lw-text")}>{option.label}</button>)}
    </div>
  </div>
}

function StartIcon({ kind }: { kind: WorkflowRow["start"]["kind"] }) {
  const props = { size: 15, className: "mt-0.5 shrink-0 text-lw-text-2", "aria-hidden": true as const }
  switch (kind) {
    case "schedule": return <Clock {...props} />
    case "chat": return <MessageSquare {...props} />
    case "button": return <MousePointerClick {...props} />
    case "column": return <Columns3 {...props} />
    case "pr": return <GitPullRequest {...props} />
    case "called": return <CornerDownRight {...props} />
  }
  return <Play {...props} />
}

function plural(count: number, one: string) {
  return `${count} ${count === 1 ? one : `${one}s`}`
}

function Week({ row }: { row: WorkflowRow }) {
  const total = row.days.reduce((sum, day) => sum + day.runs, 0)
  const failed = row.days.reduce((sum, day) => sum + day.failed, 0)
  const most = Math.max(1, ...row.days.map(day => day.runs))
  return <span className="flex items-center gap-2.5">
    <span role="img" aria-label={`${plural(total, "run")} in the last 7 days, ${failed} failed`} className="flex h-5 items-end gap-[3px]">
      {row.days.map((day, index) => <span key={index} className={cn("w-1.5 rounded-sm", day.failed > 0 ? "bg-lw-error" : day.runs > 0 ? "bg-lw-done" : "bg-lw-line")}
        style={{ height: day.runs === 0 ? 2 : Math.max(4, Math.round(day.runs / most * 20)) }} />)}
    </span>
    <span aria-hidden="true" className="text-xs leading-4">
      <span className="block font-lw-mono text-lw-text">{plural(total, "run")}</span>
      {total > 0 && <span className={cn("block", failed > 0 ? "text-lw-error" : "text-lw-text-2")}>{failed > 0 ? `${failed} failed` : "none failed"}</span>}
    </span>
  </span>
}

function SwitchButton({ row, onOpen, onChanged }: { row: WorkflowRow; onOpen: () => void; onChanged: () => Promise<unknown> }) {
  const { baseUrl } = useServer()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  if (row.switches.state === "none") return <span className="text-lw-text-2" aria-label="No triggers to switch">—</span>
  const label = switchLabel(row.switches)
  const click = async () => {
    if (row.states.length !== 1) { onOpen(); return }
    const state = row.states[0]
    setBusy(true)
    setError("")
    try {
      await setWorkflowTrigger(baseUrl, row.definition.name, state.trigger, !state.enabled)
      await onChanged()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setBusy(false)
    }
  }
  return <span className="inline-flex flex-col items-start">
    <button type="button" disabled={busy} onClick={() => void click()} aria-label={`Triggers of ${row.definition.title}: ${label}`}
      title={row.states.length === 1 ? "Switched here; the file is not changed" : "Open the row to switch each trigger"}
      className={cn("inline-flex min-h-7 items-center gap-1.5 whitespace-nowrap rounded-full border px-2.5 text-xs",
        row.switches.state === "on" ? "border-lw-live/50 text-lw-live" : row.switches.state === "mixed" ? "border-lw-needs/50 text-lw-needs" : "border-lw-line-strong text-lw-text-2")}>
      <span aria-hidden="true" className={cn("size-2 rounded-full", row.switches.state === "on" ? "bg-lw-live" : row.switches.state === "mixed" ? "bg-lw-needs" : "bg-lw-faint")} />
      {label[0].toUpperCase() + label.slice(1)}
    </button>
    {error && <span role="alert" className="mt-1 text-xs text-lw-error">{error}</span>}
  </span>
}

// LastRun is the newest run's state and age: a link to it, or on a phone's
// second line (link false) the same without the link the row already has.
function LastRun({ row, statsLoaded, now, link = true }: { row: WorkflowRow; statsLoaded: boolean; now: number; link?: boolean }) {
  if (!row.last) return statsLoaded ? <span className="whitespace-nowrap text-xs text-lw-text-2">Never run</span> : null
  const state = runState(row.last)
  const pill = <StatePill tone={state.tone} label={state.label} />
  return <span className="flex items-center gap-2">
    {link ? <Link to={`/workflows/runs/${encodeURIComponent(row.last.id)}`} className="hover:opacity-90">{pill}</Link> : pill}
    <span className="font-lw-mono text-xs text-lw-text-2" title={row.last.created_at}>{ago(row.last.created_at, now)}</span>
  </span>
}

function Details({ row, onChanged, onTags }: { row: WorkflowRow; onChanged: () => Promise<unknown>; onTags: () => void }) {
  const { definition } = row
  const takes = inputsLabel(definition.inputs)
  const column = triggerText(definition.on)
  return <div role="group" aria-label={`${definition.title} details`} className="px-3 pb-4 md:pl-14">
    <div className="flex flex-col gap-3 rounded-lg border border-lw-line bg-lw-ground p-4 text-[13px]">
      {definition.description && <p className="max-w-[72ch] text-lw-text">{definition.description}</p>}
      <div className="grid gap-4 md:grid-cols-[minmax(0,1.4fr)_minmax(0,0.8fr)_minmax(0,1.3fr)]">
        <div>
          <p className="mb-1.5 text-[11px] font-medium uppercase tracking-wide text-lw-text-2">Steps</p>
          <p className="leading-6">{definition.steps.map((step, index) => <span key={step.id}>
            {index > 0 && <span aria-hidden="true" className="px-1.5 text-lw-faint">→</span>}
            {step.title}
            {stepStart(step) !== "automatic" && <span role="img" aria-label="waits for you" className="ml-1">✋</span>}
          </span>)}</p>
        </div>
        <div>
          <p className="mb-1.5 text-[11px] font-medium uppercase tracking-wide text-lw-text-2">Takes</p>
          <p>{takes || "Nothing"}</p>
        </div>
        <div>
          <p className="mb-1.5 text-[11px] font-medium uppercase tracking-wide text-lw-text-2">Triggers</p>
          {row.states.length > 0
            ? <div className="flex flex-col gap-2">{row.states.map((state: WorkflowTriggerState) => <TriggerSwitch key={state.trigger} definition={definition} state={state} onChanged={onChanged} />)}</div>
            : <p className="text-lw-text-2">{row.inner ? "Only other workflows run it" : "Run it yourself, from a card or through the API"}</p>}
          {column && <p className="mt-1.5 text-xs text-lw-text-2">{column}</p>}
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
        <Link to={`/workflows/${encodeURIComponent(definition.name)}`} className="text-lw-link hover:underline">Open workflow</Link>
        {definition.editable && <Link to={`/workflows/${encodeURIComponent(definition.name)}/edit`} className="text-lw-link hover:underline">Edit</Link>}
        <button type="button" onClick={onTags} className="text-lw-link hover:underline">Tags…</button>
        <span title={definition.source} className="font-lw-mono text-xs text-lw-text-2 md:ml-auto">{sourceLabel(definition.source)}</span>
      </div>
    </div>
  </div>
}

function Row({ row, group, open, onToggle, statsLoaded, now, onRun, onTags, onChanged }: {
  row: WorkflowRow; group: string; open: boolean; onToggle: () => void; statsLoaded: boolean; now: number
  onRun: (definition: WorkflowDefinition) => void; onTags: (definition: WorkflowDefinition) => void; onChanged: () => Promise<unknown>
}) {
  const navigate = useNavigate()
  const { baseUrl } = useServer()
  const { definition } = row
  const builtIn = definition.source.startsWith("builtin:")
  const others = row.tags.filter(tag => tag !== group)
  const [actionError, setActionError] = useState("")
  const copy = async () => {
    setActionError("")
    try {
      await copyBuiltInWorkflow(baseUrl, definition.name, "global")
      await onChanged()
    } catch (reason) {
      setActionError(errorText(reason))
    }
  }
  const entries = [
    { label: "Open", onSelect: () => navigate(`/workflows/${encodeURIComponent(definition.name)}`) },
    { label: "Run…", onSelect: () => onRun(definition) },
    ...(definition.editable ? [{ label: "Edit", onSelect: () => navigate(`/workflows/${encodeURIComponent(definition.name)}/edit`) }] : []),
    { label: "Tags…", onSelect: () => onTags(definition) },
    ...(builtIn ? [{ label: "Copy to my workflows", onSelect: copy }] : []),
  ]
  return <li className="border-t border-lw-line first:border-t-0">
    <div className={cn("grid grid-cols-[minmax(0,1fr)_auto_auto] items-center gap-x-2 px-3 py-2 hover:bg-lw-raised/50", columns, row.inner ? "min-h-12" : "min-h-14")}>
      <button type="button" onClick={onToggle} aria-expanded={open} aria-label={`Details of ${definition.title}`}
        className="hidden size-8 items-center justify-center rounded-md text-lw-text-2 hover:bg-lw-selected hover:text-lw-text md:inline-flex">
        <ChevronRight size={14} aria-hidden="true" className={cn("transition-transform motion-reduce:transition-none", open && "rotate-90")} />
      </button>
      <div className="min-w-0">
        <Link to={`/workflows/${encodeURIComponent(definition.name)}`} title={definition.description || undefined}
          className={cn("block truncate text-[15px] hover:text-lw-link hover:underline md:text-sm", row.inner ? "font-medium text-lw-text-2" : "font-semibold text-lw-text")}>{definition.title}</Link>
        <div className="mt-0.5 flex flex-wrap items-center gap-1.5 text-xs text-lw-text-2">
          <span className="font-lw-mono">{definition.name}</span>
          {builtIn && <span>· built in</span>}
          {others.map(tag => <TagChip key={tag} tag={tag} />)}
        </div>
        <div className="mt-1 flex min-w-0 items-center gap-2 text-[13px] text-lw-text-2 md:hidden">
          <span className="truncate">{row.start.text}</span>
          <LastRun row={row} statsLoaded={statsLoaded} now={now} link={false} />
        </div>
      </div>
      <div className="hidden min-w-0 items-start gap-2 text-[13px] md:flex">
        <StartIcon kind={row.start.kind} />
        <span className="min-w-0"><span className="block truncate" title={row.start.text}>{row.start.text}</span>{row.start.detail && <span className="block text-xs text-lw-text-2">{row.start.detail}</span>}</span>
      </div>
      <div><SwitchButton row={row} onOpen={onToggle} onChanged={onChanged} /></div>
      <div className="hidden md:block"><LastRun row={row} statsLoaded={statsLoaded} now={now} /></div>
      <div className="hidden md:block">{statsLoaded && <Week row={row} />}</div>
      <div className="flex items-center justify-end gap-1">
        <Button variant="outline" size="sm" className="hidden md:inline-flex" aria-label={`Run ${definition.title}`} onClick={() => onRun(definition)}><Play size={12} aria-hidden="true" />Run</Button>
        <AgentMoreMenu label={`More for ${definition.title}`} entries={entries} />
      </div>
    </div>
    {actionError && <p role="alert" className="px-3 pb-2 text-xs text-lw-error md:pl-14">{actionError}</p>}
    {open && <Details row={row} onChanged={onChanged} onTags={() => onTags(definition)} />}
  </li>
}

function GroupNote({ group }: { group: WorkflowGroup }) {
  const all = [...group.rows, ...group.inner]
  const needs = all.filter(row => row.last && runState(row.last).tone === "needs").length
  const failed = all.filter(row => row.last?.status === "failed").length
  const parts = [needs ? `${needs} needs you` : "", failed ? `${failed} last run failed` : ""].filter(Boolean)
  if (!parts.length) return null
  return <span className={cn("ml-auto text-xs", needs ? "text-lw-needs" : "text-lw-error")}>{parts.join(" · ")}</span>
}

// WorkflowList is the Workflows tab's list (LEG-657): a row per workflow,
// grouped by tag or not at all, with search and filters.
export function WorkflowList({ definitions, triggerStates, stats, now, onRun, refresh, actions }: {
  definitions: WorkflowDefinition[]
  triggerStates: Record<string, WorkflowTriggerState[]> | null | undefined
  stats: WorkflowStats | null
  now: number
  onRun: (definition: WorkflowDefinition) => void
  refresh: () => Promise<unknown>
  actions?: ReactNode
}) {
  const [filters, setFilters] = useState<Filters>(noFilters)
  const [groupBy, setGroupByState] = useState<GroupBy>(storedGroup)
  const [sortBy, setSortBy] = useState<SortBy>("title")
  const [closed, setClosed] = useState<Set<string>>(new Set())
  const [folds, setFolds] = useState<Set<string>>(new Set())
  const [open, setOpen] = useState("")
  const [tagging, setTagging] = useState<WorkflowDefinition | null>(null)
  const [filtersOpen, setFiltersOpen] = useState(false)
  const rows = useMemo(() => workflowRows(definitions, triggerStates, stats, now), [definitions, triggerStates, stats, now])
  const shown = rows.filter(row => matches(row, filters))
  const tags = allTags(definitions)
  const known = knownTags(definitions)
  const grouped = groupBy === "tags" && tags.length > 0
  const filtered = filters.query.trim() !== "" || filters.starts !== "all" || filters.triggers !== "any"
  const setGroupBy = (next: GroupBy) => { setGroupByState(next); storeGroup(next) }
  const flip = (set: Set<string>, key: string) => { const next = new Set(set); if (next.has(key)) next.delete(key); else next.add(key); return next }
  const rowView = (row: WorkflowRow, group: string) => {
    const key = `${group}:${row.definition.name}`
    return <Row key={key} row={row} group={group} open={open === key} onToggle={() => setOpen(current => current === key ? "" : key)} statsLoaded={stats !== null} now={now}
      onRun={onRun} onTags={setTagging} onChanged={refresh} />
  }
  const sortButton = (by: SortBy, label: string) => <button type="button" onClick={() => setSortBy(by)} aria-pressed={sortBy === by}
    className={cn("inline-flex items-center gap-1", sortBy === by ? "text-lw-text" : "hover:text-lw-text")}>
    {label}{sortBy === by && (by === "title" ? <ArrowUp size={12} aria-hidden="true" /> : <ArrowDown size={12} aria-hidden="true" />)}
  </button>

  return <>
    <div className="mt-4 flex flex-wrap items-center gap-2 md:gap-3">
      <label className="flex min-h-9 min-w-0 flex-1 items-center gap-2 rounded-md border border-lw-line-strong bg-lw-surface px-2.5 text-lw-text-2 focus-within:border-lw-accent md:max-w-80 md:flex-none md:basis-80">
        <Search size={15} aria-hidden="true" />
        <input type="search" aria-label="Search workflows" placeholder="Search by title, name or tag" value={filters.query} onChange={event => setFilters({ ...filters, query: event.target.value })}
          className="min-w-0 flex-1 bg-transparent text-sm text-lw-text placeholder:text-lw-faint focus:outline-none" />
      </label>
      <Button variant="outline" size="sm" className="md:hidden" aria-expanded={filtersOpen} onClick={() => setFiltersOpen(value => !value)}><SlidersHorizontal size={14} aria-hidden="true" />Filter</Button>
      <span className="hidden text-xs text-lw-text-2 md:ml-auto md:inline">{plural(definitions.length, "workflow")}{tags.length > 0 && ` · ${plural(tags.length, "tag")}`}</span>
      {actions}
    </div>
    <div className={cn("mt-3 flex-wrap items-center gap-x-5 gap-y-2", filtersOpen ? "flex" : "hidden md:flex")}>
      <Segmented label="Starts" value={filters.starts} onChange={starts => setFilters({ ...filters, starts })}
        options={[{ value: "all" as const, label: "All" }, ...startKinds.map(kind => ({ value: kind.kind, label: kind.label }))]} />
      <Segmented label="Triggers" value={filters.triggers} onChange={triggers => setFilters({ ...filters, triggers })}
        options={[{ value: "any" as const, label: "Any" }, { value: "on" as const, label: "On" }, { value: "paused" as const, label: "Paused" }]} />
      {tags.length > 0 && <Segmented label="Group" value={groupBy} onChange={setGroupBy}
        options={[{ value: "tags" as const, label: "Tags" }, { value: "none" as const, label: "None" }]} />}
    </div>

    <div className="mt-4 overflow-hidden rounded-xl border border-lw-line bg-lw-surface shadow-[var(--shadow-lw-panel)]">
      <div className={cn("hidden min-h-9 border-b border-lw-line px-3 text-xs font-medium text-lw-text-2", columns)}>
        <span />
        <span>{sortButton("title", "Workflow")}</span>
        <span>Starts</span>
        <span>Triggers</span>
        <span>{sortButton("last", "Last run")}</span>
        <span>Last 7 days</span>
        <span />
      </div>
      {shown.length === 0 && <div className="flex flex-col items-start gap-2.5 px-5 py-8 text-sm">
        <p>{filters.query.trim() ? `No workflows match “${filters.query.trim()}”` : "No workflows match these filters"}</p>
        {filtered && <Button variant="outline" size="sm" onClick={() => setFilters(noFilters)}>Clear search and filters</Button>}
      </div>}
      {shown.length > 0 && !grouped && <ul aria-label="Workflows">{sortRows(shown, sortBy).map(row => rowView(row, ""))}</ul>}
      {shown.length > 0 && grouped && groupRows(shown, sortBy).map(group => {
        const label = group.tag || "Untagged"
        const isOpen = !closed.has(group.key)
        const foldOpen = folds.has(group.key)
        return <section key={group.key} aria-label={label} className="border-t border-lw-line first-of-type:border-t-0">
          <button type="button" aria-expanded={isOpen} onClick={() => setClosed(current => flip(current, group.key))}
            className="flex min-h-11 w-full items-center gap-2.5 bg-lw-raised px-3 text-left text-[13px] text-lw-text">
            <ChevronRight size={14} aria-hidden="true" className={cn("text-lw-text-2 transition-transform motion-reduce:transition-none", isOpen && "rotate-90")} />
            {group.tag ? <TagChip tag={group.tag} className="h-6 px-2.5 text-xs" /> : <span className="font-semibold text-lw-text-2">Untagged</span>}
            <span className="font-lw-mono text-xs text-lw-text-2">{group.rows.length + group.inner.length}</span>
            <GroupNote group={group} />
          </button>
          {isOpen && <>
            <ul aria-label={group.tag ? `Workflows tagged ${group.tag}` : "Untagged workflows"}>{group.rows.map(row => rowView(row, group.tag))}</ul>
            {group.inner.length > 0 && <>
              <button type="button" aria-expanded={foldOpen} onClick={() => setFolds(current => flip(current, group.key))}
                className="flex min-h-10 w-full items-center gap-2.5 border-t border-dashed border-lw-line px-3 text-left text-xs text-lw-text-2 hover:text-lw-text md:pl-14">
                <CornerDownRight size={14} aria-hidden="true" />Run by other workflows <span className="font-lw-mono">{group.inner.length}</span>
                <ChevronRight size={12} aria-hidden="true" className={cn("transition-transform motion-reduce:transition-none", foldOpen && "rotate-90")} />
              </button>
              {foldOpen && <ul aria-label={`Run by other workflows, ${label}`}>{group.inner.map(row => rowView(row, group.tag))}</ul>}
            </>}
          </>}
        </section>
      })}
    </div>
    {tagging && <WorkflowTagsDialog definition={tagging} known={known} onClose={() => setTagging(null)} onSaved={refresh} />}
  </>
}
