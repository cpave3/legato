import { useEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from "react"
import { pathAt, rangeOf, type Item } from "../../lib/workflowDocument"
import { addressOf, sameSelection, type Selection } from "../../lib/workflowAddress"
import { isContainerKey, isTyping, type Row } from "./outlineRows"

type Last = { address: string; place: number } | null

// shownOf is the row marked: beside the YAML the one at the cursor, else
// the row chosen.
function shownOf(navigating: boolean, text: string, cursor: number | undefined, selection: Selection | null): Selection | null {
  if (!navigating) return selection
  const atCursor = cursor !== undefined ? pathAt(text, cursor)?.item ?? null : null
  if (!atCursor) return null
  return atCursor.kind === "about" ? { kind: "about" } : atCursor
}

const sameKind = (order: Row[], selection: Selection) => order.filter(row => row.selection.kind === selection.kind)

// followingOf is the row a renamed selection now is: the row of its kind in
// the same place, until the address catches up.
function followingOf(order: Row[], selection: Selection | null, found: number, last: Last): Row | null {
  if (!selection || found >= 0 || last?.address !== addressOf(selection)) return null
  return sameKind(order, selection)[last.place] ?? null
}

// useFollowRename remembers the chosen row's place among its kind, so a
// rename (which moves a row's address) follows it to the row of its kind
// in the same place. Until the address catches up, the renamed row is
// still the one shown, so its inspector (and the tab it is on) stays.
function useFollowRename(order: Row[], selection: Selection | null, onSelect: (selection: Selection | null) => void, off: boolean) {
  const [last, setLast] = useState<Last>(null)
  const found = selection ? order.findIndex(row => sameSelection(selection, row.selection)) : -1
  const following = followingOf(order, selection, found, last)
  useEffect(() => {
    if (off) return
    if (!selection) { setLast(null); return }
    const address = addressOf(selection)
    if (found >= 0) {
      const place = sameKind(order, selection).findIndex(row => sameSelection(selection, row.selection))
      setLast(previous => previous?.address === address && previous.place === place ? previous : { address, place })
      return
    }
    if (following) onSelect(following.selection)
  }, [selection, found, order, onSelect, following, off])
  return { selectedRow: found >= 0 ? order[found] : following }
}

// jumpRangeOf is the text a row names beside the YAML: About is the first
// top-level field the outline does not show as rows.
function jumpRangeOf(text: string, items: Item[], next: Selection) {
  const item = next.kind === "about" ? items.find(i => i.item.kind === "about" && !isContainerKey(i.item.key))?.item : next
  return item ? rangeOf(text, item) : null
}

// stepRow is the row by from the chosen one, kept within the rows; the
// first when none is chosen.
function stepRow(order: Row[], selection: Selection | null, by: 1 | -1): Row | undefined {
  const at = order.findIndex(row => sameSelection(selection, row.selection))
  return order[Math.max(0, Math.min(order.length - 1, at < 0 ? 0 : at + by))]
}

// useRowKeys: j and k move through the rows, focusing the row's button.
// Enter on a row is the row button's own click, which opens it.
function useRowKeys(order: Row[], selection: Selection | null, onSelect: (selection: Selection | null) => void) {
  const outline = useRef<HTMLElement>(null)
  const onKeyDown = (event: ReactKeyboardEvent) => {
    if (isTyping(event.target) || event.metaKey || event.ctrlKey || event.altKey) return
    if (event.key !== "j" && event.key !== "k") return
    event.preventDefault()
    const next = stepRow(order, selection, event.key === "j" ? 1 : -1)
    if (!next) return
    onSelect(next.selection)
    requestAnimationFrame(() => outline.current?.querySelector<HTMLElement>(`[data-address="${CSS.escape(addressOf(next.selection))}"]`)?.focus())
  }
  return { outline, onKeyDown }
}

type Options = {
  text: string
  items: Item[]
  order: Row[]
  selection: Selection | null
  onSelect: (selection: Selection | null, how?: { open?: boolean }) => void
  navigating: boolean
  broken: boolean
  cursor?: number
  onJump?: (range: { from: number; to: number }) => void
}

/** useOutlineSelection is which row the outline marks and the inspector
 * shows, following renames; choose picks a row (beside the YAML, it moves
 * the cursor to the row's text), and onKeyDown moves with j and k. */
export function useOutlineSelection({ text, items, order, selection, onSelect, navigating, broken, cursor, onJump }: Options) {
  const shown = shownOf(navigating, text, cursor, selection)
  const { selectedRow } = useFollowRename(order, selection, onSelect, navigating || broken)
  const { outline, onKeyDown } = useRowKeys(order, selection, onSelect)
  const choose = (next: Selection, open = false) => {
    if (!navigating) { onSelect(next, { open }); return }
    const range = jumpRangeOf(text, items, next)
    if (range) onJump?.(range)
  }
  return { shown, selectedRow, choose, outline, onKeyDown }
}
