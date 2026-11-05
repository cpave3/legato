import type { BoardColumn, BoardWorker } from "../../lib/board-types"
import { BoardColumn as BoardColumnComponent } from "./BoardColumn"
import { AwayLane, WorkerReach } from "./AwayLane"
import { isColumnCollapsed } from "../../lib/boardColumns"

interface BoardGridProps {
  columns: BoardColumn[]
  cursorCol: number
  cursorRow: number
  showWorkspace: boolean
  // Whether empty columns are drawn open. When they are not, each is a narrow
  // rail; a board with nothing on it keeps its columns open.
  showEmpty?: boolean
  onToggleEmpty?: () => void
  highlightedTaskId?: string | null
  onCardClick: (colIndex: number, cardIndex: number) => void
  onCardDoubleClick?: (colIndex: number, cardIndex: number) => void
  onCardContextMenu?: (card: BoardColumn["cards"][number], position: { x: number; y: number }) => void
  onCardMove?: (cardId: string, targetColumn: string) => void
  // The workers this main is paired with: the groups of the Away lane, a
  // column marked away.
  workers?: BoardWorker[]
}

export function BoardGrid({ columns, cursorCol, cursorRow, showWorkspace, showEmpty = true, onToggleEmpty, highlightedTaskId, onCardClick, onCardDoubleClick, onCardContextMenu, onCardMove, workers = [] }: BoardGridProps) {
  return (
    <div className="flex h-full gap-2 overflow-x-auto overflow-y-hidden px-4 pt-3.5" style={{ scrollbarWidth: "thin" }}>
      {columns.map((col, i) => col.away ? (
        <AwayLane
          key={col.name}
          workers={workers}
          cards={col.cards}
          selectedIndex={cursorRow}
          isActive={cursorCol === i}
          highlightedTaskId={highlightedTaskId}
          onRowClick={(row) => onCardClick(i, row)}
          groupExtras={(worker) => <WorkerReach worker={worker} />}
        />
      ) : (
        <BoardColumnComponent
          key={col.name}
          name={col.name}
          cards={col.cards}
          selectedIndex={cursorRow}
          isActive={cursorCol === i}
          showWorkspace={showWorkspace}
          highlightedTaskId={highlightedTaskId}
          collapsed={isColumnCollapsed(columns, i, showEmpty)}
          onExpand={onToggleEmpty}
          onCardClick={onCardClick}
          onCardDoubleClick={onCardDoubleClick}
          onCardContextMenu={onCardContextMenu}
          colIndex={i}
          onCardDrop={onCardMove}
        />
      ))}
    </div>
  )
}
