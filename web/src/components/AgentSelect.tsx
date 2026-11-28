import { useCallback, useState, useRef, useEffect, type ReactNode, type RefObject } from "react"
import type { AgentInfo } from "../lib/agentWire"
import { cn } from "../lib/utils"
import { ChevronDown, ChevronRight, LayoutGrid, Plus } from "lucide-react"
import { AgentKindBadge, HostNetworkBadge, UnsandboxedBadge } from "./AgentKindBadge"
import { agentToolLabel, isAgentToolInForeground } from "../lib/agentKind"
import { agentSections, formatAgentTime, stateSeconds } from "../lib/agentList"
import { filterByStage, glanceLabels, glanceSection, needsYouOutside, stageLabel, unreadDone, type GlanceSection, type WorkStage } from "../lib/agentStage"
import { FinishedReason, NeedsYouLine, ReeveWaitLine, StageLine, StageTrack } from "./StageTrack"
import { StageMenu } from "./StageMenu"
import { subagentsLabel } from "../lib/agentGlance"
import { reeveWash } from "./ReeveEntry"
import { agentGroupLayout } from "../lib/agentGroups"
import type { AgentGrouping } from "./agent-groups/useAgentGroups"

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

interface PickerState {
  group: string
  tone: string
  stage: string
  time: string
}

// The agent's group, its stage and its time, as the trigger's second line
// reads them: "◆ Needs you · Implementing · 42m".
function pickerState(agent: AgentInfo): PickerState {
  const section = glanceSection(agent)
  const [group, tone] = agent.activity === "error" ? ["Error", "text-lw-error"] : [glanceLabels[section], groupTone[section]]
  return { group, tone, stage: stageLabel(agent.stage), time: formatAgentTime(stateSeconds(agent)) }
}

// The trigger's name for the agent in view: its title, then its state.
function pickerLabel(agent: AgentInfo | undefined): string {
  if (!agent) return "Choose an agent"
  const state = pickerState(agent)
  return `Agent: ${agentTitle(agent)}, ${[state.group, state.stage, state.time].filter(Boolean).join(", ").toLowerCase()}`
}

function agentTitle(agent: AgentInfo): string {
  return agent.task_title || agent.command || agent.task_id
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
  // without onStageFilter the list has no Stage menu.
  stageFilter?: WorkStage | null
  onStageFilter?: (stage: WorkStage | null) => void
}

// The badges before a row's ID: a conductor, or a worker's role.
function RoleBadge({ agent }: { agent: AgentInfo }) {
  if (agent.role === "conductor") {
    return (
      <span className="inline-flex items-center rounded bg-lw-accent/20 px-1.5 py-0.5 text-[10px] font-medium uppercase text-lw-link">
        Conductor
      </span>
    )
  }
  if (!agent.parent_task_id || !agent.role) return null
  return (
    <span className="inline-flex items-center rounded bg-lw-raised px-1.5 py-0.5 text-[10px] font-medium uppercase text-lw-text-2">
      {agent.role}
    </span>
  )
}

// A row's last line: the tool, the sandbox, a command other than the
// tool's, and the time in its state.
function AgentRowFoot({ agent }: { agent: AgentInfo }) {
  const time = formatAgentTime(stateSeconds(agent))
  return (
    <span className="flex min-w-0 items-center gap-1.5 pl-4">
      <AgentKindBadge kind={agent.agent_kind} engram={agent.engram} isNative={!!agent.has_chimera_endpoint} />
      {agent.unsandboxed && <UnsandboxedBadge />}
      {agent.docker_host_network && <HostNetworkBadge />}
      {!isAgentToolInForeground(agent.agent_kind, agent.command) && (
        <span className="min-w-0 truncate font-mono text-xs text-lw-text-2">{agent.command}</span>
      )}
      {time && <span className="ml-auto shrink-0 font-lw-mono text-xs text-lw-text-2">{time}</span>}
    </span>
  )
}

