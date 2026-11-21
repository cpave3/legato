import { cn } from "../../lib/utils"
import { useEffect, useRef, useState, type DragEvent, type MouseEvent, type ReactNode } from "react"
import { Link } from "react-router-dom"
import { MoreHorizontal } from "lucide-react"
import type { BoardCard as BoardCardType } from "../../lib/board-types"
import { agentStripLook, formatDuration, needsYouStrip, needsYouStrips, priorityTone, slotChipLabel, type NeedsYouStrip, type StripAction } from "../../lib/boardCard"
import { openNeedsYou, showApproval } from "../../lib/railState"
import { resumeInterruptedAgent } from "../../lib/interruptedAgents"
import { useServer } from "../../hooks/useServer"
import { useToast } from "../../hooks/useToast"
import { animatedAgentActivityEnabled } from "../../lib/appearancePreferences"
import { Mark } from "../PullRequestMarks"
import { CardPullRequests } from "./PullRequests"

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

const stripRow = "flex items-center justify-between gap-2 border-t border-lw-line px-3 py-[5px] text-xs"
const stripAction = "shrink-0 font-medium text-lw-text underline underline-offset-2 hover:text-lw-accent-text"

// The line at the foot of a card whose task has an agent: what the agent is
// doing, on the tint of that state, and the time that goes with it.
function AgentStrip({ card }: { card: BoardCardType }) {
  const look = agentStripLook(card)
  return (
    <div data-testid="agent-strip" className={cn(stripRow, look.tint)}>
      <span className={cn("flex min-w-0 items-center gap-1.5", look.tone)}><span aria-hidden="true">{look.glyph}</span><span className="truncate">{look.label}</span>
        {card.slot_agents?.filter(slot => !card.needs_you?.slots?.some(waiting => waiting.slot === slot.slot)).map((slot) => <span key={slot.slot} className="shrink-0 rounded border border-lw-line px-1 text-lw-text-2">{slotChipLabel(slot.slot, slot.state)}</span>)}
      </span>
      {look.time > 0 && <span className="shrink-0 font-lw-mono text-lw-text-2">{formatDuration(look.time)}</span>}
    </div>
  )
}

// Whether the page is as narrow as a phone, where the needs-you rail is a
// sheet.
function phoneWidth(): boolean {
  return typeof window.matchMedia === "function" && window.matchMedia("(max-width: 767px)").matches
}

// The way to what a strip says waits: the rail on the agent's items, the
// approval, the plan.
function StripActionLink({ action }: { action: StripAction }) {
  const stop = (event: MouseEvent) => event.stopPropagation()
  switch (action.kind) {
    case "approval":
      return <button type="button" onClick={event => { event.stopPropagation(); showApproval(action.approvalId) }} onDoubleClick={stop} className={cn(stripAction, "min-h-11 md:min-h-0")}>{action.label}</button>
    case "items":
      // From md up the rail beside the agent lists its items; on a phone,
      // where going to the agent would close it, the sheet opens here.
      return <Link to={`/agents?task=${encodeURIComponent(action.taskId)}`} onClick={event => {
        event.stopPropagation()
        if (phoneWidth()) event.preventDefault()
        openNeedsYou()
      }} onDoubleClick={stop} className={cn(stripAction, "inline-flex min-h-11 items-center md:min-h-0")}>{action.label}</Link>
    case "plan":
      return <Link to={`/plans/${encodeURIComponent(action.planId)}`} onClick={stop} onDoubleClick={stop} className={stripAction}>{action.label}</Link>
    case "resume":
      return <ResumeButton taskId={action.taskId} label={action.label} />
  }
}

// Resumes an interrupted agent as Reload does, where its conversation was.
function ResumeButton({ taskId, label }: { taskId: string; label: string }) {
  const baseUrl = useServer()?.baseUrl ?? ""
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  return <button type="button" disabled={busy} onClick={event => {
    event.stopPropagation()
    setBusy(true)
    resumeInterruptedAgent(baseUrl, taskId).catch((error: unknown) => toast?.addToast(error instanceof Error ? error.message : String(error), "error")).finally(() => setBusy(false))
  }} onDoubleClick={event => event.stopPropagation()} className={cn(stripAction, "min-h-11 disabled:opacity-50 md:min-h-0")}>{busy ? "Resuming…" : label}</button>
}

