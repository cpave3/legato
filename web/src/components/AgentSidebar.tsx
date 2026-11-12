import { useEffect, useRef, useState, type DragEvent, type ReactNode } from "react";
import type { AgentInfo } from "../hooks/useWebSocket";
import type { AgentGrouping } from "../hooks/useAgentGroups";
import type { AgentContext } from "../hooks/useAgentContexts";
import { cn } from "../lib/utils";
import { Plus, Layers, Bell, LayoutGrid, ChevronRight, MoreHorizontal, Folders, Activity } from "lucide-react";
import { AgentActionMenu } from "./AgentActionMenu";
import { AgentKindBadge, UnsandboxedBadge } from "./AgentKindBadge";
import { ActivityBar, ContextMeter } from "./AgentMeters";
import { isAgentToolInForeground } from "../lib/agentKind";
import { agentSections, formatAgentTime, sectionLabels, stateSeconds, type AgentListSection } from "../lib/agentList";
import { agentGroupLayout, type AgentGroupBlock } from "../lib/agentGroups";
import type { EnsembleSection } from "../lib/agentGlance";

interface AgentSidebarProps {
  agents: AgentInfo[];
  selectedId: string | null;
  onSelect: (taskId: string) => void;
  onSpawn: () => void;
  onStartSwarm?: () => void;
  // Shows every agent at once, in place of the list and one agent.
  onShowEnsemble?: () => void;
  onAgentContextMenu?: (agent: AgentInfo, position: { x: number; y: number }) => void;
  modifierHeld?: boolean;
  // Artifacts each agent showed while it was not in view, by task ID.
  unseenArtifacts?: Record<string, number>;
  // How full each native agent's context window is, by task ID.
  contexts?: Record<string, AgentContext>;
  // Shown above the agents: those still being started (PreparingAgents).
  preparing?: ReactNode;
  // Pinned first, above the groups: the Reeve (ReeveEntry).
  reeve?: ReactNode;
  // Lists the agents by group (useAgentGroups); without it, by status.
  grouping?: AgentGrouping;
}

// The type an agent being dragged to another group carries.
const AGENT_DRAG_TYPE = "application/x-legato-agent";

const sectionTone: Record<EnsembleSection, string> = {
  "needs-you": "text-lw-needs",
  running: "text-lw-live",
  background: "text-lw-done",
  idle: "text-lw-text-2",
};

