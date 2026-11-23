import { useState, useRef, useEffect, type ReactNode } from "react"
import type { AgentInfo } from "../hooks/useWebSocket"
import { cn } from "../lib/utils"
import { ChevronDown, ChevronRight, LayoutGrid, Plus } from "lucide-react"
import { AgentKindBadge, HostNetworkBadge, UnsandboxedBadge } from "./AgentKindBadge"
import { agentToolLabel, isAgentToolInForeground } from "../lib/agentKind"
import { agentSections, formatAgentTime, stateSeconds } from "../lib/agentList"
import { filterByStage, glanceLabels, glanceSection, needsYouOutside, stageCounts, stageLabel, workStages, type GlanceSection, type WorkStage } from "../lib/agentStage"
import { NeedsYouLine, StageLine, StageTrack } from "./StageTrack"
import { reeveWash } from "./ReeveEntry"
import { agentGroupLayout } from "../lib/agentGroups"
import type { AgentGrouping } from "../hooks/useAgentGroups"

function ActivityDot({ activity }: { activity: string }) {
  switch (activity) {
    case "working":
      return <span className="inline-flex size-2 shrink-0 rounded-full bg-lw-live" title="Working" />
    case "waiting":
      return <span className="inline-flex size-2 shrink-0 rounded-full bg-lw-needs" title="Waiting" />
    case "background":
      // Its turn ended with a shell or subagent of its own still running.
      return <span className="inline-flex size-2 shrink-0 rounded-full bg-lw-live" title="Background work running" />
    case "error":
      return <span className="inline-flex size-2 shrink-0 rounded-full bg-lw-error" title="Error" />
    default:
      return <span className="inline-flex size-2 shrink-0 rounded-full bg-[var(--theme-border,#4a4955)]" title="Idle" />
  }
}

const groupTone: Record<GlanceSection, string> = {
  "needs-you": "text-lw-needs",
  "your-turn": "text-lw-needs",
  working: "text-lw-live",
  finished: "text-lw-done",
  ended: "text-lw-text-2",
}

// The agent's group, its stage and its time, as the trigger's second line
// reads them: "◆ Needs you · Implementing · 42m".
function pickerState(agent: AgentInfo): { group: string; tone: string; stage: string; time: string } {
  const section = glanceSection(agent)
  const [group, tone] = agent.activity === "error" ? ["Error", "text-lw-error"] : [glanceLabels[section], groupTone[section]]
  return { group, tone, stage: stageLabel(agent.stage), time: formatAgentTime(stateSeconds(agent)) }
}

interface AgentSelectProps {
  agents: AgentInfo[]
  selectedId: string | null
  onSelect: (taskId: string) => void
  // Leads the list: the Reeve (ReeveEntry), whether or not it runs.
  reeve?: ReactNode
  // Lists the agents by group (useAgentGroups) when its mode is group.
  grouping?: AgentGrouping
  // The stage the list is filtered to (null for all) and how it changes;
  // without onStageFilter the list has no stage chips.
  stageFilter?: WorkStage | null
  onStageFilter?: (stage: WorkStage | null) => void
}

