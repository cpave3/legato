import { cn } from "../../lib/utils"
import { useEffect, useRef, type DragEvent, type MouseEvent, type ReactNode } from "react"
import { Link } from "react-router-dom"
import { MoreHorizontal } from "lucide-react"
import type { BoardCard as BoardCardType } from "../../lib/board-types"
import { agentStripLook, formatDuration, priorityTone } from "../../lib/boardCard"
import { animatedAgentActivityEnabled } from "../../lib/appearancePreferences"

interface BoardCardProps {
  card: BoardCardType
  selected: boolean
  highlighted?: boolean
  column: string
  showWorkspace: boolean
  onClick: () => void
  onDoubleClick?: () => void
  onContextMenu?: (position: { x: number; y: number }) => void
  onActionMenu?: (position: { x: number; y: number }) => void
  onDragStart?: (event: DragEvent<HTMLDivElement>) => void
}

// The line at the foot of a card whose task has an agent: what the agent is
// doing, on the tint of that state, and the time that goes with it.
function AgentStrip({ card }: { card: BoardCardType }) {
  const look = agentStripLook(card)
  return (
    <div data-testid="agent-strip" className={cn("flex items-center justify-between gap-2 px-3 py-[5px] text-xs", look.tint)}>
      <span className={cn("flex min-w-0 items-center gap-1.5", look.tone)}><span aria-hidden="true">{look.glyph}</span><span className="truncate">{look.label}</span></span>
      {look.time > 0 && <span className="shrink-0 font-lw-mono text-lw-text-2">{formatDuration(look.time)}</span>}
    </div>
  )
}

function MetaLine({ card, showWorkspace }: { card: BoardCardType; showWorkspace: boolean }) {
  const swarm = card.swarm_stats && card.swarm_stats.total > 0 ? card.swarm_stats : null
  const parts: ReactNode[] = []
  if (card.priority) parts.push(<span key="priority" className={priorityTone(card.priority)}>{card.priority}</span>)
  if (card.issue_type) parts.push(<span key="type">{card.issue_type}</span>)
  if (showWorkspace && card.workspace_name) parts.push(<span key="workspace">{card.workspace_name}</span>)
  if (swarm) parts.push(<span key="swarm" className="text-lw-link">{swarm.done}/{swarm.total} workers</span>)
  if (parts.length === 0) return null
  return (
    <div data-testid="card-meta" className="flex flex-wrap items-center gap-x-1.5 text-xs text-lw-text-2">
      {parts.map((part, index) => <span key={index} className="contents">{index > 0 && <span aria-hidden="true" className="text-lw-faint">·</span>}{part}</span>)}
    </div>
  )
}

function Mark({ glyph, tone, children }: { glyph: string; tone: string; children: ReactNode }) {
  return <span className={cn("inline-flex items-center gap-1", tone)}><span aria-hidden="true">{glyph}</span><span>{children}</span></span>
}

function checksMark(status: string) {
  switch (status) {
    case "pass": return <Mark glyph="✓" tone="text-lw-live">CI passed</Mark>
    case "fail": return <Mark glyph="✕" tone="text-lw-error">CI failed</Mark>
    case "pending": return <Mark glyph="◌" tone="text-lw-needs">CI pending</Mark>
    default: return <span className="text-lw-faint">no checks</span>
  }
}

function reviewMark(decision: string) {
  switch (decision) {
    case "APPROVED": return <Mark glyph="✓" tone="text-lw-live">approved</Mark>
    case "CHANGES_REQUESTED": return <Mark glyph="✕" tone="text-lw-error">changes requested</Mark>
    default: return <span className="text-lw-faint">review pending</span>
  }
}

function PullRequestLine({ card }: { card: BoardCardType }) {
  const ready = !card.pr_is_draft && card.pr_check_status === "pass" && card.pr_review_decision === "APPROVED"
  return (
    <div aria-label={`Pull request #${card.pr_number}`} className="flex flex-wrap items-center gap-x-2 pt-0.5 text-xs text-lw-text-2">
      <span className="font-lw-mono">#{card.pr_number}</span>
      {card.pr_is_draft && <span>draft</span>}
      {checksMark(card.pr_check_status)}
      {reviewMark(card.pr_review_decision)}
      {card.pr_comment_count > 0 && <span>{card.pr_comment_count} {card.pr_comment_count === 1 ? "comment" : "comments"}</span>}
      {ready && <span className="text-lw-done">ready to merge</span>}
    </div>
  )
}