function AgentRow({ agent, selected, onSelect }: { agent: AgentInfo; selected: boolean; onSelect: (taskId: string) => void }) {
  const finished = glanceSection(agent) === "finished"
  const unread = unreadDone(agent)
  const subagents = subagentsLabel(agent.background_jobs)
  return (
    <button
      type="button"
      aria-current={selected ? "true" : undefined}
      onClick={() => onSelect(agent.task_id)}
      className={cn(
        "relative flex w-full flex-col gap-1 border-b border-lw-line px-3 py-2.5 text-left transition-colors last:border-b-0",
        selected ? "bg-lw-raised text-lw-text" : "text-lw-text-2 hover:bg-zinc-800/60 hover:text-lw-text",
        agent.reeve && reeveWash,
      )}
    >
      {unread && <span aria-label="Unread: finished with a note for you" title="Finished with a note for you" className="absolute inset-y-1 left-0 w-[3px] rounded-r bg-lw-done" />}
      <div className="flex items-center gap-2">
        <ActivityDot activity={agent.activity} />
        <RoleBadge agent={agent} />
        <span className="text-sm font-mono truncate">{agent.task_id}</span>
      </div>

      {agent.task_title && (
        <span className="truncate pl-4 text-sm text-lw-text">{agent.task_title}</span>
      )}
      {/* Under Finished the group names the stage: a row gives only why. */}
      {finished ? <FinishedReason agent={agent} unread={unread} className="pl-4" /> : <StageLine agent={agent} className="pl-4" />}
      <NeedsYouLine agent={agent} className="pl-4" />
      <ReeveWaitLine agent={agent} className="pl-4" />
      {subagents && (
        // The row is a button: the tree opens in the agent's header.
        <span className="truncate pl-4 text-[11px] text-lw-live">◌ {subagents}</span>
      )}
      <AgentRowFoot agent={agent} />
    </button>
  )
}

// The trigger's second line. What the agent is doing always shows. Its ID
// and its tool follow where there is room for them whole, the tool first
// to go: a fragment of either says nothing.
function PickerStateLine({ agent }: { agent: AgentInfo }) {
  const state = pickerState(agent)
  const tool = agentToolLabel(agent.agent_kind, agent.engram)
  return (
    <span className="flex min-w-0 items-center gap-1.5 overflow-hidden whitespace-nowrap text-xs leading-4 text-lw-text-2">
      <ActivityDot activity={agent.activity} />
      <span className={cn("shrink-0", state.tone)}>{state.group}</span>
      {state.stage && (
        <span className="flex shrink-0 items-center gap-1">
          <span aria-hidden="true" className="text-lw-faint">·</span>
          <StageTrack stage={agent.stage} size="xs" label="" />
          <span>{state.stage}</span>
        </span>
      )}
      {state.time && (
        <span className="shrink-0">
          <span aria-hidden="true" className="text-lw-faint">· </span>
          <span>{state.time}</span>
        </span>
      )}
      <span className="hidden shrink-0 @min-[12.5rem]:inline">
        <span aria-hidden="true" className="text-lw-faint">· </span>
        <span className="font-lw-mono">{agent.task_id}</span>
      </span>
      <span title={`${tool}, ${agent.has_chimera_endpoint ? "native" : "terminal"} session`} className="hidden shrink-0 @min-[18rem]:inline">
        <span aria-hidden="true" className="text-lw-faint">· </span>
        <span>{tool}</span>
      </span>
    </span>
  )
}

function PickerTrigger({ agent, open, onToggle }: { agent: AgentInfo | undefined; open: boolean; onToggle: () => void }) {
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-label={pickerLabel(agent)}
      aria-expanded={open}
      className={cn(
        "flex min-h-11 w-full min-w-0 items-center gap-2 rounded-lg border bg-lw-surface py-1.5 pl-3 pr-2 text-left font-lw transition-colors",
        open ? "border-lw-accent" : "border-lw-line hover:border-lw-line-strong",
      )}
    >
      {agent ? (
        <span className="@container flex min-w-0 flex-1 flex-col">
          <span className="truncate text-sm font-semibold leading-5 text-lw-text">{agentTitle(agent)}</span>
          <PickerStateLine agent={agent} />
        </span>
      ) : (
        <span className="flex-1 truncate text-sm text-lw-text-2">Choose an agent</span>
      )}
      <ChevronDown size={16} aria-hidden="true" className={cn("shrink-0 text-lw-text-2 transition-transform", open && "rotate-180")} />
    </button>
  )
}

// A list that is open closes on a click anywhere outside it.
function useClickOutside(open: boolean, containerRef: RefObject<HTMLDivElement | null>, close: () => void) {
  useEffect(() => {
    if (!open) return
    const onClick = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) close()
    }
    document.addEventListener("click", onClick, true)
    return () => document.removeEventListener("click", onClick, true)
  }, [open, containerRef, close])
}