export function AgentSidebar({
  agents,
  selectedId,
  onSelect,
  onSpawn,
  onStartSwarm,
  onShowEnsemble,
  onAgentContextMenu,
  modifierHeld,
  unseenArtifacts,
  contexts,
  preparing,
  reeve,
  grouping,
}: AgentSidebarProps) {
  // The Reeve's agent is pinned on its own, above the groups.
  const listed = reeve ? agents.filter((a) => !a.reeve) : agents;
  const grouped = grouping?.mode === "group";
  // The agent being dragged to another group.
  const [dragging, setDragging] = useState<string | null>(null);
  let position = 0;

  const entry = (agent: AgentInfo) => {
    const index = position++;
    return (
      <AgentEntry
        key={agent.task_id}
        agent={agent}
        index={index}
        selected={selectedId === agent.task_id}
        modifierHeld={modifierHeld}
        unseen={unseenArtifacts?.[agent.task_id] ?? 0}
        context={contexts?.[agent.task_id]}
        onSelect={onSelect}
        onAgentContextMenu={onAgentContextMenu}
        onDragStart={grouping ? setDragging : undefined}
        onDragEnd={grouping ? () => setDragging(null) : undefined}
      />
    );
  };

  return (
    <div aria-label="Agent sessions" className="flex w-[264px] max-w-full shrink-0 flex-col overflow-x-hidden overflow-y-auto border-r border-[#222228] bg-[#141417] font-lw text-[13px] leading-5 text-lw-text">
      <div className="flex items-center justify-between px-3.5 pb-2.5 pt-3.5">
        <span className="font-semibold">
          Agents <span className="font-normal text-lw-faint">{agents.length}</span>
        </span>
        <div className="flex items-center gap-1">
          {grouping && (
            <button
              onClick={() => grouping.setMode(grouped ? "status" : "group")}
              aria-label={grouped ? "List by status" : "List by group"}
              className="flex h-7 w-7 items-center justify-center rounded-md text-lw-text-2 hover:bg-lw-raised hover:text-lw-text"
              title={grouped ? "List by status" : "List by group"}
            >
              {grouped ? <Activity size={14} /> : <Folders size={14} />}
            </button>
          )}
          {onShowEnsemble && (
            <button
              onClick={onShowEnsemble}
              aria-label="Ensemble"
              className="flex h-7 w-7 items-center justify-center rounded-md text-lw-text-2 hover:bg-lw-raised hover:text-lw-text"
              title="Ensemble: every agent at once (e)"
            >
              <LayoutGrid size={14} />
            </button>
          )}
          {onStartSwarm && (
            <button
              onClick={onStartSwarm}
              aria-label="Start swarm"
              className="flex h-7 w-7 items-center justify-center rounded-md text-lw-text-2 hover:bg-lw-raised hover:text-lw-text"
              title="Start swarm"
            >
              <Layers size={14} />
            </button>
          )}
          <button
            onClick={onSpawn}
            aria-label="Spawn agent"
            className="flex h-7 w-7 items-center justify-center rounded-md border border-lw-line-strong bg-lw-raised text-lw-text hover:border-[#4a4955]"
            title="Spawn new agent"
          >
            <Plus size={14} />
          </button>
        </div>
      </div>

      <div className="flex flex-col gap-0.5 px-2 pb-3">
        {reeve}
        {preparing}
        {grouped && grouping ? (
          <>
            {agentGroupLayout(listed, grouping.known, { withUngrouped: dragging !== null }).map((block) => (
              <AgentGroupSection
                key={block.name ?? ""}
                block={block}
                grouping={grouping}
                dragging={dragging}
                onDropped={() => setDragging(null)}
                renderEntry={entry}
              />
            ))}
            <button
              type="button"
              onClick={grouping.newGroup}
              className="mt-2 flex items-center gap-1.5 self-start rounded px-2 py-1 text-xs text-lw-text-2 hover:bg-lw-raised hover:text-lw-text"
            >
              <Plus size={12} aria-hidden="true" />New group
            </button>
          </>
        ) : (
          agentSections(listed).map((section) => (
            <StatusSection key={section.section} section={section} level={2} renderEntry={entry} />
          ))
        )}
      </div>
    </div>
  );
}

// StatusSection is the agents doing one thing, under its heading.
function StatusSection({ section: { section, agents }, level, renderEntry }: {
  section: AgentListSection;
  level: 2 | 3;
  renderEntry: (agent: AgentInfo) => ReactNode;
}) {
  const Heading = level === 2 ? "h2" : "h3";
  return (
    <section className="flex flex-col gap-0.5">
      <Heading className={cn("m-0 px-2 pb-0.5 text-[11px] font-semibold uppercase tracking-[1px]", level === 2 ? "pt-2.5" : "pt-1.5 text-[10px]", sectionTone[section])}>
        {sectionLabels[section]} · {agents.length}
      </Heading>
      {agents.map(renderEntry)}
    </section>
  );
}

