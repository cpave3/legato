import { useEffect, useState } from "react"
import { ChevronDown, ChevronRight, MoreHorizontal } from "lucide-react"
import type { BoardColumn } from "../../lib/board-types"
import { BoardCard } from "./BoardCard"

const storageKey = "legato.mobile-board.collapsed"

function initialCollapsed(): Set<string> {
  try {
    return new Set(JSON.parse(localStorage.getItem(storageKey) || "[]") as string[])
  } catch {
    return new Set()
  }
}

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
  const [collapsed, setCollapsed] = useState(initialCollapsed)

  useEffect(() => {
    try {
      localStorage?.setItem(storageKey, JSON.stringify([...collapsed]))
    } catch {
      // Persistence is an enhancement; private browsing must not break the board.
    }
  }, [collapsed])

  function toggle(name: string) {
    setCollapsed((current) => {
      const next = new Set(current)
      if (next.has(name)) next.delete(name)
      else next.add(name)
      return next
    })
  }

  return (
    <div data-mobile-board className="h-full overflow-y-auto px-3 pb-5 pt-2">
      {columns.map((column, columnIndex) => {
        const closed = collapsed.has(column.name) && !column.cards.some((card) => card.id === highlightedTaskId)
        return (
          <section key={column.name} className="mb-3 overflow-hidden rounded-xl border border-zinc-800 bg-zinc-950">
            <button
              type="button"
              aria-expanded={!closed}
              onClick={() => toggle(column.name)}
              className="flex min-h-11 w-full items-center gap-2 px-3 text-left"
            >
              {closed ? <ChevronRight size={16} /> : <ChevronDown size={16} />}
              <h2 className="text-sm font-semibold text-zinc-200">{column.name}</h2>
              <span className="ml-auto rounded-full bg-zinc-800 px-2 py-0.5 text-xs text-zinc-400">{column.cards.length}</span>
            </button>
            {!closed && (
              <div className="space-y-2 border-t border-zinc-800 p-2">
                {column.cards.length === 0 && <p className="px-2 py-4 text-center text-xs text-zinc-600">No tasks</p>}
                {column.cards.map((card, rowIndex) => (
                  <div key={card.id} className="relative">
                    <BoardCard
                      card={card}
                      column={column.name}
                      selected={false}
                      highlighted={card.id === highlightedTaskId}
                      showWorkspace={showWorkspace}
                      onClick={() => onCardClick(columnIndex, rowIndex)}
                      onContextMenu={(position) => onCardContextMenu?.(card, position)}
                    />
                    <button
                      type="button"
                      aria-label={`Open actions for ${card.title}`}
                      onClick={(event) => {
                        event.stopPropagation()
                        const rect = event.currentTarget.getBoundingClientRect()
                        onCardContextMenu?.(card, { x: rect.right, y: rect.bottom })
                      }}
                      className="absolute right-1 top-1 flex h-11 w-11 items-center justify-center rounded-lg text-zinc-500 hover:bg-zinc-800 hover:text-zinc-200"
                    >
                      <MoreHorizontal size={18} />
                    </button>
                  </div>
                ))}
              </div>
            )}
          </section>
        )
      })}
    </div>
  )
}
