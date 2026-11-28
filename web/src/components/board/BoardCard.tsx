import { cn } from "../../lib/utils"
import { useEffect, useRef, useState, type DragEvent, type MouseEvent, type ReactNode, type RefObject } from "react"
import { Link } from "react-router-dom"
import { MoreHorizontal } from "lucide-react"
import type { BoardCard as BoardCardType } from "../../lib/board-types"
import { agentStripLook, cardSource, formatDuration, isMilestone, slotChipLabel, type NeedsYouStrip, type StripAction } from "../../lib/boardCard"
import { cardFrame, cardMetaParts, type CardFrame } from "../../lib/boardCardFrame"
import { openNeedsYou, showApproval } from "../../lib/railState"
import type { AwayStrip as AwayStripLook } from "../../lib/away"
import { resumeInterruptedAgent } from "../../lib/interruptedAgents"
import { useServer } from "../../hooks/useServer"
import { useToast } from "../../hooks/useToast"
import { animatedAgentActivityEnabled } from "../../lib/appearancePreferences"
import { Mark } from "../PullRequestMarks"
import { CardPullRequests } from "./PullRequests"
import { StageTrack } from "../StageTrack"
import { stageLabel } from "../../lib/agentStage"
import { useReeveName } from "../../lib/reeveName"

// CardStage is an agent's stage on a card's strip: the track and the word,
// or for a finished agent "✓ Finished", which says the same.
function CardStage({ stage }: { stage?: string }) {
  const label = stageLabel(stage)
  if (!label) return null
  if (stage === "finished") {
    return (
      <span className="flex shrink-0 items-center gap-1 text-lw-done">
        <span aria-hidden="true" className="text-lw-faint">·</span>
        <span aria-hidden="true">✓</span><span>{label}</span>
      </span>
    )
  }
  return (
    <span className="flex shrink-0 items-center gap-1 text-lw-text-2">
      <span aria-hidden="true" className="text-lw-faint">·</span>
      <StageTrack stage={stage} size="xs" label="" />
      <span>{label}</span>
    </span>
  )
}

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
  // An away task's actions under its strip (Return, Take back, a stuck
  // review's answers).
  actions?: ReactNode
}

const stripRow = "flex items-center justify-between gap-2 border-t border-lw-line px-3 py-[5px] text-xs"
const stripAction = "shrink-0 font-medium text-lw-text underline underline-offset-2 hover:text-lw-accent-text"

