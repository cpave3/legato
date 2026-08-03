import { useCallback, useEffect, useMemo, useState } from "react"
import { ArrowDown, ArrowLeft, ArrowRight, ArrowUp, Check, Clipboard, RefreshCw } from "lucide-react"
import { InlineNotice } from "../components/ui/InlineNotice"
import { useServer } from "../hooks/useServer"
import {
  fetchReport,
  fetchReportMarkdown,
  type ReportDirectory,
  type ReportEnvelope,
  type ReportPeriodKind,
  type ReportSummary,
  type ReportSwarm,
  type ReportTask,
  type ReportWorkspace,
} from "../lib/report"

type Breakdown = "tasks" | "workspaces" | "projects" | "swarms"
type SortDirection = "asc" | "desc"

function formatDuration(seconds: number): string {
  if (seconds <= 0) return "—"
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  if (hours && minutes) return `${hours}h ${minutes}m`
  if (hours) return `${hours}h`
  if (minutes) return `${minutes}m`
  return "<1m"
}

function formatDate(date: string): string {
  return new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", timeZone: "UTC" }).format(new Date(`${date}T00:00:00Z`))
}

function formatRange(start: string, end: string): string {
  const options: Intl.DateTimeFormatOptions = { month: "short", day: "numeric", year: "numeric", hour: "numeric", minute: "2-digit", timeZoneName: "short" }
  const formatter = new Intl.DateTimeFormat(undefined, options)
  return `${formatter.format(new Date(start))} – ${formatter.format(new Date(end))}`
}

function ratio(summary: ReportSummary): number {
  const total = summary.working_seconds + summary.waiting_seconds
  return total === 0 ? 0 : summary.working_seconds / total
}

function Delta({ current, previous, ratioPoints = false }: { current: number; previous: number; ratioPoints?: boolean }) {
  if (ratioPoints) {
    const points = (current - previous) * 100
    if (points === 0) return <span>— vs previous period</span>
    const Icon = points > 0 ? ArrowUp : ArrowDown
    return <span className="inline-flex items-center gap-1"><Icon aria-hidden="true" size={12} />{Math.abs(points).toFixed(0)} pts vs previous period</span>
  }
  if (previous === 0) return <span>— vs previous period</span>
  const change = ((current - previous) / previous) * 100
  if (change === 0) return <span>— vs previous period</span>
  const Icon = change > 0 ? ArrowUp : ArrowDown
  return <span className="inline-flex items-center gap-1"><Icon aria-hidden="true" size={12} />{Math.abs(change).toFixed(0)}% vs previous period</span>
}

function MetricCard({ label, value, children, secondary }: { label: string; value: string; children: React.ReactNode; secondary?: string }) {
  return <article className="rounded-xl border border-zinc-800 bg-zinc-900/60 p-4">
    <h3 className="text-xs font-medium uppercase tracking-wide text-zinc-500">{label}</h3>
    <p className="mt-2 text-2xl font-semibold text-zinc-100">{value}</p>
    {secondary && <p className="mt-1 text-xs text-zinc-500">{secondary}</p>}
    <p className="mt-2 text-xs text-zinc-400">{children}</p>
  </article>
}

function SummaryCards({ current, previous }: { current: ReportSummary; previous: ReportSummary }) {
  return <section aria-label="Summary" className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
    <MetricCard label="Working time" value={formatDuration(current.working_seconds)}><Delta current={current.working_seconds} previous={previous.working_seconds} /></MetricCard>
    <MetricCard label="Waiting time" value={formatDuration(current.waiting_seconds)}><Delta current={current.waiting_seconds} previous={previous.waiting_seconds} /></MetricCard>
    <MetricCard label="Working ratio" value={`${(ratio(current) * 100).toFixed(0)}%`}><Delta current={ratio(current)} previous={ratio(previous)} ratioPoints /></MetricCard>
    <MetricCard label="Tasks completed" value={String(current.tasks_completed)} secondary={`${current.tasks_created} created`}><Delta current={current.tasks_completed} previous={previous.tasks_completed} /></MetricCard>
    <MetricCard label="Agent sessions" value={String(current.agent_sessions)}><Delta current={current.agent_sessions} previous={previous.agent_sessions} /></MetricCard>
    <MetricCard label="PRs merged" value={String(current.prs_merged)}><Delta current={current.prs_merged} previous={previous.prs_merged} /></MetricCard>
  </section>
}

