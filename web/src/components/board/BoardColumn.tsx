import { useEffect, useRef, useState, type DragEvent } from "react"
import { cn } from "../../lib/utils"
import type { BoardCard as BoardCardType } from "../../lib/board-types"
import { dropPlacement, dropSlot } from "../../lib/boardOrder"
import { BoardCard } from "./BoardCard"
import { useDropTarget } from "./useDropTarget"

interface BoardColumnProps {
  name: string
  cards: BoardCardType[]
  selectedIndex: number
  isActive: boolean
  showWorkspace: boolean
  highlightedTaskId?: string | null
  // A column drawn as a narrow rail: an empty one, while empty columns are
  // folded away. onExpand opens them all again.
  collapsed?: boolean
  onExpand?: () => void
  // The rail's tooltip, when it says more than how to open it.
  railTitle?: string
  onCardClick: (colIndex: number, cardIndex: number) => void
  onCardDoubleClick?: (colIndex: number, cardIndex: number) => void
  onCardContextMenu?: (card: BoardCardType, position: { x: number; y: number }) => void
  colIndex: number
  // A card dropped on the column: its ID, the column's name and the card it
  // goes before (null for the end). A rail has no places, so it gives no
  // before. A card dropped where it already is is not reported.
  onCardDrop?: (cardId: string, column: string, before?: string | null) => void
}

// The rule under each column's name, in the color of where work stands.
function columnRule(name: string): string {
  switch (name.toLowerCase()) {
    case "ready":
    case "done":
      return "border-lw-done"
    case "doing":
      return "border-lw-accent"
    case "review":
      return "border-lw-live"
    case "unmapped":
      return "border-lw-line-strong"
    case "away":
      return "border-lw-accent-ring"
    default:
      return "border-[#4a4955]"
  }
}

export function BoardColumn({
  name,
  cards,
  selectedIndex,
  isActive,
  showWorkspace,
  highlightedTaskId,
  collapsed = false,
  onExpand,
  railTitle,
  onCardClick,
  onCardDoubleClick,
  onCardContextMenu,
  colIndex,
  onCardDrop,
}: BoardColumnProps) {
  const cardRefs = useRef<(HTMLDivElement | null)[]>([])
  // The card of this column being dragged, if one is: dropping it just above
  // or below itself changes nothing.
  const draggingId = useRef<string | null>(null)
  // The slot under the pointer while a card is dragged over the column.
  const [slot, setSlot] = useState<number | null>(null)

  const placementAt = (event: DragEvent<HTMLElement>, cardId: string) => {
    const boxes = cards.map((_, i) => cardRefs.current[i]?.getBoundingClientRect() ?? { top: 0, height: 0 })
    const at = dropSlot(boxes, event.clientY)
    return { slot: at, ...dropPlacement(cards.map((card) => card.id), cardId, at) }
  }

  const { dragOver, handlers } = useDropTarget(name, onCardDrop && ((cardId, column, event) => {
    setSlot(null)
    draggingId.current = null
    if (collapsed) {
      onCardDrop(cardId, column)
      return
    }
    const placement = placementAt(event, cardId)
    if (!placement.unchanged) onCardDrop(cardId, column, placement.before)
  }))

  const columnHandlers = {
    ...handlers,
    onDragOver: (event: DragEvent<HTMLElement>) => {
      handlers.onDragOver(event)
      if (!onCardDrop) return
      // The dragged card's ID cannot be read until the drop; only this
      // column's own card is known, from its dragstart.
      const placement = placementAt(event, draggingId.current ?? "")
      setSlot(placement.unchanged ? null : placement.slot)
    },
  }
  const showSlot = dragOver && slot !== null ? slot : null

  useEffect(() => {
    if (!isActive) return
    cardRefs.current[selectedIndex]?.scrollIntoView?.({ block: "nearest" })
  }, [isActive, selectedIndex])

  if (collapsed) {
    return (
      <button
        type="button"
        aria-label={`${name}, ${cards.length} cards`}
        title={railTitle ?? "Show empty columns (z)"}
        onClick={onExpand}
        {...handlers}
        className={cn(
          "flex w-10 shrink-0 flex-col items-center gap-2.5 border-t-2 pt-2.5 font-lw text-[11px] text-lw-faint transition-colors hover:text-lw-text-2",
          columnRule(name),
          dragOver && "bg-lw-selected text-lw-text",
        )}
      >
        <span className="font-lw-mono">{cards.length}</span>
        <span className="uppercase tracking-[2px] [writing-mode:vertical-rl]">{name}</span>
      </button>
    )
  }

  return (
    <section
      aria-label={name}
      className={cn(
        "flex h-full min-h-0 min-w-[260px] flex-1 basis-0 flex-col gap-2 rounded-lg font-lw transition-colors",
        dragOver && "bg-lw-surface ring-1 ring-inset ring-lw-accent-ring"
      )}
      {...columnHandlers}
    >
      <div
        data-column-header
        data-active={isActive || undefined}
        className={cn("flex items-center justify-between border-b-2 px-1 pb-1.5 pt-0.5", columnRule(name))}
      >
        <span className={cn("text-xs font-semibold uppercase tracking-[1px]", isActive ? "text-lw-text" : "text-lw-text-2")}>{name}</span>
        <span className="font-lw-mono text-xs text-lw-text-2">{cards.length}</span>
      </div>
      <div data-board-column-cards className="board-column-scrollbar flex min-h-0 min-w-0 flex-1 flex-col gap-2 overflow-x-hidden overflow-y-auto pb-2 pt-1">
        {cards.map((card, i) => (
          <div
            key={card.id}
            ref={(element) => { cardRefs.current[i] = element }}
            data-board-column-card
            className="relative min-w-0 w-full"
            onDragEnd={() => { draggingId.current = null }}
          >
            {/* Where a dragged card will land: drawn in the gap above the
                card (the list's top padding, for the first), so drawing it
                moves nothing under the pointer. */}
            {showSlot === i && <DropLine at={i === 0 ? "-top-[3px]" : "-top-[5px]"} />}
            {showSlot === cards.length && i === cards.length - 1 && <DropLine at="-bottom-[5px]" />}
            <BoardCard
              card={card}
              selected={isActive && selectedIndex === i}
              highlighted={card.id === highlightedTaskId}
              column={name}
              showWorkspace={showWorkspace}
              onClick={() => onCardClick(colIndex, i)}
              onDoubleClick={onCardDoubleClick ? () => onCardDoubleClick(colIndex, i) : undefined}
              onContextMenu={(position) => onCardContextMenu?.(card, position)}
              onActionMenu={(position) => onCardContextMenu?.(card, position)}
              onDragStart={(event) => {
                event.dataTransfer.effectAllowed = "move"
                event.dataTransfer.setData("text/plain", card.id)
                draggingId.current = card.id
              }}
            />
          </div>
        ))}
      </div>
    </section>
  )
}

function DropLine({ at }: { at: string }) {
  return <div data-drop-indicator aria-hidden className={cn("pointer-events-none absolute inset-x-0 z-10 h-0.5 rounded-full bg-lw-accent", at)} />
}
