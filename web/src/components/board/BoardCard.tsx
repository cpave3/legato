import { cn } from "../../lib/utils"
import type { DragEvent, MouseEvent } from "react"
import type { BoardCard as BoardCardType } from "../../lib/board-types"
import { animatedAgentActivityEnabled } from "../../lib/appearancePreferences"
import {
  Globe,
  FileText,
  AlertTriangle,
  CheckCircle2,
  Code2,
  XCircle,
  Clock,
  Check,
  FileWarning,
  MessageSquare,
  Diamond,
  Play,
  Pause,
  ListChecks,
  MoreHorizontal,
  GitPullRequest,
} from "lucide-react"

interface BoardCardProps {
  card: BoardCardType
  selected: boolean
  column: string
  showWorkspace: boolean
  onClick: () => void
  onContextMenu?: (position: { x: number; y: number }) => void
  onActionMenu?: (position: { x: number; y: number }) => void
  onDragStart?: (event: DragEvent<HTMLDivElement>) => void
}

function formatDuration(seconds: number): string {
  if (seconds <= 0) return ""
  const minutes = Math.floor(seconds / 60)
  if (minutes < 1) return "<1m"
  const hours = Math.floor(minutes / 60)
  const remaining = minutes % 60
  if (hours === 0) return `${remaining}m`
  if (remaining === 0) return `${hours}h`
  return `${hours}h ${remaining}m`
}

function priorityBorderClass(priority: string): string {
  switch (priority) {
    case "High":
    case "Highest":
      return "border-l-[#E06C75]"
    case "Medium":
      return "border-l-[#E5C07B]"
    case "Low":
    case "Lowest":
      return "border-l-[#5DCAA5]"
    default:
      return "border-l-transparent"
  }
}

function priorityTextClass(priority: string): string {
  switch (priority) {
    case "High":
    case "Highest":
      return "text-[#E06C75]"
    case "Medium":
      return "text-[#E5C07B]"
    case "Low":
    case "Lowest":
      return "text-[#5DCAA5]"
    default:
      return "text-zinc-500"
  }
}

function providerIcon(provider: string) {
  switch (provider) {
    case "jira":
      return <Globe size={12} className="text-[#85B7EB]" />
    case "github":
      return <Code2 size={12} className="text-zinc-300" />
    default:
      return <FileText size={12} className="text-zinc-600" />
  }
}