interface ListProps {
  // The agents listed, after the stage filter, and all but the Reeve.
  agents: AgentInfo[]
  others: AgentInfo[]
  filter: WorkStage | null
  selectedId: string | null
  onSelect: (taskId: string) => void
  onStageFilter?: (stage: WorkStage | null) => void
}

// The top of the open list: the Reeve with the Stage menu beside it, how
// many agents there are, and those that need the person outside the filter.
function ListHead({ reeve, others, agents, filter, onStageFilter, onClose }: ListProps & { reeve?: ReactNode; onClose: () => void }) {
  const staging = Boolean(onStageFilter) && others.length > 0
  return (
    <>
      {(reeve || staging) && (
        // The top row: the Reeve, and the Stage menu beside it.
        <div className="flex items-center gap-1.5 border-b border-lw-line p-1">
          <div className="min-w-0 flex-1" onClick={onClose}>{reeve}</div>
          {staging && onStageFilter && <StageMenu agents={others} filter={filter} onFilter={onStageFilter} touch />}
        </div>
      )}
      {others.length > 0 && (
        <p className="m-0 px-3 pt-2 text-xs text-lw-text-2">
          {filter ? `${agents.length} of ${others.length}` : others.length} agent{others.length === 1 ? "" : "s"}
        </p>
      )}
      <OutsideFilter count={needsYouOutside(others, filter)} onStageFilter={onStageFilter} />
    </>
  )
}

// The agents that need the person but the stage filter hides.
function OutsideFilter({ count, onStageFilter }: { count: number; onStageFilter?: (stage: WorkStage | null) => void }) {
  if (count === 0) return null
  return (
    <div role="status" className="mx-3 my-2 flex items-center justify-between gap-2 rounded-lg border border-lw-line bg-lw-raised px-3 py-2 text-[13px] text-lw-needs">
      <span>◆ {count} need{count === 1 ? "s" : ""} you outside this filter</span>
      <button type="button" onClick={() => onStageFilter?.(null)} className="min-h-8 shrink-0 font-medium text-lw-link">Show</button>
    </div>
  )
}

function EmptyList({ filter, onStageFilter }: { filter: WorkStage | null; onStageFilter?: (stage: WorkStage | null) => void }) {
  return (
    <div className="px-3 py-4 text-center text-sm text-lw-text-2">
      {filter ? <>No agents are {stageLabel(filter).toLowerCase()}. <button type="button" onClick={() => onStageFilter?.(null)} className="text-lw-link">Show all</button></> : "No active agents"}
    </div>
  )
}

// The agents by group: each a header that collapses it, with a count of
// those that need the person while it is collapsed.
function GroupedAgents({ agents, grouping, selectedId, onSelect }: { agents: AgentInfo[]; grouping: AgentGrouping; selectedId: string | null; onSelect: (taskId: string) => void }) {
  return agentGroupLayout(agents, grouping.known).map((group) => {
    const label = group.name ?? "Ungrouped"
    const collapsed = grouping.isCollapsed(group.name)
    const hidden = collapsed && group.needsYou > 0
    return (
      <div key={label} className="border-b border-lw-line last:border-b-0">
        <button
          type="button"
          aria-expanded={!collapsed}
          aria-label={`${label}, ${group.agents.length}${hidden ? `, ${group.needsYou} need you` : ""}`}
          onClick={() => grouping.toggle(group.name)}
          className="flex min-h-11 w-full items-center gap-2 bg-lw-ground/60 px-3 text-left text-xs font-semibold text-lw-text"
        >
          <ChevronRight size={14} aria-hidden="true" className={cn("shrink-0 text-lw-text-2 transition-transform", !collapsed && "rotate-90")} />
          <span className={cn("min-w-0 truncate", !group.name && "text-lw-text-2")}>{label}</span>
          <span className="font-normal text-lw-faint">{group.agents.length}</span>
          {hidden && (
            <span aria-hidden="true" className="ml-auto flex h-5 min-w-5 items-center justify-center rounded-full bg-lw-needs px-1.5 text-[11px] font-semibold text-black">{group.needsYou}</span>
          )}
        </button>
        {!collapsed && group.agents.map((agent) => <AgentRow key={agent.task_id} agent={agent} selected={selectedId === agent.task_id} onSelect={onSelect} />)}
      </div>
    )
  })
}

