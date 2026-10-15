import { useEffect, useRef, useState, type DragEvent } from "react"
import { cn } from "../../lib/utils"
import type { BoardCard as BoardCardType } from "../../lib/board-types"
import { BoardCard } from "./BoardCard"

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
  onCardClick: (colIndex: number, cardIndex: number) => void
  onCardDoubleClick?: (colIndex: number, cardIndex: number) => void
  onCardContextMenu?: (card: BoardCardType, position: { x: number; y: number }) => void
  colIndex: number
  onCardDrop?: (cardId: string, column: string) => void
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
    default:
      return "border-[#4a4955]"
  }
}

function useDropTarget(name: string, onCardDrop?: (cardId: string, column: string) => void) {
  const [dragOver, setDragOver] = useState(false)
  return {
    dragOver,
    handlers: {
      onDragOver: (event: DragEvent<HTMLElement>) => {
        if (!onCardDrop) return
        event.preventDefault()
        event.dataTransfer.dropEffect = "move"
        setDragOver(true)
      },
      onDragLeave: (event: DragEvent<HTMLElement>) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setDragOver(false)
      },
      onDrop: (event: DragEvent<HTMLElement>) => {
        event.preventDefault()
        setDragOver(false)
        const cardId = event.dataTransfer.getData("text/plain")
        if (cardId) onCardDrop?.(cardId, name)
      },
    },
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
  onCardClick,
  onCardDoubleClick,
  onCardContextMenu,
  colIndex,
  onCardDrop,
}: BoardColumnProps) {
  const { dragOver, handlers } = useDropTarget(name, onCardDrop)
  const cardRefs = useRef<(HTMLDivElement | null)[]>([])

  useEffect(() => {
    if (!isActive) return
    cardRefs.current[selectedIndex]?.scrollIntoView?.({ block: "nearest" })
  }, [isActive, selectedIndex])

  if (collapsed) {
    return (
      <button
        type="button"
        aria-label={`${name}, ${cards.length} cards`}
        title="Show empty columns (z)"
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
      {...handlers}
    >
      <div
        data-column-header
        data-active={isActive || undefined}
        className={cn("flex items-center justify-between border-b-2 px-1 pb-1.5 pt-0.5", columnRule(name))}
      >
        <span className={cn("text-xs font-semibold uppercase tracking-[1px]", isActive ? "text-lw-text" : "text-lw-text-2")}>{name}</span>
        <span className="font-lw-mono text-xs text-lw-text-2">{cards.length}</span>
      </div>
      <div data-board-column-cards className="board-column-scrollbar flex min-h-0 min-w-0 flex-1 flex-col gap-2 overflow-x-hidden overflow-y-auto pb-2">
        {cards.map((card, i) => (
          <div
            key={card.id}
            ref={(element) => { cardRefs.current[i] = element }}
            data-board-column-card
            className="min-w-0 w-full"
          >
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
              }}
            />
          </div>
        ))}
      </div>
    </section>
  )
}