export function BoardCard({ card, selected, column, showWorkspace, onClick, onContextMenu, onActionMenu, onDragStart }: BoardCardProps) {
  const isDone = column === "Done"
  const workingDur = formatDuration(card.working_seconds || 0)
  const waitingDur = formatDuration(card.waiting_seconds || 0)
  const errorDur = formatDuration(card.error_seconds || 0)
  const hasSwarm = card.swarm_stats && card.swarm_stats.total > 0
  const animateAgentActivity = card.agent_active && animatedAgentActivityEnabled()

  return (
    <div
      onClick={onClick}
      onContextMenu={(event: MouseEvent<HTMLDivElement>) => {
        if (!onContextMenu) return
        event.preventDefault()
        onContextMenu({ x: event.clientX, y: event.clientY })
      }}
      draggable
      onDragStart={onDragStart}
      className={cn(
        "relative cursor-pointer rounded border-y border-r border-l-2 border-y-zinc-800/80 border-r-zinc-800/80 px-2.5 py-2 text-sm text-zinc-300 shadow-sm shadow-black/20 transition-[background-color,border-color,box-shadow]",
        priorityBorderClass(card.priority),
        card.agent_active && "ring-1 ring-inset ring-indigo-500/60",
        animateAgentActivity && "agent-activity-orbit",
        animateAgentActivity && !card.agent_state && "agent-activity-orbit-idle",
        animateAgentActivity && card.agent_state === "working" && "agent-activity-orbit-working",
        selected
          ? "bg-[linear-gradient(135deg,#292342_0%,#211d35_52%,#171725_100%)] border-y-violet-400/45 border-r-violet-400/45 shadow-md shadow-violet-950/40"
          : "bg-[#1E1E2E] hover:border-y-zinc-700 hover:border-r-zinc-700 hover:bg-zinc-900 hover:shadow-md hover:shadow-black/30",
        isDone && !selected && "opacity-60"
      )}
    >
      {onActionMenu && <button type="button" aria-label={`Open actions for ${card.title}`} onClick={event => { event.stopPropagation(); const rect = event.currentTarget.getBoundingClientRect(); onActionMenu({ x: rect.right, y: rect.bottom }) }} className="absolute right-1 top-1 z-10 flex h-7 w-7 items-center justify-center rounded text-zinc-500 hover:bg-zinc-800 hover:text-zinc-200"><MoreHorizontal size={15} /></button>}
      {/* Key line */}
      <div className="flex items-center gap-1.5">
        {providerIcon(card.provider)}
        {card.agent_state === "working" && (
          <span className="flex items-center gap-0.5 text-[10px] font-bold text-[#1D9E75]">
            <Play size={8} fill="currentColor" /> RUNNING
          </span>
        )}
        {card.agent_state === "waiting" && (
          <span className="flex items-center gap-0.5 text-[10px] font-bold text-[#E5C07B]">
            <Pause size={8} /> WAITING
          </span>
        )}
        {card.agent_state === "error" && (
          <span className="flex items-center gap-0.5 text-[10px] font-bold text-red-400">
            <AlertTriangle size={8} /> ERROR
          </span>
        )}
        {card.agent_state === "mixed" && (
          <span className="flex items-center gap-0.5 text-[10px] font-bold text-violet-400">
            <ListChecks size={8} /> MIXED
          </span>
        )}
        {card.agent_active && !card.agent_state && (
          <span className="flex items-center gap-0.5 text-[10px] text-zinc-500">
            IDLE
          </span>
        )}
        {card.has_warning && <AlertTriangle size={10} className="text-red-400" />}
        <span className="text-[10px] font-mono text-zinc-500">
          {card.id}
        </span>
      </div>

      {/* Title */}
      <div className={cn("mt-1 font-semibold leading-snug", selected && "text-zinc-100", isDone && !selected && "line-through text-zinc-500")}>
        {card.title}
      </div>

      {/* Meta line */}
      <div className="mt-1 flex flex-wrap items-center gap-1.5 text-[10px]">
        {card.priority && (
          <span className={cn("font-medium", priorityTextClass(card.priority))}>
            {card.priority}
          </span>
        )}
        {card.priority && card.issue_type && <span className="text-zinc-600">·</span>}
        {card.issue_type && <span className="text-zinc-500">{card.issue_type}</span>}
        {showWorkspace && card.workspace_name && (
          <>
            <span className="text-zinc-600">·</span>
            <span className="flex items-center gap-1">
              <span
                className="inline-block h-1.5 w-1.5 rounded-full"
                style={{ backgroundColor: card.workspace_color || "#71717a" }}
              />
              <span className="text-zinc-500">{card.workspace_name}</span>
            </span>
          </>
        )}
        {hasSwarm && (
          <>
            <span className="text-zinc-600">·</span>
            <span className="font-bold text-[#AFA9EC]">
              {card.swarm_stats?.done}/{card.swarm_stats?.total} <Diamond size={8} className="inline" />
            </span>
          </>
        )}
      </div>

      {/* Review line */}
      {(card.review_ready || Boolean(card.review_unreviewed)) && (
        <div className={cn("mt-1 flex items-center gap-1 text-[10px] font-medium", card.review_unreviewed ? "text-amber-300" : "text-emerald-400")}>
          <ListChecks size={10} />
          {card.review_unreviewed ? `Review ${card.review_unreviewed}` : "Reviewed"}
        </div>
      )}

      {/* GitHub line */}
      {card.pr_number > 0 && (
        <div aria-label={`GitHub PR #${card.pr_number}`} className="mt-1 flex flex-wrap items-center gap-1.5 text-[10px]">
          <span className="flex items-center gap-0.5 font-medium text-zinc-400">
            <GitPullRequest size={10} /> #{card.pr_number}
          </span>
          {card.pr_is_draft && <span className="rounded bg-zinc-800 px-1 py-0.5 text-zinc-500">Draft</span>}
          {card.pr_check_status === "pass" ? (
            <span className="flex items-center gap-0.5 text-[#1D9E75]"><CheckCircle2 size={9} /> CI passed</span>
          ) : card.pr_check_status === "fail" ? (
            <span className="flex items-center gap-0.5 text-red-400"><XCircle size={9} /> CI failed</span>
          ) : card.pr_check_status === "pending" ? (
            <span className="flex items-center gap-0.5 text-[#E5C07B]"><Clock size={9} /> CI pending</span>
          ) : (
            <span className="flex items-center gap-0.5 text-zinc-600"><Clock size={9} /> No checks</span>
          )}
          {card.pr_review_decision === "APPROVED" ? (
            <span className="flex items-center gap-0.5 text-[#1D9E75]"><Check size={9} /> Approved</span>
          ) : card.pr_review_decision === "CHANGES_REQUESTED" ? (
            <span className="flex items-center gap-0.5 text-red-400"><FileWarning size={9} /> Changes requested</span>
          ) : (
            <span className="flex items-center gap-0.5 text-zinc-500"><Clock size={9} /> Pending review</span>
          )}
          {card.pr_comment_count > 0 && (
            <span className="flex items-center gap-0.5 text-zinc-500">
              <MessageSquare size={9} /> {card.pr_comment_count}
            </span>
          )}
        </div>
      )}

      {/* Duration line */}
      {(workingDur || waitingDur || errorDur) && (
        <div className="mt-1 flex items-center gap-2 text-[10px]">
          {workingDur && (
            <span className="flex items-center gap-0.5 text-[#1D9E75]">
              <Play size={8} fill="currentColor" /> <span className="text-zinc-500">{workingDur}</span>
            </span>
          )}
          {waitingDur && (
            <span className="flex items-center gap-0.5 text-[#E5C07B]">
              <Pause size={8} /> <span className="text-zinc-500">{waitingDur}</span>
            </span>
          )}
          {errorDur && (
            <span className="flex items-center gap-0.5 text-red-400">
              <AlertTriangle size={8} /> <span className="text-zinc-500">{errorDur}</span>
            </span>
          )}
        </div>
      )}
    </div>
  )
}