function DailyActivity({ days }: { days: ReportEnvelope["report"]["by_day"] }) {
  const maximum = Math.max(1, ...days.map(day => day.working_seconds + day.waiting_seconds))
  return <section className="rounded-xl border border-zinc-800 bg-zinc-900/40 p-4" aria-labelledby="daily-activity-title">
    <div className="flex flex-wrap items-center justify-between gap-3">
      <h2 id="daily-activity-title" className="font-semibold">Daily activity</h2>
      <div className="flex gap-4 text-xs text-zinc-400"><span className="flex items-center gap-1.5"><i className="size-2 rounded-sm bg-indigo-500" />Working</span><span className="flex items-center gap-1.5"><i className="size-2 rounded-sm bg-amber-500" />Waiting</span></div>
    </div>
    <div className="mt-4 space-y-4">
      {days.map(day => {
        const workingWidth = day.working_seconds / maximum * 100
        const waitingWidth = day.waiting_seconds / maximum * 100
        const label = `${formatDate(day.date)}: ${formatDuration(day.working_seconds)} working, ${formatDuration(day.waiting_seconds)} waiting`
        return <div key={day.date} role="img" aria-label={label}>
          <div className="mb-1 flex justify-between gap-3 text-xs"><span className="text-zinc-300">{formatDate(day.date)}</span><span className="text-zinc-500">{formatDuration(day.working_seconds)} working · {formatDuration(day.waiting_seconds)} waiting</span></div>
          <div className="flex h-3 overflow-hidden rounded-full bg-zinc-800"><span className="bg-indigo-500" style={{ width: `${workingWidth}%` }} /><span className="bg-amber-500" style={{ width: `${waitingWidth}%` }} /></div>
        </div>
      })}
    </div>
  </section>
}

function SortButton({ label, field, sort, onSort }: { label: string; field: string; sort: { field: string; direction: SortDirection }; onSort: (field: string) => void }) {
  const active = sort.field === field
  return <button onClick={() => onSort(field)} className="inline-flex items-center gap-1" aria-label={`Sort by ${label}`}>
    {label}{active && (sort.direction === "desc" ? <ArrowDown size={12} /> : <ArrowUp size={12} />)}
  </button>
}

const th = "whitespace-nowrap px-3 py-2 text-left text-xs font-medium text-zinc-500"
const td = "whitespace-nowrap px-3 py-2 text-sm text-zinc-300"

