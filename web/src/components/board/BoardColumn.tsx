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
}: BoardColumnProps) {
  return (
    <div className="flex min-w-[280px] flex-1 flex-col">
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
          />
        ))}
      </div>
    </div>
  )
}
