import { useEffect, useState, type ReactNode } from "react"
import { ExternalLink } from "lucide-react"

import { cn } from "../../lib/utils"
import { awayGroups, awayLaneName, workerReach } from "../../lib/away"
import type { BoardCard, BoardWorker } from "../../lib/board-types"
import { BoardCard as BoardCardView } from "./BoardCard"
import { useDropTarget } from "./useDropTarget"

// The Away lane at the end of a main's web board: the tasks away on workers,
// grouped by worker. Each group's header names the worker and opens it; each
// task is the board's own card, its away variant: the worker in its header,
// the review or the away state in its strip, its actions under it. A card
// is selected as in a column, which shows its task panel. A card dragged from a column and dropped
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
  // What a row offers under its details: Return, Recall, Take back.
  rowActions?: (card: BoardCard) => ReactNode
  // A card dropped on the lane: its ID, to hand it off.
  onCardDrop?: (cardId: string) => void
  // A card's menu, from a right click or its ⋯.
  onCardContextMenu?: (card: BoardCard, position: { x: number; y: number }) => void
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

export function AwayLane({ workers, cards, selectedIndex, isActive, highlightedTaskId, onRowClick, groupExtras, rowActions, onCardDrop, onCardContextMenu }: AwayLaneProps) {
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
                  <div key={card.id} className="p-1.5">
                    <BoardCardView
                      card={card}
                      selected={isActive && selectedIndex === i}
                      highlighted={card.id === highlightedTaskId}
                      column="Away"
                      showWorkspace={false}
                      onClick={() => onRowClick(i)}
                      onContextMenu={onCardContextMenu && ((position) => onCardContextMenu(card, position))}
                      onActionMenu={onCardContextMenu && ((position) => onCardContextMenu(card, position))}
                      actions={rowActions?.(card)}
                    />
                  </div>
                )
              })
            )}
          </div>
        ))}
      </div>
    </section>
  )
}