// AgentGroupSection is one group: a header that collapses it and takes
// agents dropped on it, then its agents under what they are doing.
function AgentGroupSection({ block, grouping, dragging, onDropped, renderEntry }: {
  block: AgentGroupBlock;
  grouping: AgentGrouping;
  dragging: string | null;
  onDropped: () => void;
  renderEntry: (agent: AgentInfo) => ReactNode;
}) {
  const label = block.name ?? "Ungrouped";
  const collapsed = grouping.isCollapsed(block.name);
  const count = block.agents.length;
  const [over, setOver] = useState(false);

  const draggedId = (event: DragEvent) => event.dataTransfer?.getData(AGENT_DRAG_TYPE) || dragging;
  const onDragOver = (event: DragEvent) => {
    if (!dragging && !event.dataTransfer?.types?.includes(AGENT_DRAG_TYPE)) return;
    event.preventDefault();
    if (event.dataTransfer) event.dataTransfer.dropEffect = "move";
    setOver(true);
  };
  const onDrop = (event: DragEvent) => {
    event.preventDefault();
    setOver(false);
    const id = draggedId(event);
    onDropped();
    if (!id || block.agents.some((agent) => agent.task_id === id)) return;
    grouping.move(id, block.name);
  };

  return (
    <section
      aria-label={label}
      data-drop-target={over ? "true" : undefined}
      onDragOver={onDragOver}
      onDragLeave={(event) => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setOver(false); }}
      onDrop={onDrop}
      className={cn("flex flex-col gap-0.5 rounded-md", over && "bg-lw-accent/10 shadow-[inset_0_0_0_1px] shadow-lw-accent-ring")}
    >
      <div className="group/header flex items-center pt-2">
        <h2 className="m-0 min-w-0 flex-1">
          <button
            type="button"
            aria-expanded={!collapsed}
            aria-label={`${label}, ${count}${collapsed && block.needsYou > 0 ? `, ${block.needsYou} need you` : ""}`}
            onClick={() => grouping.toggle(block.name)}
            className="flex w-full min-w-0 items-center gap-1.5 rounded px-1.5 py-1 text-left text-xs font-semibold text-lw-text hover:bg-lw-raised"
          >
            <ChevronRight size={12} aria-hidden="true" className={cn("shrink-0 text-lw-text-2 transition-transform", !collapsed && "rotate-90")} />
            <span className={cn("min-w-0 truncate", !block.name && "text-lw-text-2")}>{label}</span>
            <span className="shrink-0 font-normal text-lw-faint">{count}</span>
            {collapsed && block.needsYou > 0 && (
              <span
                aria-label={`${block.needsYou} need you`}
                title={`${block.needsYou} need you`}
                className="ml-auto flex h-4 min-w-4 shrink-0 items-center justify-center rounded-full bg-lw-needs px-1 text-[10px] font-semibold text-black"
              >
                {block.needsYou}
              </span>
            )}
          </button>
        </h2>
        <GroupMenu name={block.name} label={label} grouping={grouping} />
      </div>
      {!collapsed && block.sections.map((section) => (
        <StatusSection key={section.section} section={section} level={3} renderEntry={renderEntry} />
      ))}
      {!collapsed && count === 0 && dragging && (
        <div className="mx-2 mb-1 rounded border border-dashed border-lw-line-strong px-2 py-2 text-center text-[11px] text-lw-text-2">Drop here</div>
      )}
    </section>
  );
}

// GroupMenu is a group's actions, beside its header.
function GroupMenu({ name, label, grouping }: { name: string | null; label: string; grouping: AgentGrouping }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const close = (event: Event) => {
      if (event instanceof KeyboardEvent ? event.key === "Escape" : !ref.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", close);
    document.addEventListener("keydown", close);
    return () => {
      document.removeEventListener("mousedown", close);
      document.removeEventListener("keydown", close);
    };
  }, [open]);

  const item = (text: string, action: () => void, danger = false) => (
    <button
      type="button"
      role="menuitem"
      onClick={() => { setOpen(false); action(); }}
      className={cn("flex w-full items-center px-3 py-1.5 text-left text-xs hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none", danger ? "text-red-300" : "text-zinc-200")}
    >
      {text}
    </button>
  );

  return (
    <div ref={ref} className="relative shrink-0">
      <button
        type="button"
        aria-label={`${label} group actions`}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
        className={cn("flex h-6 w-6 items-center justify-center rounded text-lw-text-2 hover:bg-lw-raised hover:text-lw-text focus:opacity-100 group-hover/header:opacity-100", open ? "opacity-100" : "opacity-0")}
      >
        <MoreHorizontal size={14} />
      </button>
      {open && (
        <div role="menu" aria-label={`${label} group`} className="absolute right-0 top-full z-20 mt-1 min-w-44 rounded-md border border-zinc-700 bg-zinc-900 py-1 shadow-xl">
          {name !== null && item("Rename group", () => grouping.renameGroup(name))}
          {item("Collapse all others", () => grouping.collapseOthers(name))}
          {name !== null && item("Delete group", () => grouping.deleteGroup(name), true)}
        </div>
      )}
    </div>
  );
}

