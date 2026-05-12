import type { AgentInfo } from "../hooks/useWebSocket"
import { cn } from "../lib/utils"
import { Plus, Play, Pause, Layers } from "lucide-react"
import { AgentActionMenu } from "./AgentActionMenu"

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
  onStartSwarm?: () => void
  modifierHeld?: boolean
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

// groupAndSortAgents groups agents by parent_task_id (conductor first within
// each group), with solo (non-swarm) sessions appearing after all swarm groups.
// Returns a flat list with index-stable ordering for keyboard switching, plus
// a parallel array of grouping metadata for rendering.
interface GroupedAgent {
  agent: AgentInfo
  groupStart: boolean // true if this entry begins a new swarm group
  groupParentId: string // parent_task_id, or "" for solo
  isSoloDivider: boolean // true for the first solo session after swarm groups
}

function groupAndSortAgents(agents: AgentInfo[]): GroupedAgent[] {
  const swarmsOrder: string[] = []
  const swarms: Record<string, AgentInfo[]> = {}
  const solo: AgentInfo[] = []

  for (const a of agents) {
    if (!a.parent_task_id) {
      solo.push(a)
      continue
    }
    if (!swarms[a.parent_task_id]) {
      swarmsOrder.push(a.parent_task_id)
      swarms[a.parent_task_id] = []
    }
    swarms[a.parent_task_id].push(a)
  }

  const result: GroupedAgent[] = []
  let hadSwarms = false
  for (const parentId of swarmsOrder) {
    hadSwarms = true
    const group = swarms[parentId]
    // Conductor first.
    const conductor = group.find((g) => g.role === "conductor")
    const workers = group.filter((g) => g.role !== "conductor")
    let first = true
    if (conductor) {
      result.push({ agent: conductor, groupStart: first, groupParentId: parentId, isSoloDivider: false })
      first = false
    }
    for (const w of workers) {
      result.push({ agent: w, groupStart: first, groupParentId: parentId, isSoloDivider: false })
      first = false
    }
  }
  for (let i = 0; i < solo.length; i++) {
    result.push({
      agent: solo[i],
      groupStart: false,
      groupParentId: "",
      isSoloDivider: i === 0 && hadSwarms,
    })
  }
  return result
}

export function AgentSidebar({ agents, selectedId, onSelect, onSpawn, onStartSwarm, modifierHeld }: AgentSidebarProps) {
  const grouped = groupAndSortAgents(agents)

  return (
    <div className="flex w-64 flex-col border-r border-zinc-800 bg-zinc-950 overflow-y-auto">
      <div className="flex items-center justify-between px-3 py-3">
        <span className="text-xs font-medium uppercase tracking-wider text-zinc-500">
          Agents ({agents.length})
        </span>
        <div className="flex items-center gap-0.5">
          {onStartSwarm && (
            <button
              onClick={onStartSwarm}
              className="rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-indigo-300"
              title="Start swarm"
            >
              <Layers size={14} />
            </button>
          )}
          <button
            onClick={onSpawn}
            className="rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
            title="Spawn new agent"
          >
            <Plus size={14} />
          </button>
        </div>
      </div>
      {grouped.map(({ agent, groupStart, groupParentId, isSoloDivider }, index) => {
        const isConductor = agent.role === "conductor"
        const isWorker = agent.parent_task_id && !isConductor
        const isSwarm = !!agent.parent_task_id

        return (
          <div key={agent.task_id}>
            {groupStart && (
              <div className="px-3 pt-3 pb-1 text-[10px] font-semibold uppercase tracking-wider text-indigo-400">
                ◆ swarm: <span className="font-mono normal-case">{groupParentId}</span>
              </div>
            )}
            {isSoloDivider && (
              <div className="px-3 pt-3 pb-1 text-[10px] uppercase tracking-wider text-zinc-600">
                ── solo ──
              </div>
            )}
            <div className={cn(
              "relative flex items-center border-l-2 transition-colors group",
              isSwarm && !isConductor && "ml-2", // worker indent
              selectedId === agent.task_id
                ? isSwarm
                  ? "border-l-indigo-500 bg-zinc-900 text-zinc-100"
                  : "border-l-indigo-500 bg-zinc-900 text-zinc-100"
                : isSwarm
                ? "border-l-indigo-700/60 text-zinc-400 hover:bg-zinc-900/50 hover:text-zinc-200"
                : "border-l-transparent text-zinc-400 hover:bg-zinc-900/50 hover:text-zinc-200"
            )}>
              <button
                onClick={() => onSelect(agent.task_id)}
                className="flex flex-1 flex-col gap-0.5 px-3 py-2 text-left"
              >
                {modifierHeld && index < 9 && (
                  <span className="absolute right-2 top-1/2 -translate-y-1/2 flex h-5 w-5 items-center justify-center rounded bg-indigo-600 text-[10px] font-bold text-white">
                    {index + 1}
                  </span>
                )}
                <div className="flex items-center gap-2">
                  {activityBadge(agent.activity)}
                  {isConductor && (
                    <span className="inline-flex items-center rounded bg-indigo-600/30 px-1.5 py-0.5 text-[10px] font-medium uppercase text-indigo-300" title="Swarm conductor">
                      ◆ conductor
                    </span>
                  )}
                  {isWorker && agent.role && (
                    <span className="inline-flex items-center rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] font-medium uppercase text-zinc-400" title={`Worker role: ${agent.role}`}>
                      {agent.role}
                    </span>
                  )}
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
                {agent.state_timeline && agent.state_timeline.length > 0 && (
                  <div className="flex items-center gap-0.5 pl-4">
                    {agent.state_timeline.map((s, idx) => (
                      <div
                        key={idx}
                        className="h-2 flex-1 rounded-sm"
                        style={{
                          backgroundColor:
                            s === "working" ? "#10b981" :
                            s === "waiting" ? "#eab308" :
                            "#71717a",
                        }}
                      />
                    ))}
                  </div>
                )}
              </button>
              {isSwarm && (
                <div className="pr-2">
                  <AgentActionMenu
                    agentTaskId={agent.task_id}
                    parentTaskId={agent.parent_task_id ?? ""}
                    subtaskId={agent.subtask_id ?? agent.task_id}
                    role={agent.role ?? "worker"}
                  />
                </div>
              )}
            </div>
          </div>
        )
      })}
    </div>
  )
}
