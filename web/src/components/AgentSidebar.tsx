import type { AgentInfo } from "../hooks/useWebSocket"
import { cn } from "../lib/utils"
import { Plus, Play, Pause } from "lucide-react"

function formatDuration(seconds: number): string {
  if (seconds < 60) return "<1m"
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  const hours = Math.floor(minutes / 60)
  const remainingMinutes = minutes % 60
  return `${hours}h ${remainingMinutes}m`
}

interface AgentSidebarProps {
  agents: AgentInfo[]
  selectedId: string | null
  onSelect: (taskId: string) => void
  onSpawn: () => void
}

function activityBadge(activity: string) {
  switch (activity) {
    case "working":
      return (
        <span className="inline-flex h-2 w-2 rounded-full bg-emerald-500" title="Working" />
      )
    case "waiting":
      return (
        <span className="inline-flex h-2 w-2 rounded-full bg-yellow-500" title="Waiting" />
      )
    default:
      return (
        <span className="inline-flex h-2 w-2 rounded-full bg-zinc-600" title="Idle" />
      )
  }
}

export function AgentSidebar({ agents, selectedId, onSelect, onSpawn }: AgentSidebarProps) {
  return (
    <div className="flex w-64 flex-col border-r border-zinc-800 bg-zinc-950 overflow-y-auto">
      <div className="flex items-center justify-between px-3 py-3">
        <span className="text-xs font-medium uppercase tracking-wider text-zinc-500">
          Agents ({agents.length})
        </span>
        <button
          onClick={onSpawn}
          className="rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
          title="Spawn new agent"
        >
          <Plus size={14} />
        </button>
      </div>
      {agents.map((agent) => (
        <button
          key={agent.task_id}
          onClick={() => onSelect(agent.task_id)}
          className={cn(
            "flex flex-col gap-0.5 px-3 py-2 text-left transition-colors border-l-2",
            selectedId === agent.task_id
              ? "border-l-indigo-500 bg-zinc-900 text-zinc-100"
              : "border-l-transparent text-zinc-400 hover:bg-zinc-900/50 hover:text-zinc-200"
          )}
        >
          <div className="flex items-center gap-2">
            {activityBadge(agent.activity)}
            <span className="text-sm font-mono truncate">{agent.task_id}</span>
          </div>
          {agent.task_title && (
            <span className="text-xs text-zinc-500 truncate pl-4">
              {agent.task_title}
            </span>
          )}
          <span className="text-xs text-zinc-600 truncate pl-4 font-mono">
            {agent.command}
          </span>
          {(agent.working_seconds >= 60 || agent.waiting_seconds >= 60) && (
            <div className="flex items-center gap-2 pl-4 text-[10px]">
              {agent.working_seconds >= 60 && (
                <span className="flex items-center gap-0.5 text-emerald-800">
                  <Play size={8} />
                  {formatDuration(agent.working_seconds)}
                </span>
              )}
              {agent.waiting_seconds >= 60 && (
                <span className="flex items-center gap-0.5 text-yellow-800">
                  <Pause size={8} />
                  {formatDuration(agent.waiting_seconds)}
                </span>
              )}
            </div>
          )}
        </button>
      ))}
    </div>
  )
}
