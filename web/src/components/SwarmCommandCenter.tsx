import { useMemo } from "react"
import { AlertTriangle, Bot, ChevronLeft, Radio, Users } from "lucide-react"
import type { AgentInfo } from "../hooks/useWebSocket"
import type { SwarmStatusData } from "../lib/swarm"
import { cn } from "../lib/utils"
import { useAgentPreview } from "../hooks/useAgentPreview"
import { LoadingState } from "./ui/LoadingState"

interface WorkerView {
  subtask: SwarmStatusData["subtasks"][number]
  agent: AgentInfo | null
}

function lifecycleLabel(status: string) {
  return status.replaceAll("_", " ")
}

function statusTone(status: string) {
  if (status === "done") return "border-emerald-800 bg-emerald-950/20 text-emerald-300"
  if (status === "cancelled" || status === "dead") return "border-red-900 bg-red-950/20 text-red-300"
  if (status === "reporting") return "border-indigo-800 bg-indigo-950/20 text-indigo-300"
  if (status === "in_progress" || status === "dispatched") return "border-emerald-900 bg-emerald-950/20 text-emerald-300"
  return "border-zinc-700 bg-zinc-900 text-zinc-400"
}

function WorkerCard({ worker, focused, onExpand, onOpenSingle }: {
  worker: WorkerView
  focused: boolean
  onExpand: () => void
  onOpenSingle: () => void
}) {
  const previewable = Boolean(worker.agent && !worker.agent.has_chimera_endpoint && worker.agent.status === "running" && !focused)
  const { preview, observe } = useAgentPreview(worker.agent?.task_id ?? null, previewable)
  const attention = worker.agent?.activity === "waiting" || worker.agent?.activity === "error"

  return (
    <article ref={observe} className={cn("rounded-lg border p-3", focused ? "border-indigo-500 bg-indigo-950/20" : "border-zinc-800 bg-zinc-950/60")}>
      <div className="flex min-w-0 items-start gap-2">
        <button
          type="button"
          className="min-w-0 flex-1 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500"
          onClick={onExpand}
          disabled={!worker.agent}
          aria-label={worker.agent ? `Expand ${worker.subtask.title}` : `${worker.subtask.title} has no running session`}
        >
          <span className="block truncate text-sm font-medium text-zinc-200">{worker.subtask.title}</span>
          <span className="mt-1 flex flex-wrap items-center gap-1.5 text-[10px] uppercase tracking-wide text-zinc-500">
            <span>{worker.subtask.role || "worker"}</span>
            <span>step {worker.subtask.step_index + 1}</span>
            {worker.agent?.activity && <span>{worker.agent.activity}</span>}
          </span>
        </button>
        <span className={cn("rounded border px-1.5 py-0.5 text-[10px] font-medium uppercase", statusTone(worker.subtask.status))}>
          {lifecycleLabel(worker.subtask.status)}
        </span>
        {attention && <AlertTriangle size={14} className="mt-0.5 text-amber-400" aria-label="Needs attention" />}
      </div>
      {preview?.content && (
        <pre className="mt-2 max-h-24 overflow-hidden whitespace-pre-wrap break-words border-t border-zinc-800 pt-2 font-mono text-[10px] leading-4 text-zinc-500">
          {preview.content.slice(-900)}
        </pre>
      )}
      {!worker.agent && <p className="mt-2 text-xs text-zinc-600">No active session</p>}
      {worker.agent?.has_chimera_endpoint && !focused && <p className="mt-2 text-xs text-zinc-600">Native session · expand to inspect</p>}
      {worker.agent && (
        <button type="button" onClick={onOpenSingle} className="mt-2 text-[10px] text-zinc-500 hover:text-indigo-300">
          Open only this agent
        </button>
      )}
    </article>
  )
}

