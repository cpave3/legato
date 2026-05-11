import { useState, useRef, useEffect } from "react"
import type { AgentInfo } from "../hooks/useWebSocket"
import { cn } from "../lib/utils"
import { ChevronDown, Play, Pause } from "lucide-react"

function formatDuration(seconds: number): string {
  if (seconds < 60) return "<1m"
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  const hours = Math.floor(minutes / 60)
  const remainingMinutes = minutes % 60
  return `${hours}h ${remainingMinutes}m`
}

function ActivityDot({ activity }: { activity: string }) {
  switch (activity) {
    case "working":
      return <span className="inline-flex h-2 w-2 rounded-full bg-emerald-500" title="Working" />
    case "waiting":
      return <span className="inline-flex h-2 w-2 rounded-full bg-yellow-500" title="Waiting" />
    default:
      return <span className="inline-flex h-2 w-2 rounded-full bg-zinc-600" title="Idle" />
  }
}

function ActivityLabel({ activity }: { activity: string }) {
  switch (activity) {
    case "working":
      return <span className="text-[10px] font-medium uppercase text-emerald-400">Working</span>
    case "waiting":
      return <span className="text-[10px] font-medium uppercase text-yellow-400">Waiting</span>
    default:
      return <span className="text-[10px] font-medium uppercase text-zinc-500">Idle</span>
  }
}

interface AgentSelectProps {
  agents: AgentInfo[]
  selectedId: string | null
  onSelect: (taskId: string) => void
}

export function AgentSelect({ agents, selectedId, onSelect }: AgentSelectProps) {
  const [open, setOpen] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)
  const selectedAgent = agents.find((a) => a.task_id === selectedId)

  useEffect(() => {
    if (!open) return
    const onClick = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener("click", onClick, true)
    return () => document.removeEventListener("click", onClick, true)
  }, [open])

  const handleSelect = (taskId: string) => {
    onSelect(taskId)
    setOpen(false)
  }

  return (
    <div ref={containerRef} className="relative flex-1">
      <button
        onClick={() => setOpen((v) => !v)}
        className={cn(
          "flex w-full items-center gap-2 rounded border bg-zinc-900 px-3 py-2 text-left transition-colors",
          open
            ? "border-zinc-500 text-zinc-100"
            : "border-zinc-700 text-zinc-200 hover:border-zinc-600"
        )}
      >
        {selectedAgent ? (
          <>
            <ActivityDot activity={selectedAgent.activity} />
            <div className="flex flex-1 flex-col overflow-hidden">
              <span className="truncate text-sm font-medium">
                {selectedAgent.task_title || selectedAgent.command}
              </span>
              <span className="truncate text-xs text-zinc-500 font-mono">
                {selectedAgent.task_id}
              </span>
            </div>
          </>
        ) : (
          <span className="flex-1 truncate text-sm text-zinc-500">Select agent…</span>
        )}
        <ChevronDown size={14} className={cn("shrink-0 text-zinc-500 transition-transform", open && "rotate-180")} />
      </button>

      {open && (
        <div className="absolute left-0 right-0 top-full z-50 mt-1 max-h-[60vh] overflow-y-auto rounded border border-zinc-700 bg-zinc-900 shadow-xl">
          {agents.length === 0 && (
            <div className="px-3 py-4 text-center text-sm text-zinc-500">
              No active agents
            </div>
          )}
          {agents.map((agent) => {
            const isSelected = selectedId === agent.task_id
            const isConductor = agent.role === "conductor"
            const isWorker = !!agent.parent_task_id && !isConductor
            return (
              <button
                key={agent.task_id}
                onClick={() => handleSelect(agent.task_id)}
                className={cn(
                  "flex w-full flex-col gap-1 border-b border-zinc-800 px-3 py-2.5 text-left transition-colors last:border-b-0",
                  isSelected ? "bg-zinc-800 text-zinc-100" : "text-zinc-400 hover:bg-zinc-800/60 hover:text-zinc-200"
                )}
              >
                <div className="flex items-center gap-2">
                  <ActivityDot activity={agent.activity} />
                  {isConductor && (
                    <span className="inline-flex items-center rounded bg-indigo-600/30 px-1.5 py-0.5 text-[10px] font-medium uppercase text-indigo-300">
                      Conductor
                    </span>
                  )}
                  {isWorker && agent.role && (
                    <span className="inline-flex items-center rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] font-medium uppercase text-zinc-400">
                      {agent.role}
                    </span>
                  )}
                  <span className="text-sm font-mono truncate">{agent.task_id}</span>
                </div>

                {agent.task_title && (
                  <span className="truncate pl-4 text-xs text-zinc-300">{agent.task_title}</span>
                )}
                <span className="truncate pl-4 text-xs font-mono text-zinc-500">{agent.command}</span>

                <div className="flex items-center gap-2 pl-4">
                  <ActivityLabel activity={agent.activity} />
                  {(agent.working_seconds >= 60 || agent.waiting_seconds >= 60) && (
                    <span className="text-[10px] text-zinc-500">·</span>
                  )}
                  {agent.working_seconds >= 60 && (
                    <span className="flex items-center gap-0.5 text-[10px] text-emerald-600">
                      <Play size={8} />
                      {formatDuration(agent.working_seconds)}
                    </span>
                  )}
                  {agent.waiting_seconds >= 60 && (
                    <span className="flex items-center gap-0.5 text-[10px] text-yellow-700">
                      <Pause size={8} />
                      {formatDuration(agent.waiting_seconds)}
                    </span>
                  )}
                </div>
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}
