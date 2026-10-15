import { useEffect, useRef, useState } from "react"
import { cn } from "../../lib/utils"
import type { BoardColumn } from "../../lib/board-types"
import { BoardCard } from "./BoardCard"

const storageKey = "legato.mobile-board.column"
const swipeDistance = 60

function rememberedColumn(): string | null {
  try {
    return localStorage.getItem(storageKey)
  } catch {
    return null
  }
}

// The column shown when the board opens: the one holding a highlighted task,
// else the one last looked at, else the first with cards in it.
function initialColumn(columns: BoardColumn[], highlightedTaskId?: string | null): number {
  const highlighted = highlightedTaskId ? columns.findIndex((column) => column.cards.some((card) => card.id === highlightedTaskId)) : -1
  if (highlighted >= 0) return highlighted
  const remembered = columns.findIndex((column) => column.name === rememberedColumn())
  if (remembered >= 0) return remembered
  return Math.max(0, columns.findIndex((column) => column.cards.length > 0))
}

// The board on a phone: a chip for each column, and the cards of one column
// at a time. A sideways swipe moves to the next or previous column.
export function MobileBoardList({
  columns,
  showWorkspace,
  highlightedTaskId,
  onCardClick,
  onCardContextMenu,
}: {
  columns: BoardColumn[]
  showWorkspace: boolean
  highlightedTaskId?: string | null
  onCardClick: (column: number, row: number) => void
  onCardContextMenu?: (card: BoardColumn["cards"][number], position: { x: number; y: number }) => void
}) {
  const [shown, setShown] = useState(() => initialColumn(columns, highlightedTaskId))
  const touchStart = useRef<{ x: number; y: number } | null>(null)
  const chipRefs = useRef<(HTMLButtonElement | null)[]>([])
  const index = Math.min(shown, Math.max(0, columns.length - 1))
  const column = columns[index]

  useEffect(() => {
    if (!highlightedTaskId) return
    const highlighted = columns.findIndex((candidate) => candidate.cards.some((card) => card.id === highlightedTaskId))
    if (highlighted >= 0) setShown(highlighted)
  }, [columns, highlightedTaskId])

  useEffect(() => {
    chipRefs.current[index]?.scrollIntoView?.({ block: "nearest", inline: "nearest" })
  }, [index])

  function show(next: number) {
    if (next < 0 || next >= columns.length) return
    setShown(next)
    try {
      localStorage.setItem(storageKey, columns[next].name)
    } catch {
      // Remembering the column is a convenience; private browsing must not break the board.
    }
  }

  if (!column) return null

  return (
    <div data-mobile-board className="flex h-full flex-col font-lw">
      <div role="tablist" aria-label="Columns" className="flex shrink-0 gap-1.5 overflow-x-auto px-4 pb-2.5" style={{ scrollbarWidth: "none" }}>
        {columns.map((candidate, candidateIndex) => {
          const selected = candidateIndex === index
          return (
            <button
              key={candidate.name}
              ref={(element) => { chipRefs.current[candidateIndex] = element }}
              type="button"
              role="tab"
              aria-selected={selected}
              onClick={() => show(candidateIndex)}
              className={cn(
                "h-9 shrink-0 whitespace-nowrap rounded-full px-3.5 text-[13px]",
                selected
                  ? "bg-lw-text font-semibold text-lw-ground"
                  : cn("border border-lw-line-strong", candidate.cards.length ? "text-lw-text-2" : "text-lw-faint"),
              )}
            >
              {candidate.name} {candidate.cards.length}
            </button>
          )
        })}
      </div>
      <div
        role="tabpanel"
        aria-label={column.name}
        className="flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto px-4 pb-4 pt-0.5"
        onTouchStart={(event) => {
          const touch = event.touches[0]
          touchStart.current = touch ? { x: touch.clientX, y: touch.clientY } : null
        }}
        onTouchEnd={(event) => {
          const start = touchStart.current
          const touch = event.changedTouches[0]
          touchStart.current = null
          if (!start || !touch) return
          const dx = touch.clientX - start.x
          const dy = touch.clientY - start.y
          if (Math.abs(dx) < swipeDistance || Math.abs(dx) < Math.abs(dy) * 1.5) return
          show(index + (dx < 0 ? 1 : -1))
        }}
      >
        {column.cards.length === 0 && <p className="py-8 text-center text-sm text-lw-faint">Nothing in {column.name}</p>}
        {column.cards.map((card, rowIndex) => (
          <BoardCard
            key={card.id}
            card={card}
            column={column.name}
            selected={false}
            highlighted={card.id === highlightedTaskId}
            showWorkspace={showWorkspace}
            onClick={() => onCardClick(index, rowIndex)}
            onContextMenu={(position) => onCardContextMenu?.(card, position)}
            onActionMenu={(position) => onCardContextMenu?.(card, position)}
          />
        ))}
        {columns.length > 1 && <p className="pt-1 text-center text-xs text-lw-faint">Swipe sideways for other columns · press and hold a card for its actions</p>}
      </div>
    </div>
  )
}
