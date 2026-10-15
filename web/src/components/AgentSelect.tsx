import { useState, useRef, useEffect } from "react"
import type { AgentInfo } from "../hooks/useWebSocket"
import { cn } from "../lib/utils"
import { ChevronDown, Play, Pause, AlertTriangle, LayoutGrid, Plus } from "lucide-react"
import { AgentKindBadge } from "./AgentKindBadge"
import { agentKindLabel, isAgentToolInForeground } from "../lib/agentKind"
import { ensembleSection } from "../lib/agentGlance"
import { formatAgentTime, sectionLabels, stateSeconds } from "../lib/agentList"

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
      return <span className="inline-flex size-2 shrink-0 rounded-full bg-lw-live" title="Working" />
    case "waiting":
      return <span className="inline-flex size-2 shrink-0 rounded-full bg-lw-needs" title="Waiting" />
    case "background":
      // Its turn ended with a shell or subagent of its own still running.
      return <span className="inline-flex size-2 shrink-0 rounded-full bg-lw-done" title="Background work running" />
    case "error":
      return <span className="inline-flex size-2 shrink-0 rounded-full bg-lw-error" title="Error" />
    default:
      return <span className="inline-flex size-2 shrink-0 rounded-full bg-[#4a4955]" title="Idle" />
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

const stateTone = {
  "needs-you": "text-lw-needs",
  running: "text-lw-live",
  background: "text-lw-done",
  idle: "text-lw-text-2",
}

// What an agent is doing, with its time, as the line under its title says it:
// "Running 4h 4m".
function pickerState(agent: AgentInfo): { text: string; tone: string } {
  const time = formatAgentTime(stateSeconds(agent))
  const section = ensembleSection(agent)
  const [label, tone] = agent.activity === "error" ? ["Error", "text-lw-error"] : [sectionLabels[section], stateTone[section]]
  return { text: time ? `${label} ${time}` : label, tone }
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
  const title = selectedAgent ? selectedAgent.task_title || selectedAgent.command || selectedAgent.task_id : ""
  const state = selectedAgent ? pickerState(selectedAgent) : null

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
    // min-w-0: a flex item is never narrower than its content otherwise, and
    // a long title would push the buttons beside it off a phone's screen.
    <div ref={containerRef} className="relative min-w-0 flex-1">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-label={selectedAgent ? `Agent: ${title}, ${state!.text.toLowerCase()}` : "Choose an agent"}
        aria-expanded={open}
        className={cn(
          "flex min-h-11 w-full min-w-0 items-center gap-2 rounded-lg border bg-lw-surface py-1.5 pl-3 pr-2 text-left font-lw transition-colors",
          open ? "border-lw-accent" : "border-lw-line hover:border-lw-line-strong",
        )}
      >
        {selectedAgent ? (
          <span className="@container flex min-w-0 flex-1 flex-col">
            <span className="truncate text-sm font-semibold leading-5 text-lw-text">{title}</span>
            {/* What the agent is doing always shows. Its ID and its tool
                follow where there is room for them whole, the tool first to
                go: a fragment of either says nothing. */}
            <span className="flex min-w-0 items-center gap-1.5 overflow-hidden whitespace-nowrap text-xs leading-4 text-lw-text-2">
              <ActivityDot activity={selectedAgent.activity} />
              <span className={cn("shrink-0", state!.tone)}>{state!.text}</span>
              <span className="hidden shrink-0 @min-[12.5rem]:inline">
                <span aria-hidden="true" className="text-lw-faint">· </span>
                <span className="font-lw-mono">{selectedAgent.task_id}</span>
              </span>
              <span title={`${agentKindLabel(selectedAgent.agent_kind)}, ${selectedAgent.has_chimera_endpoint ? "native" : "terminal"} session`} className="hidden shrink-0 @min-[18rem]:inline">
                <span aria-hidden="true" className="text-lw-faint">· </span>
                <span>{agentKindLabel(selectedAgent.agent_kind)}</span>
              </span>
            </span>
          </span>
        ) : (
          <span className="flex-1 truncate text-sm text-lw-text-2">Choose an agent</span>
        )}
        <ChevronDown size={16} aria-hidden="true" className={cn("shrink-0 text-lw-text-2 transition-transform", open && "rotate-180")} />
      </button>

      {open && (
        <div role="group" aria-label="Running agents" className="absolute left-0 right-0 top-full z-50 mt-1 max-h-[60vh] overflow-y-auto rounded-lg border border-lw-line-strong bg-lw-surface font-lw shadow-xl">
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
                type="button"
                aria-current={isSelected ? "true" : undefined}
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

interface AgentPickerBarProps extends AgentSelectProps {
  onEnsemble: () => void
  onSpawn: () => void
}

// AgentPickerBar is the top of the agents page on a phone, where there is no
// room for the list: the agent in view, which opens the others, then the
// ensemble and a new agent.
export function AgentPickerBar({ onEnsemble, onSpawn, ...picker }: AgentPickerBarProps) {
  const square = "flex size-11 shrink-0 items-center justify-center rounded-lg transition-colors"
  return (
    <div className="flex shrink-0 items-center gap-1.5 border-b border-[#222228] bg-lw-ground p-2">
      <AgentSelect {...picker} />
      <button type="button" onClick={onEnsemble} aria-label="Ensemble" title="Ensemble: every agent at once" className={cn(square, "text-[#c9c8d0] hover:bg-lw-raised hover:text-lw-text")}>
        <LayoutGrid size={18} aria-hidden="true" />
      </button>
      <button type="button" onClick={onSpawn} aria-label="Spawn agent" title="Spawn agent" className={cn(square, "bg-lw-accent text-white hover:brightness-110")}>
        <Plus size={18} aria-hidden="true" />
      </button>
    </div>
  )
}
