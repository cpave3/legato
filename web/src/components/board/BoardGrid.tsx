import type { BoardColumn } from "../../lib/board-types"
import { BoardColumn as BoardColumnComponent } from "./BoardColumn"

interface BoardGridProps {
  columns: BoardColumn[]
  cursorCol: number
  cursorRow: number
  showWorkspace: boolean
  onCardClick: (colIndex: number, cardIndex: number) => void
}

export function BoardGrid({ columns, cursorCol, cursorRow, showWorkspace, onCardClick }: BoardGridProps) {
  return (
    <div className="flex h-full gap-1 overflow-x-auto overflow-y-hidden px-2 pb-2" style={{ scrollbarWidth: "thin" }}>
      {columns.map((col, i) => (
        <BoardColumnComponent
          key={col.name}
          name={col.name}
          cards={col.cards}
          selectedIndex={cursorRow}
          isActive={cursorCol === i}
          showWorkspace={showWorkspace}
          onCardClick={onCardClick}
          colIndex={i}
        />
      ))}
    </div>
  )
}
