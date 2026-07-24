import { cn } from "../../lib/utils"
import type { DragEvent } from "react"
import type { BoardCard as BoardCardType } from "../../lib/board-types"
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
} from "lucide-react"

interface BoardCardProps {
  card: BoardCardType
  selected: boolean
  column: string
  showWorkspace: boolean
  onClick: () => void
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

function providerIcon(provider: string, selected: boolean) {
  const dimColor = selected ? "text-zinc-500" : "text-zinc-600"
  switch (provider) {
    case "jira":
      return <Globe size={12} className={cn("text-[#85B7EB]", selected && "text-[#285878]")} />
    case "github":
      return <Code2 size={12} className={cn("text-zinc-300", selected && "text-[#1E1E2E]")} />
    default:
      return <FileText size={12} className={dimColor} />
  }
}

export function BoardCard({ card, selected, column, showWorkspace, onClick, onDragStart }: BoardCardProps) {
  const isDone = column === "Done"
  const workingDur = formatDuration(card.working_seconds || 0)
  const waitingDur = formatDuration(card.waiting_seconds || 0)
  const hasSwarm = card.swarm_stats && card.swarm_stats.total > 0

  return (
    <div
      onClick={onClick}
      draggable
      onDragStart={onDragStart}
      className={cn(
        "relative cursor-pointer rounded border-l-2 px-2.5 py-2 text-sm transition-colors",
        priorityBorderClass(card.priority),
        selected
          ? "bg-[#EEEDFE] text-[#1E1E2E]"
          : "bg-[#1E1E2E] text-zinc-300 hover:bg-zinc-900",
        isDone && !selected && "opacity-60"
      )}
    >
      {/* Key line */}
      <div className="flex items-center gap-1.5">
        {providerIcon(card.provider, selected)}
        {card.agent_state === "working" && (
          <span className={cn("flex items-center gap-0.5 text-[10px] font-bold", selected ? "text-[#287828]" : "text-[#1D9E75]")}>
            <Play size={8} fill="currentColor" /> RUNNING
          </span>
        )}
        {card.agent_state === "waiting" && (
          <span className={cn("flex items-center gap-0.5 text-[10px] font-bold", selected ? "text-[#285878]" : "text-[#E5C07B]")}>
            <Pause size={8} /> WAITING
          </span>
        )}
        {card.agent_active && !card.agent_state && (
          <span className={cn("flex items-center gap-0.5 text-[10px]", selected ? "text-zinc-500" : "text-zinc-500")}>
            IDLE
          </span>
        )}
        {card.has_warning && (
          <AlertTriangle size={10} className={cn(selected ? "text-red-700" : "text-red-400")} />
        )}
        <span className={cn("text-[10px] font-mono", selected ? "text-zinc-500" : "text-zinc-600")}>
          {card.id}
        </span>
      </div>

      {/* Title */}
      <div className={cn("mt-1 font-semibold leading-snug", isDone && !selected && "line-through text-zinc-500", selected && "text-[#1E1E2E]")}>
        {card.title}
      </div>

      {/* Meta line */}
      <div className="mt-1 flex flex-wrap items-center gap-1.5 text-[10px]">
        {card.priority && (
          <span className={cn("font-medium", priorityTextClass(card.priority))}>
            {card.priority}
          </span>
        )}
        {card.priority && card.issue_type && <span className={cn(selected ? "text-zinc-400" : "text-zinc-600")}>·</span>}
        {card.issue_type && (
          <span className={cn(selected ? "text-zinc-500" : "text-zinc-500")}>{card.issue_type}</span>
        )}
        {showWorkspace && card.workspace_name && (
          <>
            <span className={cn(selected ? "text-zinc-400" : "text-zinc-600")}>·</span>
            <span className="flex items-center gap-1">
              <span
                className="inline-block h-1.5 w-1.5 rounded-full"
                style={{ backgroundColor: card.workspace_color || "#71717a" }}
              />
              <span className={cn(selected ? "text-zinc-500" : "text-zinc-500")}>{card.workspace_name}</span>
            </span>
          </>
        )}
        {hasSwarm && (
          <>
            <span className={cn(selected ? "text-zinc-400" : "text-zinc-600")}>·</span>
            <span className={cn("font-bold", selected ? "text-[#5b3fa3]" : "text-[#AFA9EC]")}>
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

      {/* PR line */}
      {card.pr_number > 0 && (
        <div className="mt-1 flex flex-wrap items-center gap-1.5 text-[10px]">
          {card.pr_is_draft ? (
            <span className={cn("rounded bg-zinc-800 px-1 py-0.5", selected ? "text-zinc-500" : "text-zinc-500")}>
              Draft
            </span>
          ) : (
            <>
              {card.pr_check_status === "pass" && (
                <span className="flex items-center gap-0.5 text-[#1D9E75]">
                  <CheckCircle2 size={9} /> CI
                </span>
              )}
              {card.pr_check_status === "fail" && (
                <span className="flex items-center gap-0.5 text-red-400">
                  <XCircle size={9} /> CI
                </span>
              )}
              {card.pr_check_status === "pending" && (
                <span className="flex items-center gap-0.5 text-[#E5C07B]">
                  <Clock size={9} /> CI
                </span>
              )}
              {card.pr_review_decision === "APPROVED" && (
                <span className="flex items-center gap-0.5 text-[#1D9E75]">
                  <Check size={9} /> OK
                </span>
              )}
              {card.pr_review_decision === "CHANGES_REQUESTED" && (
                <span className="flex items-center gap-0.5 text-red-400">
                  <FileWarning size={9} /> Changes
                </span>
              )}
              {card.pr_comment_count > 0 && (
                <span className={cn("flex items-center gap-0.5", selected ? "text-zinc-500" : "text-zinc-600")}>
                  <MessageSquare size={9} /> {card.pr_comment_count}
                </span>
              )}
            </>
          )}
        </div>
      )}

      {/* Duration line */}
      {(workingDur || waitingDur) && (
        <div className="mt-1 flex items-center gap-2 text-[10px]">
          {workingDur && (
            <span className={cn("flex items-center gap-0.5", selected ? "text-[#287828]" : "text-[#1D9E75]")}>
              <Play size={8} fill="currentColor" /> <span className={cn(selected ? "text-zinc-500" : "text-zinc-600")}>{workingDur}</span>
            </span>
          )}
          {waitingDur && (
            <span className={cn("flex items-center gap-0.5", selected ? "text-[#285878]" : "text-[#E5C07B]")}>
              <Pause size={8} /> <span className={cn(selected ? "text-zinc-500" : "text-zinc-600")}>{waitingDur}</span>
            </span>
          )}
        </div>
      )}
    </div>
  )
}
