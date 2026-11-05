import { useEffect, useRef, useState, type ReactNode } from "react"
import { ExternalLink } from "lucide-react"

import { cn } from "../../lib/utils"
import { awayDetail, awayGroups, awayLaneName, awayLook, awayTime, workerReach } from "../../lib/away"
import { awayPRSummary } from "../../lib/awayPullRequests"
import type { BoardCard, BoardWorker } from "../../lib/board-types"
import { useDropTarget } from "./useDropTarget"

// The Away lane at the end of a main's web board: the tasks away on workers,
// grouped by worker. Each group's header names the worker and opens it; each
// row shows the task's title and ID, the column it left, the worker's state
// of it as last heard, and when it was handed off. A row is selected like a
// card, which shows its task panel. A card dragged from a column and dropped
// on the lane is handed off. See docs/ai/web-ui.md § The Away lane.

interface AwayLaneProps {
  workers: BoardWorker[]
  // The lane's cards, in the order of its groups (withAwayLane).
  cards: BoardCard[]
  selectedIndex: number
  isActive: boolean
  highlightedTaskId?: string | null
  onRowClick: (index: number) => void
  // What a group's header adds after its name: how the worker can be
  // reached, its Return all done.
  groupExtras?: (worker: BoardWorker, cards: BoardCard[]) => ReactNode
  // What a row offers beside it: Return, Recall, Take back.
  rowActions?: (card: BoardCard) => ReactNode
  // A card dropped on the lane: its ID, to hand it off.
  onCardDrop?: (cardId: string) => void
}

// An away task's row: in the Away lane, and in the phone board's Away section.
export function AwayRow({ card, selected, highlighted, onClick, actions }: { card: BoardCard; selected: boolean; highlighted: boolean; onClick: () => void; actions?: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null)
  const away = card.away!
  const look = awayLook(away)
  const prs = awayPRSummary(card.pull_requests)
  useEffect(() => {
    if (selected || highlighted) ref.current?.scrollIntoView?.({ block: "nearest" })
  }, [selected, highlighted])
  return (
    <div ref={ref} data-card={card.id} className={cn("flex items-start gap-2 border-t border-lw-line first:border-t-0", selected && "bg-lw-selected", highlighted && "ring-2 ring-inset ring-cyan-400")}>
      <button
        type="button"
        data-selected={selected || undefined}
        onClick={onClick}
        className="flex min-w-0 flex-1 flex-col gap-1 px-3 py-2 text-left hover:bg-lw-raised"
      >
        <span className="text-sm font-semibold leading-[19px] text-lw-text">{card.title}</span>
        <span className="flex flex-wrap items-center gap-x-1.5 gap-y-1 text-xs text-lw-text-2">
          <span className="font-lw-mono text-lw-faint">{card.id}</span>
          <span aria-hidden="true" className="text-lw-faint">·</span>
          <span>from {away.left_column}</span>
          <span aria-hidden="true" className="text-lw-faint">·</span>
          <span data-testid="away-state" title={awayDetail(away)} className={cn("inline-flex max-w-full items-center gap-1 rounded-full px-2 py-px", look.tint, look.tone)}>
            <span aria-hidden="true">{look.glyph}</span>
            <span className="truncate">{look.label}</span>
          </span>
          <span aria-hidden="true" className="text-lw-faint">·</span>
          <time dateTime={away.handed_off_at} title="Handed off" className="font-lw-mono">{awayTime(away.handed_off_at)}</time>
        </span>
        {prs && <span data-testid="away-prs" className={cn("text-xs", prs.blocked ? "text-lw-error" : "text-lw-text-2")}>{prs.text}</span>}
      </button>
      {actions && <div className="flex shrink-0 items-center gap-1 py-2 pr-2">{actions}</div>}
    </div>
  )
}

// WorkerReach is a worker group's header saying whether the worker can be
// reached: when this main last pulled it, since when it is unreachable, or
// that it no longer accepts this main.
export function WorkerReach({ worker, now }: { worker: BoardWorker; now?: Date }) {
  // "2 min ago" moves on by itself, between the board's refreshes.
  const [tick, setTick] = useState(() => new Date())
  useEffect(() => {
    if (now) return
    const timer = setInterval(() => setTick(new Date()), 30_000)
    return () => clearInterval(timer)
  }, [now])
  const reach = workerReach(worker, now ?? tick)
  return (
    <span data-testid="worker-reach" title={worker.last_pull_at ? `Last pulled ${new Date(worker.last_pull_at).toLocaleString()}` : undefined}
      className={cn("text-xs", reach.tone === "bad" ? "text-lw-error" : reach.tone === "quiet" ? "text-lw-faint" : "text-lw-text-2")}>
      {reach.text}
    </span>
  )
}

export function AwayLane({ workers, cards, selectedIndex, isActive, highlightedTaskId, onRowClick, groupExtras, rowActions, onCardDrop }: AwayLaneProps) {
  const groups = awayGroups(workers, cards)
  const { dragOver, handlers } = useDropTarget(awayLaneName, onCardDrop && ((cardId) => onCardDrop(cardId)))
  let index = 0
  return (
    <section aria-label={awayLaneName} {...handlers} className={cn("flex h-full min-h-0 min-w-[300px] flex-1 basis-0 flex-col gap-2 rounded-lg font-lw transition-colors", dragOver && "bg-lw-accent-soft ring-1 ring-inset ring-lw-accent-ring")}>
      <div data-column-header data-active={isActive || undefined} className="flex items-center justify-between border-b-2 border-lw-accent-ring px-1 pb-1.5 pt-0.5">
        <span className={cn("text-xs font-semibold uppercase tracking-[1px]", isActive ? "text-lw-text" : "text-lw-text-2")}>{awayLaneName}</span>
        <span className="font-lw-mono text-xs text-lw-text-2">{cards.length}</span>
      </div>
      <div className="board-column-scrollbar flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto pb-2">
        {groups.map(({ worker, cards: groupCards }) => (
          <div key={worker.instance_id} role="group" aria-label={worker.name} className="overflow-hidden rounded-lg border border-lw-line bg-lw-surface">
            <div className="flex flex-wrap items-center gap-x-2 gap-y-1 border-b border-lw-line px-3 py-2">
              <span className="text-sm font-semibold text-lw-text">{worker.name}</span>
              {groupExtras?.(worker, groupCards)}
              {worker.address && (
                <a href={worker.address} target="_blank" rel="noreferrer" aria-label={`Open on ${worker.name}`} title={`Open ${worker.name}'s board`} className="ml-auto flex items-center gap-1 text-xs text-lw-link hover:underline">
                  Open <ExternalLink size={12} aria-hidden="true" />
                </a>
              )}
            </div>
            {groupCards.length === 0 ? (
              <p className="m-0 px-3 py-2.5 text-xs text-lw-faint">Nothing away on {worker.name}. Drag a card here, or hand it off from its menu or its panel.</p>
            ) : (
              groupCards.map((card) => {
                const i = index++
                return (
                  <AwayRow
                    key={card.id}
                    card={card}
                    selected={isActive && selectedIndex === i}
                    highlighted={card.id === highlightedTaskId}
                    onClick={() => onRowClick(i)}
                    actions={rowActions?.(card)}
                  />
                )
              })
            )}
          </div>
        ))}
      </div>
    </section>
  )
}