export function AgentSelect({ agents: all, selectedId, onSelect, reeve, grouping, stageFilter = null, onStageFilter }: AgentSelectProps) {
  // The Reeve leads the list; with its own entry it is not listed again. It
  // has no stage, so a filter leaves it out.
  const others = all.filter((a) => !a.reeve)
  const filter = onStageFilter ? stageFilter : null
  const outside = needsYouOutside(others, filter)
  const shown = filterByStage(others, filter)
  const agents = shown
  // Without an entry of its own, the Reeve's agent leads the list.
  const reeveAgent = reeve || filter ? undefined : all.find((a) => a.reeve)
  const [open, setOpen] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)
  const selectedAgent = all.find((a) => a.task_id === selectedId)
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

  const renderAgent = (agent: AgentInfo) => {
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
                  isSelected ? "bg-lw-raised text-lw-text" : "text-lw-text-2 hover:bg-zinc-800/60 hover:text-lw-text",
                  agent.reeve && reeveWash,
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
                  <span className="truncate pl-4 text-sm text-lw-text">{agent.task_title}</span>
                )}
                <StageLine agent={agent} className="pl-4" />
                <NeedsYouLine agent={agent} className="pl-4" />
                <span className="flex min-w-0 items-center gap-1.5 pl-4">
                  <AgentKindBadge kind={agent.agent_kind} engram={agent.engram} isNative={!!agent.has_chimera_endpoint} />
                  {agent.unsandboxed && <UnsandboxedBadge />}
                  {agent.docker_host_network && <HostNetworkBadge />}
                  {!isAgentToolInForeground(agent.agent_kind, agent.command) && (
                    <span className="min-w-0 truncate font-mono text-xs text-lw-text-2">{agent.command}</span>
                  )}
                  {formatAgentTime(stateSeconds(agent)) && <span className="ml-auto shrink-0 font-lw-mono text-xs text-lw-text-2">{formatAgentTime(stateSeconds(agent))}</span>}
                </span>
              </button>
            )
  }

  return (
    // min-w-0: a flex item is never narrower than its content otherwise, and
    // a long title would push the buttons beside it off a phone's screen.
    <div ref={containerRef} className="relative min-w-0 flex-1">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-label={selectedAgent ? `Agent: ${title}, ${[state!.group, state!.stage, state!.time].filter(Boolean).join(", ").toLowerCase()}` : "Choose an agent"}
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
              <span className={cn("shrink-0", state!.tone)}>{state!.group}</span>
              {state!.stage && (
                <span className="flex shrink-0 items-center gap-1">
                  <span aria-hidden="true" className="text-lw-faint">·</span>
                  <StageTrack stage={selectedAgent.stage} size="xs" label="" />
                  <span>{state!.stage}</span>
                </span>
              )}
              {state!.time && (
                <span className="shrink-0">
                  <span aria-hidden="true" className="text-lw-faint">· </span>
                  <span>{state!.time}</span>
                </span>
              )}
              <span className="hidden shrink-0 @min-[12.5rem]:inline">
                <span aria-hidden="true" className="text-lw-faint">· </span>
                <span className="font-lw-mono">{selectedAgent.task_id}</span>
              </span>
              <span title={`${agentToolLabel(selectedAgent.agent_kind, selectedAgent.engram)}, ${selectedAgent.has_chimera_endpoint ? "native" : "terminal"} session`} className="hidden shrink-0 @min-[18rem]:inline">
                <span aria-hidden="true" className="text-lw-faint">· </span>
                <span>{agentToolLabel(selectedAgent.agent_kind, selectedAgent.engram)}</span>
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
          {reeve && <div className="border-b border-lw-line p-1" onClick={() => setOpen(false)}>{reeve}</div>}
          {onStageFilter && others.length > 0 && (
            <div role="group" aria-label="Filter by stage" className="flex gap-2 overflow-x-auto border-b border-lw-line px-3 py-2">
              {[null, ...workStages].map((stage) => {
                const on = filter === stage
                const count = stage ? stageCounts(others)[stage] : others.length
                return (
                  <button
                    key={stage ?? "all"}
                    type="button"
                    aria-pressed={on}
                    onClick={() => onStageFilter(stage)}
                    className={cn("flex min-h-11 shrink-0 items-center gap-1.5 rounded-full border px-3 text-[13px] font-medium", on ? "border-lw-accent bg-lw-selected text-lw-text" : "border-lw-line bg-lw-raised text-lw-text")}
                  >
                    {stage && <StageTrack stage={stage} label="" />}
                    {stage ? stageLabel(stage) : "All"}
                    <span className="font-lw-mono font-normal text-lw-text-2">{count}</span>
                  </button>
                )
              })}
            </div>
          )}
          {outside > 0 && (
            <div role="status" className="mx-3 my-2 flex items-center justify-between gap-2 rounded-lg border border-lw-line bg-lw-raised px-3 py-2 text-[13px] text-lw-needs">
              <span>◆ {outside} need{outside === 1 ? "s" : ""} you outside this filter</span>
              <button type="button" onClick={() => onStageFilter?.(null)} className="min-h-8 shrink-0 font-medium text-lw-link">Show</button>
            </div>
          )}
          {agents.length === 0 && !reeve && !reeveAgent && (
            <div className="px-3 py-4 text-center text-sm text-lw-text-2">
              {filter ? <>No agents are {stageLabel(filter).toLowerCase()}. <button type="button" onClick={() => onStageFilter?.(null)} className="text-lw-link">Show all</button></> : "No active agents"}
            </div>
          )}
          {grouping?.mode === "group"
            ? agentGroupLayout(agents, grouping.known).map((group) => {
              const label = group.name ?? "Ungrouped"
              const collapsed = grouping.isCollapsed(group.name)
              return (
                <div key={label} className="border-b border-lw-line last:border-b-0">
                  <button
                    type="button"
                    aria-expanded={!collapsed}
                    aria-label={`${label}, ${group.agents.length}${collapsed && group.needsYou > 0 ? `, ${group.needsYou} need you` : ""}`}
                    onClick={() => grouping.toggle(group.name)}
                    className="flex min-h-11 w-full items-center gap-2 bg-lw-ground/60 px-3 text-left text-xs font-semibold text-lw-text"
                  >
                    <ChevronRight size={14} aria-hidden="true" className={cn("shrink-0 text-lw-text-2 transition-transform", !collapsed && "rotate-90")} />
                    <span className={cn("min-w-0 truncate", !group.name && "text-lw-text-2")}>{label}</span>
                    <span className="font-normal text-lw-faint">{group.agents.length}</span>
                    {collapsed && group.needsYou > 0 && (
                      <span aria-hidden="true" className="ml-auto flex h-5 min-w-5 items-center justify-center rounded-full bg-lw-needs px-1.5 text-[11px] font-semibold text-black">{group.needsYou}</span>
                    )}
                  </button>
                  {!collapsed && group.agents.map(renderAgent)}
                </div>
              )
            })
            : [...(reeveAgent ? [renderAgent(reeveAgent)] : []), ...agentSections(agents).map(({ section, agents: members }) => (
              <section key={section} aria-label={glanceLabels[section]}>
                <h3 className={cn("m-0 px-3 pb-1 pt-2.5 text-[11px] font-semibold uppercase tracking-[1px]", groupTone[section], section === "your-turn" && "pl-6 opacity-80")}>
                  {glanceLabels[section]} · {members.length}
                </h3>
                {members.map(renderAgent)}
              </section>
            ))]}
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
    <div className="flex shrink-0 items-center gap-1.5 border-b border-[var(--theme-border,#222228)] bg-lw-ground p-2">
      <AgentSelect {...picker} />
      <button type="button" onClick={onEnsemble} aria-label="Ensemble" title="Ensemble: every agent at once" className={cn(square, "text-[var(--theme-text,#c9c8d0)] hover:bg-lw-raised hover:text-lw-text")}>
        <LayoutGrid size={18} aria-hidden="true" />
      </button>
      <button type="button" onClick={onSpawn} aria-label="Spawn agent" title="Spawn agent" className={cn(square, "bg-lw-accent text-[var(--theme-canvas,#fff)] hover:brightness-110")}>
        <Plus size={18} aria-hidden="true" />
      </button>
    </div>
  )
}
