import type { BoardColumn } from "./board-types"

// Whether a column is drawn as a narrow rail: an empty one, while empty
// columns are folded away. A board with nothing on it keeps its columns open.
export function isColumnCollapsed(columns: BoardColumn[], index: number, showEmpty: boolean): boolean {
  if (showEmpty || columns[index]?.cards.length) return false
  return columns.some((column) => column.cards.length > 0)
}
