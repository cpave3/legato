import { cn } from "../../lib/utils"
import { useEffect, useRef, type DragEvent, type MouseEvent, type ReactNode } from "react"
import { Link } from "react-router-dom"
import { MoreHorizontal } from "lucide-react"
import type { BoardCard as BoardCardType, PRMetaView } from "../../lib/board-types"
import { agentStripLook, formatDuration, priorityTone, slotChipLabel } from "../../lib/boardCard"
import { animatedAgentActivityEnabled } from "../../lib/appearancePreferences"
import { Mark } from "../PullRequestMarks"
import { PRActionButton } from "./PRMacros"
import { prStatus, prToneClass } from "../../lib/prStatus"

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
      <span className={cn("flex min-w-0 items-center gap-1.5", look.tone)}><span aria-hidden="true">{look.glyph}</span><span className="truncate">{look.label}</span>
        {card.slot_agents?.map((slot) => <span key={slot.slot} className="shrink-0 rounded border border-lw-line px-1 text-lw-text-2">{slotChipLabel(slot.slot, slot.state)}</span>)}
      </span>
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

// A pull request's number, linked to GitHub, struck through when closed;
// someone else's names its repo.
export function PRNumber({ pr }: { pr: PRMetaView }) {
  const label = `${pr.pinned ? pr.repo : ""}#${pr.pr_number}`
  const closed = pr.state.toUpperCase() === "CLOSED"
  const tone = cn("font-lw-mono", closed ? "text-lw-text-2 line-through" : "text-lw-link")
  if (!pr.pr_url) return <span className={tone}>{label}</span>
  return <a href={pr.pr_url} target="_blank" rel="noreferrer" onClick={event => event.stopPropagation()} onDoubleClick={event => event.stopPropagation()} className={cn(tone, "hover:underline")}>{label}</a>
}

// One pull request in one line: its number, the word for its state and a
// detail. One that needs the operator is a box on the wash of its state,
// with its one action.
export function PullRequestRow({ card, pr, heldBelow = false }: { card: BoardCardType; pr: PRMetaView; heldBelow?: boolean }) {
  const status = prStatus(pr, heldBelow)
  if (!status.needsYou || !status.action) {
    return (
      <div aria-label={`Pull request #${pr.pr_number}`} className="flex flex-wrap items-center gap-x-1.5 pt-0.5 text-xs leading-4 text-lw-text-2">
        <PRNumber pr={pr} />
        <span className={prToneClass(status.tone)}>{status.word}</span>
        {status.detail.map(text => <span key={text} className="contents"><span aria-hidden="true">·</span><span>{text}</span></span>)}
      </div>
    )
  }
  const error = status.tone === "error"
  // Someone else's PR says its checks under its review; the operator's own
  // says nothing more there.
  const below = status.action === "others_pr" ? status.detail.filter(text => !text.includes("comment")).join(" · ") : ""
  const above = status.action === "others_pr" ? status.detail.filter(text => text.includes("comment")) : status.detail
  return (
    <div aria-label={`Pull request #${pr.pr_number}`} data-needs-you={status.tone} className={cn("mt-1 flex flex-col gap-1 rounded-md border px-2 py-[7px] text-xs leading-4", error ? "border-lw-error/25 bg-lw-error-soft" : "border-lw-needs/25 bg-lw-needs-soft")}>
      <div className={cn("flex flex-wrap items-center gap-x-1.5", prToneClass(status.tone))}>
        <span aria-hidden="true">{error ? "✕" : "◆"}</span>
        <PRNumber pr={pr} />
        <span>{[status.word, ...above].join(" · ")}</span>
      </div>
      <div className="flex items-center justify-between gap-2 text-lw-text-2">
        <span>{below}</span>
        <PRActionButton card={card} pr={pr} action={status.action} />
      </div>
    </div>
  )
}

function ReviewStrip({ card }: { card: BoardCardType }) {
  const count = card.review_unreviewed ?? 0
  return (
    <div data-testid="review-strip" className="flex items-center justify-between gap-2 bg-lw-needs-soft px-3 py-[5px] text-xs">
      <span className="flex min-w-0 items-center gap-1.5 text-lw-needs"><span aria-hidden="true">◆</span><span className="truncate">{count} {count === 1 ? "change" : "changes"} to review</span></span>
      {card.review_tour_id && <Link to={`/review/${encodeURIComponent(card.review_tour_id)}`} onClick={event => event.stopPropagation()} className="shrink-0 text-[var(--theme-warning,#c9b78f)] hover:text-lw-needs">Review →</Link>}
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
        <button
          type="button"
          aria-pressed={selected}
          onKeyDown={(event) => {
            if (event.key !== "Enter") return
            event.preventDefault()
            event.stopPropagation()
            ;(onDoubleClick ?? onClick)()
          }}
          className="rounded-sm text-left text-sm font-semibold leading-[19px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-lw-accent"
        >{card.title}</button>
        {card.handoff_from && <div data-testid="handoff-from" className="truncate text-xs text-lw-text-2">{`from ${card.handoff_from.main} · ${card.handoff_from.main_task_id}`}</div>}
        <MetaLine card={card} showWorkspace={showWorkspace} />
        {card.review_ready && !card.review_unreviewed && <Mark glyph="✓" tone="text-xs text-lw-done">Reviewed</Mark>}
        {(card.pull_requests ?? []).map(pr => <PullRequestRow key={`${pr.repo}#${pr.pr_number}`} card={card} pr={pr} />)}
      </div>
      {card.agent_active && <AgentStrip card={card} />}
      {Boolean(card.review_unreviewed) && <ReviewStrip card={card} />}
    </div>
  )
}