function BreakdownTable({ active, envelope }: { active: Breakdown; envelope: ReportEnvelope }) {
  const [sort, setSort] = useState({ field: "working_seconds", direction: "desc" as SortDirection })
  useEffect(() => setSort({ field: "working_seconds", direction: "desc" }), [active])
  const onSort = (field: string) => setSort(current => ({ field, direction: current.field === field && current.direction === "desc" ? "asc" : "desc" }))
  const rows = active === "tasks" ? envelope.report.by_task : active === "workspaces" ? envelope.report.by_workspace : active === "projects" ? envelope.report.by_directory : envelope.report.by_swarm
  const sorted = [...rows].sort((a, b) => {
    const value = (row: unknown) => {
      const record = row as Record<string, unknown>
      return sort.field === "total"
        ? Number(record.working_seconds ?? 0) + Number(record.waiting_seconds ?? 0)
        : Number(record[sort.field] ?? 0)
    }
    return (value(a) - value(b)) * (sort.direction === "asc" ? 1 : -1)
  })
  if (sorted.length === 0) return <div className="rounded-lg border border-dashed border-zinc-800 p-8 text-center text-sm text-zinc-500">No {active === "projects" ? "project" : active.slice(0, -1)} activity for this period</div>

  if (active === "tasks") return <div className="overflow-x-auto"><table aria-label="Tasks breakdown" className="w-full"><thead><tr><th className={th}>ID</th><th className={th}>Title</th><th className={th}>Workspace</th><th className={th}>Provider</th>{["working_seconds", "waiting_seconds", "total"].map(field => <th key={field} className={th} aria-sort={sort.field === field ? (sort.direction === "desc" ? "descending" : "ascending") : "none"}><SortButton label={field === "working_seconds" ? "Working" : field === "waiting_seconds" ? "Waiting" : "Total"} field={field} sort={sort} onSort={onSort} /></th>)}</tr></thead><tbody>{(sorted as ReportTask[]).map(row => <tr key={row.task_id} className="border-t border-zinc-800"><td className={td}>{row.display_id}</td><td className={td}>{row.title}</td><td className={td}>{row.workspace_name || "—"}</td><td className={td}>{row.provider || "Local"}</td><td className={td}>{formatDuration(row.working_seconds)}</td><td className={td}>{formatDuration(row.waiting_seconds)}</td><td className={td}>{formatDuration(row.working_seconds + row.waiting_seconds)}</td></tr>)}</tbody></table></div>

  if (active === "swarms") return <div className="overflow-x-auto"><table aria-label="Swarms breakdown" className="w-full"><thead><tr><th className={th}>Title</th>{[["working_seconds", "Working"], ["waiting_seconds", "Waiting"], ["wall_clock_seconds", "Wall clock"], ["parallel_ratio", "Parallel ratio"], ["worker_count", "Workers"], ["subtask_count", "Subtasks"]].map(([field, label]) => <th key={field} className={th} aria-sort={sort.field === field ? (sort.direction === "desc" ? "descending" : "ascending") : "none"}><SortButton label={label} field={field} sort={sort} onSort={onSort} /></th>)}</tr></thead><tbody>{(sorted as ReportSwarm[]).map(row => <tr key={row.parent_task_id} className="border-t border-zinc-800"><td className={td}>{row.title || row.parent_task_id}</td><td className={td}>{formatDuration(row.working_seconds)}</td><td className={td}>{formatDuration(row.waiting_seconds)}</td><td className={td}>{formatDuration(row.wall_clock_seconds)}</td><td className={td}>{row.parallel_ratio ? `${row.parallel_ratio.toFixed(1)}x` : "—"}</td><td className={td}>{row.worker_count}</td><td className={td}>{row.subtask_count}</td></tr>)}</tbody></table></div>

  const namedRows = sorted as (ReportWorkspace | ReportDirectory)[]
  const nameLabel = active === "projects" ? "Project path" : "Workspace"
  return <div className="overflow-x-auto"><table aria-label={`${active === "projects" ? "Projects" : "Workspaces"} breakdown`} className="w-full"><thead><tr><th className={th}>{nameLabel}</th>{[["task_count", "Tasks"], ["working_seconds", "Working"], ["waiting_seconds", "Waiting"], ["total", "Total"]].map(([field, label]) => <th key={field} className={th} aria-sort={sort.field === field ? (sort.direction === "desc" ? "descending" : "ascending") : "none"}><SortButton label={label} field={field} sort={sort} onSort={onSort} /></th>)}</tr></thead><tbody>{namedRows.map(row => {
    const project = "directory" in row
    const name = project ? row.directory : row.workspace_name
    return <tr key={project ? row.directory : String(row.workspace_id ?? row.workspace_name)} className="border-t border-zinc-800"><td className={`${td} max-w-80 truncate`} title={name}>{name || "Unassigned"}</td><td className={td}>{row.task_count}</td><td className={td}>{formatDuration(row.working_seconds)}</td><td className={td}>{formatDuration(row.waiting_seconds)}</td><td className={td}>{formatDuration(row.working_seconds + row.waiting_seconds)}</td></tr>
  })}</tbody></table></div>
}

function hasActivity(summary: ReportSummary): boolean {
  return summary.working_seconds > 0 || summary.waiting_seconds > 0 || summary.tasks_created > 0 || summary.tasks_completed > 0 || summary.agent_sessions > 0 || summary.prs_merged > 0
}