// The line at the foot of a card whose task has an agent: what the agent is
// doing, on the tint of that state, and the time that goes with it.
function AgentStrip({ card }: { card: BoardCardType }) {
  const look = agentStripLook(card, useReeveName())
  return (
    <div data-testid="agent-strip" title={look.tool || undefined} className={cn(stripRow, look.tint)}>
      <span className={cn("flex min-w-0 items-center gap-1.5", look.tone)}><span aria-hidden="true">{look.glyph}</span><span className="shrink-0">{look.label}</span>
        {look.stage && <CardStage stage={card.stage} />}
        {look.reason && <span className="min-w-0 truncate text-lw-text-2">· {look.reason}</span>}
        {card.slot_agents?.filter(slot => !card.needs_you?.slots?.some(waiting => waiting.slot === slot.slot)).map((slot) => <span key={slot.slot} className="shrink-0 rounded border border-lw-line px-1 text-lw-text-2">{slotChipLabel(slot.slot, slot.state, slot.stage)}</span>)}
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

// An away task's strip: its review while one runs, else its away state.
function AwayStripRow({ strip, stage }: { strip: AwayStripLook; stage?: string }) {
  return (
    <div data-testid="away-strip" className={cn(stripRow, "flex-wrap", strip.tint)}>
      <span className={cn("flex min-w-[55%] flex-1 items-start gap-1.5", strip.tone)}><span aria-hidden="true">{strip.glyph}</span><span className="min-w-0 [overflow-wrap:anywhere]">{strip.text}</span>
        {/* A review pass says what it does: only the track, named by the stage. */}
        {strip.review ? <StageTrack stage={stage} size="xs" className="mt-[3px]" /> : <CardStage stage={stage} />}
      </span>
      {strip.detail && <span className="ml-auto text-right text-lw-text-2">{strip.detail}</span>}
    </div>
  )
}

// The strip that says why a card needs the operator, with the way there.
function NeedsYouStripRow({ strip }: { strip: NeedsYouStrip }) {
  return (
    <div data-testid="needs-you-strip" className={cn(stripRow, "bg-lw-needs-soft")}>
      <span className="flex min-w-0 items-start gap-1.5 text-lw-needs"><span aria-hidden="true">{strip.glyph}</span><span className="min-w-0 [overflow-wrap:anywhere]">{strip.text}</span></span>
      <StripActionLink action={strip.action} />
    </div>
  )
}

function MetaLine({ card, showWorkspace }: { card: BoardCardType; showWorkspace: boolean }) {
  const parts = cardMetaParts(card, showWorkspace)
  if (parts.length === 0) return null
  return (
    <div data-testid="card-meta" className="flex flex-wrap items-center gap-x-1.5 text-xs text-lw-text-2">
      {parts.map((part, index) => <span key={index} className="contents">{index > 0 && <span aria-hidden="true" className="text-lw-faint">·</span>}<span key={part.key} className={part.className}>{part.text}</span></span>)}
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

// A highlighted card (one just imported) scrolls into the middle of its
// column; the Away lane keeps its selected row in view, as a column does.
function useCardInView(cardRef: RefObject<HTMLDivElement | null>, highlighted: boolean, selected: boolean, away: unknown) {
  useEffect(() => {
    if (highlighted) cardRef.current?.scrollIntoView?.({ behavior: "smooth", block: "center", inline: "nearest" })
  }, [cardRef, highlighted])
  useEffect(() => {
    if (selected && away && !highlighted) cardRef.current?.scrollIntoView?.({ block: "nearest" })
  }, [cardRef, selected, away, highlighted])
}

// The card's border: the accent when selected, else its steady edge.
function borderClass(selected: boolean, edge: string): string {
  if (selected) return "border-lw-accent bg-lw-selected"
  if (edge === "needs") return cn("bg-lw-surface", "border-lw-needs")
  if (edge === "error") return cn("bg-lw-surface", "border-lw-error")
  return cn("bg-lw-surface", "border-lw-line hover:border-lw-line-strong")
}

function ActionMenuButton({ title, onActionMenu }: { title: string; onActionMenu: (position: { x: number; y: number }) => void }) {
  return <button type="button" aria-label={`Open actions for ${title}`} onClick={event => { event.stopPropagation(); const rect = event.currentTarget.getBoundingClientRect(); onActionMenu({ x: rect.right, y: rect.bottom }) }} className="absolute right-0 top-0 z-10 flex h-11 w-11 items-center justify-center rounded-md md:right-1 md:top-1 text-lw-faint hover:bg-lw-raised hover:text-lw-text focus-visible:opacity-100 md:h-7 md:w-7 md:opacity-0 md:group-hover:opacity-100"><MoreHorizontal size={15} /></button>
}

// The header: where the task comes from, and its marks. It always leaves
// room for ⋯ at its right.
function CardSourceLine({ card }: { card: BoardCardType }) {
  const agentTime = card.agent_active ? "" : formatDuration(card.working_seconds + card.waiting_seconds + card.error_seconds)
  const from = card.handoff_from ? `${card.handoff_from.main} · ${card.handoff_from.main_task_id}` : undefined
  return (
    <div data-testid="card-source" className="flex items-center justify-between gap-2 pr-10 font-lw-mono md:pr-8 text-[11px] leading-4 text-lw-faint">
      <span className="truncate" title={from}>{cardSource(card)}</span>
      <span className="flex shrink-0 items-center gap-2">
        {card.has_warning && <Mark glyph="⚠" tone="font-lw text-lw-error">sync failed</Mark>}
        {isMilestone(card) && <span className="rounded-full border border-lw-line-strong px-1.5 font-lw leading-[14px] text-lw-text-2">Milestone</span>}
        {agentTime && <span title="Time agents spent on this task">{agentTime}</span>}
      </span>
    </div>
  )
}

// The title, the card's keyboard primary action: Enter opens the task.
function CardTitle({ title, selected, done, onOpen }: { title: string; selected: boolean; done: boolean; onOpen: () => void }) {
  return (
    <button
      type="button"
      aria-pressed={selected}
      onKeyDown={(event) => {
        if (event.key !== "Enter") return
        event.preventDefault()
        event.stopPropagation()
        onOpen()
      }}
      className={cn("rounded-sm text-left text-sm font-semibold leading-[19px] [overflow-wrap:anywhere] max-md:line-clamp-3 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-lw-accent", done && "text-lw-text-2")}
    >{title}</button>
  )
}

// The strips at the card's foot, what waits first, then the away task's
// actions.
function CardStrips({ card, frame, actions }: { card: BoardCardType; frame: CardFrame; actions?: ReactNode }) {
  return <>
    {frame.needs.map(strip => <NeedsYouStripRow key={strip.text} strip={strip} />)}
    {Boolean(card.review_unreviewed) && <ReviewStrip card={card} />}
    {frame.agentStrip && <AgentStrip card={card} />}
    {frame.away && <AwayStripRow strip={frame.away} stage={card.away?.stage} />}
    {actions && <div onClick={event => event.stopPropagation()} onDoubleClick={event => event.stopPropagation()} className="flex flex-wrap items-center gap-1 border-t border-lw-line px-3 py-2 empty:hidden">{actions}</div>}
  </>
}

// The moving light is for an agent at work, unless animation is off; a card
// that needs the operator has a steady amber edge, one in error a red one.
function tracesAgent(card: BoardCardType): boolean {
  return card.agent_active && card.agent_state === "working" && animatedAgentActivityEnabled()
}

// A right click opens the card's menu where it was clicked, when it has one.
function contextMenuAt(onContextMenu?: (position: { x: number; y: number }) => void) {
  return (event: MouseEvent<HTMLDivElement>) => {
    if (!onContextMenu) return
    event.preventDefault()
    onContextMenu({ x: event.clientX, y: event.clientY })
  }
}

// A ready review tour with nothing left to review.
function ReviewedMark({ card }: { card: BoardCardType }) {
  if (!card.review_ready || card.review_unreviewed) return null
  return <Mark glyph="✓" tone="text-xs text-lw-done">Reviewed</Mark>
}

export function BoardCard({ card, selected, highlighted = false, column, showWorkspace, onClick, onDoubleClick, onContextMenu, onActionMenu, onDragStart, actions }: BoardCardProps) {
  const cardRef = useRef<HTMLDivElement>(null)
  const frame = cardFrame(card)
  useCardInView(cardRef, highlighted, selected, card.away)

  return (
    <div
      ref={cardRef}
      data-card={card.id}
      data-highlighted-task={highlighted || undefined}
      data-selected={selected || undefined}
      data-edge={frame.edge || undefined}
      onClick={onClick}
      onDoubleClick={onDoubleClick}
      onContextMenu={contextMenuAt(onContextMenu)}
      draggable={!card.away}
      onDragStart={onDragStart}
      className={cn(
        // A column lays cards out as a flex column that scrolls; a card keeps
        // its height there rather than being squeezed to fit.
        "group relative flex shrink-0 cursor-pointer flex-col overflow-hidden rounded-lg border font-lw text-lw-text transition-colors",
        borderClass(selected, frame.edge),
        tracesAgent(card) && "lw-trace lw-trace-working",
        highlighted && "lw-highlight",
      )}
    >
      {onActionMenu && <ActionMenuButton title={card.title} onActionMenu={onActionMenu} />}
      <div className="flex flex-col gap-1 px-3 py-2.5">
        <CardSourceLine card={card} />
        <CardTitle title={card.title} selected={selected} done={column === "Done"} onOpen={onDoubleClick ?? onClick} />
        <MetaLine card={card} showWorkspace={showWorkspace} />
        <ReviewedMark card={card} />
        <CardPullRequests card={card} />
      </div>
      <CardStrips card={card} frame={frame} actions={actions} />
    </div>
  )
}
