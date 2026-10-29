import type { AgentInfo } from "../hooks/useWebSocket";
import type { AgentContext } from "../hooks/useAgentContexts";
import { cn } from "../lib/utils";
import { Plus, Layers, Bell, LayoutGrid } from "lucide-react";
import { AgentActionMenu } from "./AgentActionMenu";
import { AgentKindBadge, UnsandboxedBadge } from "./AgentKindBadge";
import { ActivityBar, ContextMeter } from "./AgentMeters";
import { isAgentToolInForeground } from "../lib/agentKind";
import { agentSections, formatAgentTime, sectionLabels, stateSeconds } from "../lib/agentList";
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
}

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
}: AgentSidebarProps) {
  const sections = agentSections(agents);
  let position = 0;

  return (
    <div aria-label="Agent sessions" className="flex w-[264px] max-w-full shrink-0 flex-col overflow-x-hidden overflow-y-auto border-r border-[#222228] bg-[#141417] font-lw text-[13px] leading-5 text-lw-text">
      <div className="flex items-center justify-between px-3.5 pb-2.5 pt-3.5">
        <span className="font-semibold">
          Agents <span className="font-normal text-lw-faint">{agents.length}</span>
        </span>
        <div className="flex items-center gap-1">
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
        {sections.map(({ section, agents: members }) => (
          <section key={section} className="flex flex-col gap-0.5">
            <h2 className={cn("m-0 px-2 pb-0.5 pt-2.5 text-[11px] font-semibold uppercase tracking-[1px]", sectionTone[section])}>
              {sectionLabels[section]} · {members.length}
            </h2>
            {members.map((agent) => {
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
                />
              );
            })}
          </section>
        ))}
      </div>
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
}: {
  agent: AgentInfo;
  index: number;
  selected: boolean;
  modifierHeld?: boolean;
  unseen: number;
  context?: AgentContext;
  onSelect: (taskId: string) => void;
  onAgentContextMenu?: (agent: AgentInfo, position: { x: number; y: number }) => void;
}) {
  const isConductor = agent.role === "conductor";
  const isSwarm = Boolean(agent.parent_task_id);
  const isWorker = isSwarm && !isConductor;
  const time = formatAgentTime(stateSeconds(agent));

  return (
    <div
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
