import { useEffect, useRef, useState } from "react"
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
  onCardClick: (colIndex: number, cardIndex: number) => void
  onCardContextMenu?: (card: BoardCardType, position: { x: number; y: number }) => void
  colIndex: number
  onCardDrop?: (cardId: string, column: string) => void
}

function columnBorderColor(name: string): string {
  switch (name) {
    case "Backlog":
      return "border-t-[#B4B2A9]"
    case "Ready":
      return "border-t-[#85B7EB]"
    case "Doing":
      return "border-t-[#7F77DD]"
    case "Review":
      return "border-t-[#5DCAA5]"
    case "Done":
      return "border-t-[#97C459]"
    default:
      return "border-t-zinc-600"
  }
}

export function BoardColumn({
  name,
  cards,
  selectedIndex,
  isActive,
  showWorkspace,
  highlightedTaskId,
  onCardClick,
  onCardContextMenu,
  colIndex,
  onCardDrop,
}: BoardColumnProps) {
  const [dragOver, setDragOver] = useState(false)
  const cardRefs = useRef<(HTMLDivElement | null)[]>([])

  useEffect(() => {
    if (!isActive) return
    cardRefs.current[selectedIndex]?.scrollIntoView?.({ block: "nearest" })
  }, [isActive, selectedIndex])

  return (
    <div
      className={cn(
        "flex h-full min-h-0 min-w-[280px] flex-1 flex-col overflow-hidden rounded transition-colors",
        dragOver && "bg-indigo-950/30 ring-1 ring-inset ring-indigo-500/60"
      )}
      onDragOver={(event) => {
        if (!onCardDrop) return
        event.preventDefault()
        event.dataTransfer.dropEffect = "move"
        setDragOver(true)
      }}
      onDragLeave={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setDragOver(false)
      }}
      onDrop={(event) => {
        event.preventDefault()
        setDragOver(false)
        const cardId = event.dataTransfer.getData("text/plain")
        if (cardId) onCardDrop?.(cardId, name)
      }}
    >
      <div
        className={cn(
          "flex items-center justify-between border-t-4 px-2 py-2 transition-[background-color,box-shadow]",
          columnBorderColor(name),
          isActive ? "bg-indigo-950/35 shadow-sm shadow-indigo-950/50" : "bg-transparent"
        )}
      >
        <span className={cn("text-xs font-bold uppercase tracking-wider", isActive ? "text-[#AFA9EC]" : "text-zinc-500")}>
          {name}
        </span>
        <span className="rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] font-medium text-zinc-400">
          {cards.length}
        </span>
      </div>
      <div data-board-column-cards className="board-column-scrollbar flex min-h-0 min-w-0 flex-1 flex-col gap-2 overflow-x-hidden overflow-y-auto py-2">
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
    </div>
  )
}
