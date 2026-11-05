import type { BoardColumn, BoardWorker } from "../../lib/board-types"
import { BoardColumn as BoardColumnComponent } from "./BoardColumn"
import { AwayLane, WorkerReach } from "./AwayLane"
import { AwayRowActions, ReturnAllDoneButton, WorkerGroupMenu } from "./AwayActions"
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
  // A card dropped on the Away lane or its rail, to hand off.
  onCardHandOff?: (cardId: string) => void
  // The workers this main is paired with: the groups of the Away lane, a
  // column marked away.
  workers?: BoardWorker[]
  // The server the Away lane's Return, Recall and Return all done call;
  // without it the lane offers none.
  baseUrl?: string
  // Told what those did: a task back home, or why one was refused.
  onAwayNotice?: (text: string, error: boolean) => void
}

export function BoardGrid({ columns, cursorCol, cursorRow, showWorkspace, showEmpty = true, onToggleEmpty, highlightedTaskId, onCardClick, onCardDoubleClick, onCardContextMenu, onCardMove, onCardHandOff, workers = [], baseUrl, onAwayNotice }: BoardGridProps) {
  return (
    <div className="flex h-full gap-2 overflow-x-auto overflow-y-hidden px-4 pt-3.5" style={{ scrollbarWidth: "thin" }}>
      {columns.map((col, i) => col.away && isColumnCollapsed(columns, i, showEmpty) ? (
        // With nothing away, the lane is a rail like an empty column, and
        // still takes a card to hand off.
        <BoardColumnComponent
          key={col.name}
          name={col.name}
          cards={col.cards}
          selectedIndex={cursorRow}
          isActive={cursorCol === i}
          showWorkspace={showWorkspace}
          collapsed
          railTitle="Drop a card here to hand it off · show empty columns (z)"
          onExpand={onToggleEmpty}
          onCardClick={onCardClick}
          colIndex={i}
          onCardDrop={onCardHandOff && ((cardId) => onCardHandOff(cardId))}
        />
      ) : col.away ? (
        <AwayLane
          key={col.name}
          workers={workers}
          cards={col.cards}
          selectedIndex={cursorRow}
          isActive={cursorCol === i}
          highlightedTaskId={highlightedTaskId}
          onRowClick={(row) => onCardClick(i, row)}
          groupExtras={(worker, groupCards) => <>
            <WorkerReach worker={worker} />
            {baseUrl !== undefined && <ReturnAllDoneButton baseUrl={baseUrl} worker={worker} cards={groupCards} onNotice={onAwayNotice} />}
            {baseUrl !== undefined && <WorkerGroupMenu baseUrl={baseUrl} worker={worker} cards={groupCards} onNotice={onAwayNotice} />}
          </>}
          rowActions={baseUrl === undefined ? undefined : (card) => <AwayRowActions baseUrl={baseUrl} card={card} onNotice={onAwayNotice} />}
          onCardDrop={onCardHandOff}
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