export function ReportsPage() {
  const { baseUrl } = useServer()
  const [period, setPeriod] = useState<ReportPeriodKind>("week")
  const [offset, setOffset] = useState(0)
  const [data, setData] = useState<ReportEnvelope | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [reload, setReload] = useState(0)
  const [active, setActive] = useState<Breakdown>("tasks")
  const [copyStatus, setCopyStatus] = useState<"idle" | "success" | "error">("idle")

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError(null)
    fetchReport(baseUrl, { period, offset, signal: controller.signal }).then(result => {
      if (!controller.signal.aborted) setData(result)
    }).catch(cause => {
      if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : String(cause))
    }).finally(() => {
      if (!controller.signal.aborted) setLoading(false)
    })
    return () => controller.abort()
  }, [baseUrl, period, offset, reload])

  const copyMarkdown = useCallback(async () => {
    setCopyStatus("idle")
    try {
      const markdown = await fetchReportMarkdown(baseUrl, { period, offset })
      await navigator.clipboard.writeText(markdown)
      setCopyStatus("success")
    } catch {
      setCopyStatus("error")
    }
  }, [baseUrl, offset, period])

  const tabs = useMemo(() => data ? [
    { id: "tasks" as const, label: "Tasks", count: data.report.by_task.length },
    { id: "workspaces" as const, label: "Workspaces", count: data.report.by_workspace.length },
    { id: "projects" as const, label: "Projects", count: data.report.by_directory.length },
    { id: "swarms" as const, label: "Swarms", count: data.report.by_swarm.length },
  ] : [], [data])

  if (!data && loading) return <div className="p-8" role="status" aria-label="Loading report"><div className="h-8 w-56 animate-pulse rounded bg-zinc-800" /><div className="mt-6 grid gap-3 sm:grid-cols-3">{Array.from({ length: 6 }, (_, i) => <div key={i} className="h-28 animate-pulse rounded-xl bg-zinc-900" />)}</div></div>
  if (!data && error) return <div className="p-6"><InlineNotice variant="error"><p>{error}</p><button onClick={() => setReload(value => value + 1)} className="mt-2 rounded border border-red-700 px-3 py-1">Retry</button></InlineNotice></div>
  if (!data) return null

  return <div className="flex-1 overflow-y-auto p-4 sm:p-6" aria-busy={loading}>
    <div className="mx-auto max-w-7xl space-y-6">
      <header className="space-y-4">
        <div className="flex flex-wrap items-start justify-between gap-4"><div><h1 className="text-xl font-semibold">Reports</h1><p className="mt-1 text-sm text-zinc-500">Agent activity and delivery trends</p></div><button onClick={copyMarkdown} className="inline-flex min-h-10 items-center gap-2 rounded-lg border border-zinc-700 px-3 text-sm text-zinc-300 hover:bg-zinc-800"><Clipboard size={16} />Copy Markdown</button></div>
        {copyStatus === "success" && <p role="status" className="flex items-center gap-1 text-sm text-emerald-400"><Check size={14} />Markdown copied</p>}
        {copyStatus === "error" && <InlineNotice variant="error">Could not copy Markdown</InlineNotice>}
        {error && <InlineNotice variant="error"><span>{error}</span><button onClick={() => setReload(value => value + 1)} className="ml-3 underline">Retry</button></InlineNotice>}
        <div className="flex flex-col gap-3 rounded-xl border border-zinc-800 bg-zinc-900/40 p-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex rounded-lg bg-zinc-950 p-1" aria-label="Report period">{(["day", "week", "month"] as ReportPeriodKind[]).map(kind => <button key={kind} onClick={() => { setPeriod(kind); setOffset(0) }} aria-pressed={period === kind} className={`rounded-md px-3 py-1.5 text-sm capitalize ${period === kind ? "bg-zinc-700 text-white" : "text-zinc-500"}`}>{kind[0].toUpperCase() + kind.slice(1)}</button>)}</div>
          <div className="flex items-center justify-between gap-3"><button aria-label="Previous period" onClick={() => setOffset(value => value - 1)} className="rounded p-2 hover:bg-zinc-800"><ArrowLeft size={17} /></button><div className="min-w-0 text-center"><h2 className="font-medium">{data.report.period.label}</h2><p className="mt-0.5 text-xs text-zinc-500">{formatRange(data.report.period.start, data.report.period.end)}</p></div><button aria-label="Next period" disabled={offset === 0} onClick={() => setOffset(value => Math.min(0, value + 1))} className="rounded p-2 hover:bg-zinc-800 disabled:opacity-30"><ArrowRight size={17} /></button></div>
        </div>
        {loading && <p role="status" className="flex items-center gap-2 text-xs text-zinc-500"><RefreshCw className="animate-spin" size={13} />Updating report</p>}
      </header>

      {!hasActivity(data.report.summary) ? <section className="rounded-xl border border-dashed border-zinc-800 px-4 py-16 text-center"><h2 className="text-lg font-medium">No activity recorded for this period</h2><p className="mt-2 text-sm text-zinc-500">Try an earlier period or a broader range.</p></section> : <>
        <SummaryCards current={data.report.summary} previous={data.comparison.summary} />
        {data.report.by_day.length > 0 && <DailyActivity days={data.report.by_day} />}
        <section className="rounded-xl border border-zinc-800 bg-zinc-900/40 p-4">
          <div role="tablist" aria-label="Report breakdowns" className="mb-4 flex gap-1 overflow-x-auto border-b border-zinc-800">{tabs.map(tab => <button key={tab.id} role="tab" aria-selected={active === tab.id} onClick={() => setActive(tab.id)} className={`whitespace-nowrap border-b-2 px-3 py-2 text-sm ${active === tab.id ? "border-indigo-400 text-indigo-300" : "border-transparent text-zinc-500"}`}>{tab.label} <span className="ml-1 rounded bg-zinc-800 px-1.5 py-0.5 text-xs">{tab.count}</span></button>)}</div>
          <BreakdownTable active={active} envelope={data} />
        </section>
      </>}
    </div>
  </div>
}
