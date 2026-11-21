import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query"
import { useEffect, useState } from "react"
import { Link, useNavigate, useSearchParams } from "react-router-dom"
import { ArrowLeft, Search, X } from "lucide-react"
import { Button } from "../components/ui/Button"
import { ConfirmDialog } from "../components/ui/ConfirmDialog"
import { InlineNotice } from "../components/ui/InlineNotice"
import { LoadingState } from "../components/ui/LoadingState"
import { useServer } from "../hooks/useServer"
import { agentKindLabel } from "../lib/agentKind"
import { queryKeys } from "../lib/query"
import {
  fetchSession,
  fetchSessions,
  forgetSession,
  restoreSession,
  sessionStateLabel,
  type SessionEntry,
  type SessionFilters,
} from "../lib/sessionHistory"
import { cn } from "../lib/utils"

const PAGE = 50

function when(iso?: string) {
  return iso ? new Date(iso).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" }) : ""
}

// useDebounced is value once it has stopped changing for delay ms.
function useDebounced<T>(value: T, delay: number): T {
  const [settled, setSettled] = useState(value)
  useEffect(() => {
    const timer = setTimeout(() => setSettled(value), delay)
    return () => clearTimeout(timer)
  }, [value, delay])
  return settled
}

// SessionHistoryPage is the session history: every conversation the
// agents of this Legato had, searchable, with Restore and Forget.
export function SessionHistoryPage() {
  const { baseUrl } = useServer()
  const [params, setParams] = useSearchParams()
  const task = params.get("task") ?? ""
  const selected = Number(params.get("session") ?? "") || null
  const [typed, setTyped] = useState(params.get("q") ?? "")
  const q = useDebounced(typed.trim(), 300)
  const [agentKind, setAgentKind] = useState("")
  const [state, setState] = useState("")
  const [repo, setRepo] = useState("")
  const settledRepo = useDebounced(repo.trim(), 300)

  const filters: SessionFilters = { q, agent_kind: agentKind, state, task, repo: settledRepo, limit: PAGE }
  const list = useInfiniteQuery({
    queryKey: queryKeys.sessions(baseUrl, filters),
    queryFn: ({ pageParam }) => fetchSessions(baseUrl, { ...filters, cursor: pageParam || undefined }),
    initialPageParam: "",
    getNextPageParam: (page) => page.next_cursor || undefined,
  })
  const entries = list.data?.pages.flatMap((page) => page.sessions) ?? []

  const setParam = (key: string, value: string) => {
    const next = new URLSearchParams(params)
    if (value) next.set(key, value)
    else next.delete(key)
    setParams(next, { replace: true })
  }

  return <main className="mx-auto flex w-full max-w-6xl flex-col gap-4 p-4 text-lw-text md:p-8">
    <div className="flex flex-wrap items-center gap-3">
      <Link to="/agents" className="flex items-center gap-1 text-sm text-lw-link" aria-label="Back to agents"><ArrowLeft size={14} aria-hidden="true" />Agents</Link>
      <h1 className="m-0 text-xl font-semibold">Session history</h1>
    </div>
    <p className="m-0 text-sm text-lw-text-2">Every conversation the agents of this Legato had. Restore one to start it again where it was.</p>

    <div role="search" className="flex flex-wrap items-center gap-2">
      <label className="flex min-w-0 flex-1 basis-60 items-center gap-2 rounded-md border border-lw-line bg-lw-surface px-2">
        <Search size={14} aria-hidden="true" className="text-lw-faint" />
        <input type="search" aria-label="Search sessions" placeholder="Search titles, folders and messages" value={typed}
          onChange={(event) => setTyped(event.target.value)}
          className="min-h-9 min-w-0 flex-1 bg-transparent text-sm outline-none" />
      </label>
      <select aria-label="Tool" value={agentKind} onChange={(event) => setAgentKind(event.target.value)} className="min-h-9 rounded-md border border-lw-line bg-lw-surface px-2 text-sm">
        <option value="">Any tool</option>
        <option value="claude-code">Claude Code</option>
        <option value="codex">Codex</option>
        <option value="chimera">Chimera</option>
      </select>
      <select aria-label="State" value={state} onChange={(event) => setState(event.target.value)} className="min-h-9 rounded-md border border-lw-line bg-lw-surface px-2 text-sm">
        <option value="">Any state</option>
        <option value="running">Running</option>
        <option value="interrupted">Interrupted</option>
        <option value="ended">Ended</option>
        <option value="away">Away</option>
      </select>
      <input aria-label="Repository" placeholder="owner/name" value={repo} onChange={(event) => setRepo(event.target.value)}
        className="min-h-9 w-36 rounded-md border border-lw-line bg-lw-surface px-2 text-sm" />
      {task && <span className="flex items-center gap-1 rounded-full bg-lw-raised px-2 py-1 text-xs">
        Task {task}
        <button type="button" aria-label="Show every task" onClick={() => setParam("task", "")} className="text-lw-text-2 hover:text-lw-text"><X size={12} aria-hidden="true" /></button>
      </span>}
    </div>

    {list.isError && <InlineNotice variant="error">Could not load the history: {list.error.message}</InlineNotice>}
    <div className={cn("grid gap-4", selected && "md:grid-cols-[minmax(0,1fr)_minmax(0,24rem)]")}>
      <section aria-label="Sessions" className="min-w-0">
        {list.isPending && <LoadingState label="Loading the history…" size="inline" />}
        {list.isSuccess && entries.length === 0 && <p className="text-sm text-lw-text-2">{q || agentKind || state || task || settledRepo ? "No session matches." : "No sessions yet."}</p>}
        <ul className="m-0 flex list-none flex-col gap-1 p-0">
          {entries.map((entry) => <SessionRow key={entry.id} entry={entry} selected={entry.id === selected} onSelect={() => setParam("session", String(entry.id))} />)}
        </ul>
        {list.hasNextPage && <Button variant="outline" size="sm" className="mt-2" disabled={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>
          {list.isFetchingNextPage ? "Loading…" : "Show more"}
        </Button>}
      </section>
      {selected && <SessionDetailPanel key={selected} id={selected} onClose={() => setParam("session", "")} />}
    </div>
  </main>
}

function where(entry: SessionEntry) {
  return entry.repo ? `${entry.repo}${entry.branch ? ` · ${entry.branch}` : ""}` : entry.cwd
}

function SessionRow({ entry, selected, onSelect }: { entry: SessionEntry; selected: boolean; onSelect: () => void }) {
  const title = entry.task_title || entry.task_id
  return <li aria-label={title}>
    <button type="button" onClick={onSelect} aria-current={selected || undefined}
      className={cn("flex w-full min-w-0 flex-col gap-0.5 rounded-md px-3 py-2 text-left hover:bg-lw-raised", selected && "bg-lw-raised")}>
      <span className="flex min-w-0 items-center gap-2">
        <span className="min-w-0 flex-1 truncate font-medium">{title}</span>
        <span className="shrink-0 text-[11px] text-lw-faint">{agentKindLabel(entry.agent_kind)}</span>
      </span>
      <span className="flex min-w-0 flex-wrap gap-x-3 text-xs text-lw-text-2">
        <span>{sessionStateLabel(entry)}</span>
        <span>{when(entry.last_seen_at)}</span>
        {entry.source === "recovered" && <span>found by folder and time</span>}
        {!entry.task_exists && <span>task deleted</span>}
      </span>
      <span className="truncate font-lw-mono text-[11px] text-lw-faint" title={where(entry)}>{where(entry)}</span>
      {entry.first_message && <span className="truncate text-xs text-lw-text-2">{entry.first_message}</span>}
      {entry.last_message && <span className="truncate text-xs text-lw-faint">{entry.last_message}</span>}
    </button>
  </li>
}

function SessionDetailPanel({ id, onClose }: { id: number; onClose: () => void }) {
  const { baseUrl } = useServer()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const detail = useQuery({ queryKey: queryKeys.session(baseUrl, id), queryFn: () => fetchSession(baseUrl, id) })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [forgetting, setForgetting] = useState(false)

  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey: [baseUrl, "sessions"] })
    await queryClient.invalidateQueries({ queryKey: queryKeys.session(baseUrl, id) })
  }
  const restore = async (taskId: string, target: "task" | "new_task") => {
    setError("")
    setBusy(true)
    try {
      const runsIn = (await restoreSession(baseUrl, id, target)) || taskId
      await refresh()
      navigate(`/agents?task=${encodeURIComponent(runsIn)}`)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setBusy(false)
    }
  }
  const forget = async () => {
    setForgetting(false)
    setError("")
    setBusy(true)
    try {
      await forgetSession(baseUrl, id)
      await queryClient.invalidateQueries({ queryKey: [baseUrl, "sessions"] })
      onClose()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setBusy(false)
    }
  }

  // On a phone the panel comes first, above the list it was picked from.
  return <aside aria-label="Session" className="order-first flex min-w-0 flex-col gap-3 rounded-xl border border-lw-line bg-lw-surface p-4 md:order-none md:sticky md:top-4 md:self-start">
    <div className="flex items-start gap-2">
      <h2 className="m-0 min-w-0 flex-1 break-words text-base font-semibold">{detail.data ? (detail.data.task_title || detail.data.task_id) : "Session"}</h2>
      <Button variant="ghost" size="icon-sm" aria-label="Close session" onClick={onClose}><X size={16} /></Button>
    </div>
    {detail.isPending && <LoadingState label="Loading…" size="inline" />}
    {detail.isError && <InlineNotice variant="error">{detail.error.message}</InlineNotice>}
    {detail.data && (() => {
      const entry = detail.data
      const canRestore = entry.restorable && !entry.new_task_only
      // A Reeve conversation is resumed as the Reeve's, never in a new task.
      const canNewTask = entry.restorable && !entry.reeve
      return <>
        <dl className="m-0 grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-xs">
          <dt className="text-lw-faint">Tool</dt><dd className="m-0">{agentKindLabel(entry.agent_kind)}</dd>
          <dt className="text-lw-faint">State</dt><dd className="m-0">{sessionStateLabel(entry)}</dd>
          <dt className="text-lw-faint">Task</dt><dd className="m-0 break-all">{entry.task_id}{!entry.task_exists && " (deleted)"}{entry.task_archived && " (archived)"}</dd>
          <dt className="text-lw-faint">Folder</dt><dd className="m-0 break-all font-lw-mono">{entry.cwd || "unknown"}</dd>
          {entry.repo && <><dt className="text-lw-faint">Repository</dt><dd className="m-0 break-all">{where(entry)}</dd></>}
          <dt className="text-lw-faint">Last active</dt><dd className="m-0">{when(entry.last_seen_at)}</dd>
          {entry.source === "recovered" && <><dt className="text-lw-faint">Source</dt><dd className="m-0">found by folder and time</dd></>}
        </dl>
        {entry.first_message && <section aria-label="First prompt">
          <h3 className="m-0 mb-1 text-[11px] font-semibold uppercase tracking-wide text-lw-faint">First prompt</h3>
          <p className="m-0 whitespace-pre-wrap break-words text-sm">{entry.first_message}</p>
        </section>}
        {entry.last_message && <section aria-label="Last message">
          <h3 className="m-0 mb-1 text-[11px] font-semibold uppercase tracking-wide text-lw-faint">Last message</h3>
          <p className="m-0 whitespace-pre-wrap break-words text-sm">{entry.last_message}</p>
        </section>}
        <section aria-label="Runs">
          <h3 className="m-0 mb-1 text-[11px] font-semibold uppercase tracking-wide text-lw-faint">Runs · {entry.runs.length}</h3>
          <ol className="m-0 flex list-none flex-col gap-1 p-0 text-xs">
            {entry.runs.map((run) => <li key={run.execution_id} className="flex flex-wrap gap-x-2">
              <span>{when(run.started_at)}</span>
              <span className="text-lw-text-2">{run.ended_at ? `${run.end_reason ?? "ended"} ${when(run.ended_at)}` : "running"}</span>
              {run.sandboxed !== undefined && <span className="text-lw-faint">{run.sandboxed ? "sandboxed" : "not sandboxed"}</span>}
              {run.engram && <span className="text-lw-faint">engram {run.engram}</span>}
              {run.resources.length > 0 && <span className="text-lw-faint">{run.resources.join(", ")}</span>}
            </li>)}
          </ol>
        </section>
        {entry.reason && <p className="m-0 text-xs text-lw-text-2">{entry.restorable ? entry.reason : `Cannot restore: ${entry.reason}`}</p>}
        {error && <p role="alert" className="m-0 whitespace-pre-line break-words text-xs text-lw-error">{error}</p>}
        <div className="flex flex-wrap gap-2">
          {canRestore && <Button size="sm" disabled={busy} onClick={() => void restore(entry.task_id, "task")}>{busy ? "Restoring…" : "Restore"}</Button>}
          {canNewTask && <Button variant={canRestore ? "outline" : "primary"} size="sm" disabled={busy}
            title="Make a new task like this one's and start the conversation there"
            onClick={() => void restore(entry.task_id, "new_task")}>Restore into a new task</Button>}
          <Button variant="outline" size="sm" disabled={busy || entry.state === "running" || entry.state === "interrupted"}
            title={entry.state === "running" || entry.state === "interrupted" ? "Stop or dismiss its agent first" : undefined}
            onClick={() => setForgetting(true)}>Forget</Button>
        </div>
      </>
    })()}
    {forgetting && <ConfirmDialog title="Forget this session?" destructive confirmLabel="Forget"
      message="It leaves Session history for good. The tool's own transcript, and the task's Resume, are left as they are."
      onConfirm={() => void forget()} onCancel={() => setForgetting(false)} />}
  </aside>
}