// The agents under what they are doing, with the Reeve's agent first when
// it has no entry of its own.
function SectionedAgents({ agents, reeveAgent, selectedId, onSelect }: { agents: AgentInfo[]; reeveAgent?: AgentInfo; selectedId: string | null; onSelect: (taskId: string) => void }) {
  const row = (agent: AgentInfo) => <AgentRow key={agent.task_id} agent={agent} selected={selectedId === agent.task_id} onSelect={onSelect} />
  return [...(reeveAgent ? [row(reeveAgent)] : []), ...agentSections(agents).map(({ section, agents: members }) => (
    <section key={section} aria-label={glanceLabels[section]}>
      <h3 className={cn("m-0 px-3 pb-1 pt-2.5 text-[11px] font-semibold uppercase tracking-[1px]", groupTone[section], section === "your-turn" && "pl-6 opacity-80")}>
        {glanceLabels[section]} · {members.length}
      </h3>
      {members.map(row)}
    </section>
  ))]
}

export function AgentSelect({ agents: all, selectedId, onSelect, reeve, grouping, stageFilter = null, onStageFilter }: AgentSelectProps) {
  // The Reeve leads the list; with its own entry it is not listed again. It
  // has no stage, so a filter leaves it out.
  const others = all.filter((a) => !a.reeve)
  const filter = onStageFilter ? stageFilter : null
  const agents = filterByStage(others, filter)
  // Without an entry of its own, the Reeve's agent leads the list.
  const reeveAgent = reeve || filter ? undefined : all.find((a) => a.reeve)
  const [open, setOpen] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)
  const close = useCallback(() => setOpen(false), [])
  useClickOutside(open, containerRef, close)

  const handleSelect = (taskId: string) => {
    onSelect(taskId)
    setOpen(false)
  }
  const list = { agents, others, filter, selectedId, onSelect: handleSelect, onStageFilter }

  return (
    // min-w-0: a flex item is never narrower than its content otherwise, and
    // a long title would push the buttons beside it off a phone's screen.
    <div ref={containerRef} className="relative min-w-0 flex-1">
      <PickerTrigger agent={all.find((a) => a.task_id === selectedId)} open={open} onToggle={() => setOpen((v) => !v)} />

      {open && (
        <div role="group" aria-label="Running agents" className="absolute left-0 right-0 top-full z-50 mt-1 max-h-[60vh] overflow-y-auto rounded-lg border border-lw-line-strong bg-lw-surface font-lw shadow-xl">
          <ListHead {...list} reeve={reeve} onClose={close} />
          {agents.length === 0 && !reeve && !reeveAgent && <EmptyList filter={filter} onStageFilter={onStageFilter} />}
          {grouping?.mode === "group"
            ? <GroupedAgents agents={agents} grouping={grouping} selectedId={selectedId} onSelect={handleSelect} />
            : <SectionedAgents agents={agents} reeveAgent={reeveAgent} selectedId={selectedId} onSelect={handleSelect} />}
        </div>
      )}
    </div>
  )
}

interface AgentPickerBarProps extends AgentSelectProps {
  onEnsemble: () => void
  onSpawn: () => void
  usage?: ReactNode
}

// AgentPickerBar is the top of the agents page on a phone, where there is no
// room for the list: the agent in view, which opens the others, then usage,
// the ensemble and a new agent.
export function AgentPickerBar({ onEnsemble, onSpawn, usage, ...picker }: AgentPickerBarProps) {
  const square = "flex size-11 shrink-0 items-center justify-center rounded-lg transition-colors"
  return (
    <div className="flex shrink-0 items-center gap-1.5 border-b border-[var(--theme-border,#222228)] bg-lw-ground p-2">
      <AgentSelect {...picker} />
      {usage}
      <button type="button" onClick={onEnsemble} aria-label="Ensemble" title="Ensemble: every agent at once" className={cn(square, "text-[var(--theme-text,#c9c8d0)] hover:bg-lw-raised hover:text-lw-text")}>
        <LayoutGrid size={18} aria-hidden="true" />
      </button>
      <button type="button" onClick={onSpawn} aria-label="Spawn agent" title="Spawn agent" className={cn(square, "bg-lw-accent text-[var(--theme-canvas,#fff)] hover:brightness-110")}>
        <Plus size={18} aria-hidden="true" />
      </button>
    </div>
  )
}