// The strip that says why a card needs the operator, with the way there.
function NeedsYouStripRow({ strip }: { strip: NeedsYouStrip }) {
  return (
    <div data-testid="needs-you-strip" className={cn(stripRow, "bg-lw-needs-soft")}>
      <span className="flex min-w-0 items-center gap-1.5 text-lw-needs"><span aria-hidden="true">{strip.glyph}</span><span className="truncate">{strip.text}</span></span>
      <StripActionLink action={strip.action} />
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

function ReviewStrip({ card }: { card: BoardCardType }) {
  const count = card.review_unreviewed ?? 0
  return (
    <div data-testid="review-strip" className={cn(stripRow, "bg-lw-needs-soft")}>
      <span className="flex min-w-0 items-center gap-1.5 text-lw-needs"><span aria-hidden="true">◆</span><span className="truncate">{count} {count === 1 ? "change" : "changes"} to review</span></span>
      {card.review_tour_id && <Link to={`/review/${encodeURIComponent(card.review_tour_id)}`} onClick={event => event.stopPropagation()} className={stripAction}>Review</Link>}
    </div>
  )
}

export function BoardCard({ card, selected, highlighted = false, column, showWorkspace, onClick, onDoubleClick, onContextMenu, onActionMenu, onDragStart }: BoardCardProps) {
  const cardRef = useRef<HTMLDivElement>(null)
  const isDone = column === "Done"
  // The moving light is for an agent at work; a card that needs the
  // operator has a steady amber edge, one in error a red one.
  const trace = card.agent_active && card.agent_state === "working" && animatedAgentActivityEnabled()
  const agentTime = card.agent_active ? "" : formatDuration(card.working_seconds + card.waiting_seconds + card.error_seconds)
  // What waits on the operator is drawn above what runs; it stands in for
  // the agent's own strip while the agent only waits or idles.
  const needs = needsYouStrips(card)
  const main = needsYouStrip(card)
  const quietAgent = ["waiting", "idle", ""].includes(card.agent_state)
  const edge = card.agent_active && card.agent_state === "error" ? "error" : needs.length > 0 || (card.agent_active && card.agent_state === "waiting") ? "needs" : ""

  useEffect(() => {
    if (highlighted) cardRef.current?.scrollIntoView?.({ behavior: "smooth", block: "center", inline: "nearest" })
  }, [highlighted])

  return (
    <div
      ref={cardRef}
      data-card={card.id}
      data-highlighted-task={highlighted || undefined}
      data-selected={selected || undefined}
      data-edge={edge || undefined}
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
        selected ? "border-lw-accent bg-lw-selected" : cn("bg-lw-surface", edge === "needs" ? "border-lw-needs" : edge === "error" ? "border-lw-error" : "border-lw-line hover:border-lw-line-strong"),
        trace && "lw-trace lw-trace-working",
        highlighted && "lw-highlight",
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
          className={cn("rounded-sm text-left text-sm font-semibold leading-[19px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-lw-accent", isDone && "text-lw-text-2")}
        >{card.title}</button>
        {card.handoff_from && <div data-testid="handoff-from" className="truncate text-xs text-lw-text-2">{`from ${card.handoff_from.main} · ${card.handoff_from.main_task_id}`}</div>}
        <MetaLine card={card} showWorkspace={showWorkspace} />
        {card.review_ready && !card.review_unreviewed && <Mark glyph="✓" tone="text-xs text-lw-done">Reviewed</Mark>}
        <CardPullRequests card={card} />
      </div>
      {needs.map(strip => <NeedsYouStripRow key={strip.text} strip={strip} />)}
      {Boolean(card.review_unreviewed) && <ReviewStrip card={card} />}
      {card.agent_active && !(main && quietAgent) && <AgentStrip card={card} />}
    </div>
  )
}