function AgentEntry({
  agent,
  index,
  selected,
  modifierHeld,
  unseen,
  context,
  onSelect,
  onAgentContextMenu,
  onDragStart,
  onDragEnd,
}: {
  agent: AgentInfo;
  index: number;
  selected: boolean;
  modifierHeld?: boolean;
  unseen: number;
  context?: AgentContext;
  onSelect: (taskId: string) => void;
  onAgentContextMenu?: (agent: AgentInfo, position: { x: number; y: number }) => void;
  // Makes the entry draggable to another group.
  onDragStart?: (taskId: string) => void;
  onDragEnd?: () => void;
}) {
  const isConductor = agent.role === "conductor";
  const isSwarm = Boolean(agent.parent_task_id);
  const isWorker = isSwarm && !isConductor;
  const time = formatAgentTime(stateSeconds(agent));

  return (
    <div
      draggable={onDragStart ? true : undefined}
      onDragStart={onDragStart ? (event) => {
        event.dataTransfer?.setData(AGENT_DRAG_TYPE, agent.task_id);
        if (event.dataTransfer) event.dataTransfer.effectAllowed = "move";
        onDragStart(agent.task_id);
      } : undefined}
      onDragEnd={onDragEnd}
      onContextMenu={onAgentContextMenu ? (event) => {
        event.preventDefault();
        onAgentContextMenu(agent, { x: event.clientX, y: event.clientY });
      } : undefined}
      className={cn(
        "group relative flex min-w-0 max-w-full items-start overflow-hidden rounded-md transition-colors",
        isWorker && "ml-3",
        selected ? "bg-lw-selected shadow-[inset_0_0_0_1px] shadow-lw-accent-ring" : "hover:bg-lw-surface",
      )}
    >
      <button
        data-agent={agent.task_id}
        onClick={() => onSelect(agent.task_id)}
        className={cn("flex min-w-0 max-w-full flex-1 flex-col gap-[3px] overflow-hidden px-2.5 py-2 text-left", selected ? "text-lw-text" : agent.activity ? "text-lw-text" : "text-[#b4b3bd]")}
      >
        <div className="flex min-w-0 items-center gap-1.5">
          {agent.notify_enabled && <Bell size={10} aria-label="Notifications on" className="shrink-0 text-lw-link" />}
          {isConductor && (
            <span title="Swarm conductor" className="shrink-0 rounded bg-lw-accent/20 px-1.5 text-[10px] font-medium uppercase text-lw-link">◆ conductor</span>
          )}
          {isWorker && agent.role && (
            <span title={`Worker role: ${agent.role}`} className="shrink-0 rounded bg-lw-raised px-1.5 text-[10px] font-medium uppercase text-lw-text-2">{agent.role}</span>
          )}
          <span className="min-w-0 truncate font-lw-mono text-xs font-medium">{agent.task_id}</span>
          {unseen > 0 && (
            <span
              aria-label={`${unseen} new artifact${unseen === 1 ? "" : "s"}`}
              title="New artifacts from this agent"
              className="flex h-4 min-w-4 shrink-0 items-center justify-center rounded-full bg-lw-accent px-1 text-[10px] font-semibold text-white"
            >
              {unseen}
            </span>
          )}
          {modifierHeld && index < 9
            ? <span className="ml-auto flex h-5 w-5 shrink-0 items-center justify-center rounded bg-lw-accent text-[10px] font-bold text-white">{index + 1}</span>
            : time && <span className="ml-auto shrink-0 text-xs text-lw-text-2">{time}</span>}
        </div>
        {agent.task_title && <span className="truncate">{agent.task_title}</span>}
        <div className="flex min-w-0 items-center justify-between gap-2 text-[11px] text-lw-text-2">
          <span className="flex min-w-0 items-center gap-1.5">
            <AgentKindBadge kind={agent.agent_kind} isNative={!!agent.has_chimera_endpoint} />
            {!isAgentToolInForeground(agent.agent_kind, agent.command) && (
              <span className="min-w-0 truncate font-lw-mono text-lw-faint">{agent.command}</span>
            )}
          </span>
          {context && <ContextMeter share={context.share} />}
        </div>
        <div className="flex items-center gap-2 pt-0.5">
          <ActivityBar states={agent.state_timeline} />
          {agent.unsandboxed && <UnsandboxedBadge />}
        </div>
      </button>
      {isSwarm && (
        <div className="pr-1 pt-1.5">
          <AgentActionMenu
            agentTaskId={agent.task_id}
            parentTaskId={agent.parent_task_id ?? ""}
            subtaskId={agent.subtask_id ?? agent.task_id}
            role={agent.role ?? "worker"}
          />
        </div>
      )}
    </div>
  );
}