export function SwarmCommandCenter({
  parentId,
  agents,
  status,
  focusedId,
  mobile,
  onFocus,
  onOpenSingle,
  renderParticipant,
}: {
  parentId: string
  agents: AgentInfo[]
  status: SwarmStatusData | null
  focusedId: string
  mobile: boolean
  onFocus: (taskId: string) => void
  onOpenSingle: (taskId: string) => void
  renderParticipant: (agent: AgentInfo) => React.ReactNode
}) {
  const conductor = agents.find((agent) => agent.parent_task_id === parentId && agent.role === "conductor") ?? null
  const workers = useMemo<WorkerView[]>(() => (status?.subtasks ?? []).map((subtask) => ({
    subtask,
    agent: agents.find((agent) => agent.parent_task_id === parentId && agent.subtask_id === subtask.id) ?? null,
  })), [agents, parentId, status])
  const focusedWorker = workers.find((worker) => worker.agent?.task_id === focusedId) ?? null
  const focusedAgent = focusedWorker?.agent ?? conductor
  const activeCount = workers.filter(({ subtask }) => ["dispatched", "in_progress", "reporting"].includes(subtask.status)).length

  if (!conductor) {
    return <LoadingState label="Loading the swarm conductor…" detail={parentId} />
  }

  if (mobile) {
    return (
      <div className="flex min-h-0 flex-1 flex-col bg-zinc-950">
        <div className="shrink-0 border-b border-zinc-800 p-2">
          <label className="block text-[10px] font-medium uppercase tracking-wide text-zinc-500" htmlFor="swarm-participant">Participant</label>
          <select
            id="swarm-participant"
            value={focusedAgent?.task_id ?? conductor.task_id}
            onChange={(event) => onFocus(event.target.value)}
            className="mt-1 min-h-10 w-full rounded border border-zinc-700 bg-zinc-900 px-3 text-sm text-zinc-200"
          >
            <option value={conductor.task_id}>Conductor · {conductor.activity || "idle"}</option>
            {workers.filter((worker) => worker.agent).map((worker) => (
              <option key={worker.subtask.id} value={worker.agent!.task_id}>{worker.subtask.title} · {lifecycleLabel(worker.subtask.status)}</option>
            ))}
          </select>
        </div>
        <div className="relative min-h-0 flex-1">{renderParticipant(focusedAgent ?? conductor)}</div>
        <details className="max-h-64 shrink-0 overflow-y-auto border-t border-zinc-800 bg-zinc-950">
          <summary className="cursor-pointer px-3 py-2 text-xs font-medium text-zinc-400">Workers ({workers.length}) · {activeCount} active</summary>
          <div className="space-y-2 p-2 pt-0">{workers.map((worker) => <WorkerCard key={worker.subtask.id} worker={worker} focused={worker.agent?.task_id === focusedId} onExpand={() => worker.agent && onFocus(worker.agent.task_id)} onOpenSingle={() => worker.agent && onOpenSingle(worker.agent.task_id)} />)}</div>
        </details>
      </div>
    )
  }

  return (
    <div className="grid min-h-0 flex-1 grid-cols-[minmax(0,55fr)_minmax(20rem,45fr)] gap-px bg-zinc-800">
      <section aria-label="Swarm conductor" className={cn("flex min-h-0 flex-col bg-zinc-950", focusedId === conductor.task_id && "ring-1 ring-inset ring-indigo-500")}>
        <button type="button" onClick={() => onFocus(conductor.task_id)} className="flex shrink-0 items-center gap-2 border-b border-zinc-800 px-3 py-2 text-left">
          <Bot size={14} className="text-indigo-400" />
          <span className="truncate text-xs font-medium text-zinc-200">Conductor</span>
          <span className="text-[10px] text-zinc-500">step {(status?.parent.active_step ?? 0) + 1}</span>
          <span className="ml-auto flex items-center gap-1 text-[10px] text-zinc-500"><Radio size={10} />{conductor.activity || "idle"}</span>
        </button>
        <div className="relative min-h-0 flex-1" onClick={() => onFocus(conductor.task_id)}>{renderParticipant(conductor)}</div>
      </section>

      <section aria-label="Swarm workers" className="flex min-h-0 flex-col bg-zinc-950">
        <header className="flex shrink-0 items-center gap-2 border-b border-zinc-800 px-3 py-2">
          {focusedWorker ? <ChevronLeft size={14} className="text-zinc-500" /> : <Users size={14} className="text-zinc-500" />}
          <span className="text-xs font-medium text-zinc-300">{focusedWorker ? focusedWorker.subtask.title : `Workers (${workers.length})`}</span>
          <span className="ml-auto text-[10px] text-zinc-500">{activeCount} active</span>
        </header>
        {focusedWorker?.agent ? (
          <>
            <div className={cn("relative min-h-0 flex-[7]", focusedId === focusedWorker.agent.task_id && "ring-1 ring-inset ring-indigo-500")} onClick={() => onFocus(focusedWorker.agent!.task_id)}>
              {renderParticipant(focusedWorker.agent)}
            </div>
            <div className="min-h-0 flex-[3] space-y-2 overflow-y-auto border-t border-zinc-800 p-2">
              {workers.filter((worker) => worker.subtask.id !== focusedWorker.subtask.id).map((worker) => <WorkerCard key={worker.subtask.id} worker={worker} focused={false} onExpand={() => worker.agent && onFocus(worker.agent.task_id)} onOpenSingle={() => worker.agent && onOpenSingle(worker.agent.task_id)} />)}
            </div>
          </>
        ) : (
          <div className="min-h-0 flex-1 space-y-2 overflow-y-auto p-2">
            {workers.length ? workers.map((worker) => <WorkerCard key={worker.subtask.id} worker={worker} focused={false} onExpand={() => worker.agent && onFocus(worker.agent.task_id)} onOpenSingle={() => worker.agent && onOpenSingle(worker.agent.task_id)} />) : <p className="p-4 text-sm text-zinc-600">The conductor is preparing the swarm plan.</p>}
          </div>
        )}
      </section>
    </div>
  )
}
