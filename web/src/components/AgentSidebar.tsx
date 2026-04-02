import type { AgentInfo } from "../hooks/useWebSocket"
import { cn } from "../lib/utils"

interface AgentSidebarProps {
  agents: AgentInfo[]
  selectedId: string | null
  onSelect: (taskId: string) => void
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

export function AgentSidebar({ agents, selectedId, onSelect }: AgentSidebarProps) {
  return (
    <div className="flex w-64 flex-col border-r border-zinc-800 bg-zinc-950 overflow-y-auto">
      <div className="px-3 py-3 text-xs font-medium uppercase tracking-wider text-zinc-500">
        Agents ({agents.length})
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
        </button>
      ))}
    </div>
  )
}
