import { useState } from "react"
import { cn } from "../../lib/utils"
import type { BoardCard as BoardCardType } from "../../lib/board-types"
import { BoardCard } from "./BoardCard"

interface BoardColumnProps {
  name: string
  cards: BoardCardType[]
  selectedIndex: number
  isActive: boolean
  showWorkspace: boolean
  onCardClick: (colIndex: number, cardIndex: number) => void
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
  onCardClick,
  colIndex,
  onCardDrop,
}: BoardColumnProps) {
  const [dragOver, setDragOver] = useState(false)

  return (
    <div
      className={cn(
        "flex min-w-[280px] flex-1 flex-col rounded transition-colors",
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
          "flex items-center justify-between border-t-4 px-2 py-2",
          columnBorderColor(name),
          isActive ? "bg-zinc-900/60" : "bg-transparent"
        )}
      >
        <span className={cn("text-xs font-bold uppercase tracking-wider", isActive ? "text-[#AFA9EC]" : "text-zinc-500")}>
          {name}
        </span>
        <span className="rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] font-medium text-zinc-400">
          {cards.length}
        </span>
      </div>
      <div className="flex flex-col gap-1.5 py-2">
        {cards.map((card, i) => (
          <BoardCard
            key={card.id}
            card={card}
            selected={isActive && selectedIndex === i}
            column={name}
            showWorkspace={showWorkspace}
            onClick={() => onCardClick(colIndex, i)}
            onDragStart={(event) => {
              event.dataTransfer.effectAllowed = "move"
              event.dataTransfer.setData("text/plain", card.id)
            }}
          />
        ))}
      </div>
    </div>
  )
}