function ReviewStrip({ card }: { card: BoardCardType }) {
  const count = card.review_unreviewed ?? 0
  return (
    <div data-testid="review-strip" className="flex items-center justify-between gap-2 bg-lw-needs-soft px-3 py-[5px] text-xs">
      <span className="flex min-w-0 items-center gap-1.5 text-lw-needs"><span aria-hidden="true">◆</span><span className="truncate">{count} {count === 1 ? "change" : "changes"} to review</span></span>
      {card.review_tour_id && <Link to={`/review/${encodeURIComponent(card.review_tour_id)}`} onClick={event => event.stopPropagation()} className="shrink-0 text-[#c9b78f] hover:text-lw-needs">Review →</Link>}
    </div>
  )
}

export function BoardCard({ card, selected, highlighted = false, column, showWorkspace, onClick, onDoubleClick, onContextMenu, onActionMenu, onDragStart }: BoardCardProps) {
  const cardRef = useRef<HTMLDivElement>(null)
  const isDone = column === "Done"
  const trace = card.agent_active && animatedAgentActivityEnabled()
  const agentTime = card.agent_active ? "" : formatDuration(card.working_seconds + card.waiting_seconds + card.error_seconds)

  useEffect(() => {
    if (highlighted) cardRef.current?.scrollIntoView?.({ behavior: "smooth", block: "center", inline: "nearest" })
  }, [highlighted])

  return (
    <div
      ref={cardRef}
      data-card={card.id}
      data-highlighted-task={highlighted || undefined}
      data-selected={selected || undefined}
      onClick={onClick}
      onDoubleClick={onDoubleClick}
      onContextMenu={(event: MouseEvent<HTMLDivElement>) => {
        if (!onContextMenu) return
        event.preventDefault()
        onContextMenu({ x: event.clientX, y: event.clientY })
      }}
      draggable
      onDragStart={onDragStart}
      className={cn(
        // A column lays cards out as a flex column that scrolls; a card keeps
        // its height there rather than being squeezed to fit.
        "group relative flex shrink-0 cursor-pointer flex-col overflow-hidden rounded-lg border font-lw text-lw-text transition-colors",
        selected ? "border-lw-accent bg-lw-selected" : "border-lw-line bg-lw-surface hover:border-lw-line-strong",
        trace && (card.agent_state === "working" ? "lw-trace lw-trace-working" : "lw-trace lw-trace-idle"),
        highlighted && "ring-2 ring-cyan-400 shadow-lg shadow-cyan-950/60",
        isDone && !selected && "opacity-60",
      )}
    >
      {onActionMenu && <button type="button" aria-label={`Open actions for ${card.title}`} onClick={event => { event.stopPropagation(); const rect = event.currentTarget.getBoundingClientRect(); onActionMenu({ x: rect.right, y: rect.bottom }) }} className="absolute right-1 top-1 z-10 flex h-9 w-9 items-center justify-center rounded-md text-lw-faint hover:bg-lw-raised hover:text-lw-text focus-visible:opacity-100 md:h-7 md:w-7 md:opacity-0 md:group-hover:opacity-100"><MoreHorizontal size={15} /></button>}
      <div className="flex flex-col gap-1 px-3 py-2.5">
        <div className="flex justify-between gap-2 pr-8 font-lw-mono text-[11px] text-lw-faint md:pr-0">
          <span className="truncate">{card.provider || "local"} · {card.id}</span>
          {card.has_warning && <Mark glyph="⚠" tone="mr-auto shrink-0 font-lw text-lw-error">sync failed</Mark>}
          {agentTime && <span title="Time agents spent on this task" className="shrink-0">{agentTime}</span>}
        </div>
        <div className="text-sm font-semibold leading-[19px]">{card.title}</div>
        <MetaLine card={card} showWorkspace={showWorkspace} />
        {card.review_ready && !card.review_unreviewed && <Mark glyph="✓" tone="text-xs text-lw-done">Reviewed</Mark>}
        {card.pr_number > 0 && <PullRequestLine card={card} />}
      </div>
      {card.agent_active && <AgentStrip card={card} />}
      {Boolean(card.review_unreviewed) && <ReviewStrip card={card} />}
    </div>
  )
}
