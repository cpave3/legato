import { useState, useRef, useEffect } from "react"
import type { AgentInfo } from "../hooks/useWebSocket"
import { cn } from "../lib/utils"
import { ChevronDown, Play, Pause, AlertTriangle } from "lucide-react"
import { AgentKindBadge } from "./AgentKindBadge"
import { isAgentToolInForeground } from "../lib/agentKind"

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
      return <span className="inline-flex h-2 w-2 rounded-full bg-lw-live" title="Working" />
    case "waiting":
      return <span className="inline-flex h-2 w-2 rounded-full bg-lw-needs" title="Waiting" />
    case "background":
      // Its turn ended with a shell or subagent of its own still running.
      return <span className="inline-flex h-2 w-2 rounded-full bg-lw-done" title="Background work running" />
    case "error":
      return <span className="inline-flex h-2 w-2 rounded-full bg-lw-error" title="Error" />
    default:
      return <span className="inline-flex h-2 w-2 rounded-full bg-[#4a4955]" title="Idle" />
  }
}

function ActivityLabel({ activity }: { activity: string }) {
  switch (activity) {
    case "working":
      return <span className="text-[10px] font-medium uppercase text-lw-live">Working</span>
    case "waiting":
      return <span className="text-[10px] font-medium uppercase text-lw-needs">Waiting</span>
    case "background":
      return <span className="text-[10px] font-medium uppercase text-lw-done">Background</span>
    case "error":
      return <span className="text-[10px] font-medium uppercase text-lw-error">Error</span>
    default:
      return <span className="text-[10px] font-medium uppercase text-lw-text-2">Idle</span>
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
          "flex w-full items-center gap-2 rounded border bg-lw-surface px-3 py-2 text-left transition-colors",
          open
            ? "border-zinc-500 text-lw-text"
            : "border-lw-line-strong text-lw-text hover:border-[#4a4955]"
        )}
      >
        {selectedAgent ? (
          <>
            <ActivityDot activity={selectedAgent.activity} />
            <div className="flex flex-1 flex-col overflow-hidden">
              <span className="truncate text-sm font-medium">
                {selectedAgent.task_title || selectedAgent.command}
              </span>
              <span className="truncate text-xs text-lw-text-2 font-mono">
                {selectedAgent.task_id}
              </span>
            </div>
            <AgentKindBadge kind={selectedAgent.agent_kind} isNative={!!selectedAgent.has_chimera_endpoint} />
          </>
        ) : (
          <span className="flex-1 truncate text-sm text-lw-text-2">Select agent…</span>
        )}
        <ChevronDown size={14} className={cn("shrink-0 text-lw-text-2 transition-transform", open && "rotate-180")} />
      </button>

      {open && (
        <div className="absolute left-0 right-0 top-full z-50 mt-1 max-h-[60vh] overflow-y-auto rounded border border-lw-line-strong bg-lw-surface shadow-xl">
          {agents.length === 0 && (
            <div className="px-3 py-4 text-center text-sm text-lw-text-2">
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
                  "flex w-full flex-col gap-1 border-b border-lw-line px-3 py-2.5 text-left transition-colors last:border-b-0",
                  isSelected ? "bg-lw-raised text-lw-text" : "text-lw-text-2 hover:bg-zinc-800/60 hover:text-lw-text"
                )}
              >
                <div className="flex items-center gap-2">
                  <ActivityDot activity={agent.activity} />
                  {isConductor && (
                    <span className="inline-flex items-center rounded bg-lw-accent/20 px-1.5 py-0.5 text-[10px] font-medium uppercase text-lw-link">
                      Conductor
                    </span>
                  )}
                  {isWorker && agent.role && (
                    <span className="inline-flex items-center rounded bg-lw-raised px-1.5 py-0.5 text-[10px] font-medium uppercase text-lw-text-2">
                      {agent.role}
                    </span>
                  )}
                  <span className="text-sm font-mono truncate">{agent.task_id}</span>
                </div>

                {agent.task_title && (
                  <span className="truncate pl-4 text-xs text-[#c9c8d0]">{agent.task_title}</span>
                )}
                <span className="flex min-w-0 items-center gap-1.5 pl-4">
                  <AgentKindBadge kind={agent.agent_kind} isNative={!!agent.has_chimera_endpoint} />
                  {!isAgentToolInForeground(agent.agent_kind, agent.command) && (
                    <span className="min-w-0 truncate font-mono text-xs text-lw-text-2">{agent.command}</span>
                  )}
                </span>

                <div className="flex items-center gap-2 pl-4">
                  <ActivityLabel activity={agent.activity} />
                  {(agent.working_seconds >= 60 || agent.waiting_seconds >= 60 || agent.error_seconds >= 60) && (
                    <span className="text-[10px] text-lw-text-2">·</span>
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
                  {agent.error_seconds >= 60 && (
                    <span className="flex items-center gap-0.5 text-[10px] text-red-600">
                      <AlertTriangle size={8} />
                      {formatDuration(agent.error_seconds)}
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
