import type { DiffLineKind, DiffSelection } from "./review"

// One line of a review diff: whether it is selected, how a click or a drag
// changes the selection, and how the line is tinted.

// Where a drag across the lines of a hunk began.
export interface DragOrigin {
  filePath: string
  hunkAnchor: string
  lineIndex: number
  // The drag has reached another line, so the click that ends it does not
  // also select.
  moved: boolean
}

// A line's place: the file, the hunk and the line's index in it.
export interface LinePlace {
  path: string
  anchor: string
  index: number
}

function inHunk(selection: DiffSelection | null | undefined, { path, anchor }: LinePlace): selection is DiffSelection {
  return selection?.file_path === path && selection.hunk_anchor === anchor
}

export function isLineSelected(selection: DiffSelection | null | undefined, place: LinePlace): boolean {
  return inHunk(selection, place) && place.index >= selection.start && place.index <= selection.end
}

// A click on a line's gutter grows the selection in its hunk to take the
// line in, or starts a selection of the line alone.
export function clickedSelection(selection: DiffSelection | null | undefined, place: LinePlace): DiffSelection {
  if (inHunk(selection, place)) return { ...selection, start: Math.min(selection.start, place.index), end: Math.max(selection.end, place.index) }
  return { file_path: place.path, hunk_anchor: place.anchor, start: place.index, end: place.index }
}

// A drag that reaches a line of its own hunk selects from where it began to
// that line, and is marked as moved once it has left the first line. A drag
// from another hunk, or none, selects nothing.
export function draggedSelection(origin: DragOrigin | null, place: LinePlace): DiffSelection | null {
  if (!origin || origin.filePath !== place.path || origin.hunkAnchor !== place.anchor) return null
  origin.moved ||= origin.lineIndex !== place.index
  return { file_path: place.path, hunk_anchor: place.anchor, start: Math.min(origin.lineIndex, place.index), end: Math.max(origin.lineIndex, place.index) }
}

const PLAIN_TINT: Record<DiffLineKind, string> = {
  add: "bg-emerald-950/40 text-emerald-200",
  del: "bg-red-950/40 text-red-200",
  ctx: "text-zinc-400",
}

// Inside a noted range, softer, so the range's own colour shows.
const RANGE_TINT: Record<DiffLineKind, string> = {
  add: "range-tinted-add bg-emerald-950/[0.25] text-emerald-100",
  del: "range-tinted-del bg-red-950/[0.25] text-red-100",
  ctx: "bg-amber-950/20 text-amber-100",
}

const SELECTED_TINT = "bg-[var(--theme-selection,color-mix(in_oklab,var(--color-indigo-900)_60%,transparent))] text-[var(--theme-text,var(--color-indigo-100))]"

// The classes that tint a line. A selected line in a block takes the
// block's colour and none of its own.
export function lineTint(kind: DiffLineKind, inRange: boolean, selected: boolean): (string | false)[] {
  if (inRange) return selected ? [] : [RANGE_TINT[kind]]
  return [PLAIN_TINT[kind], selected && SELECTED_TINT]
}

const MARK: Record<DiffLineKind, string> = { add: "+", del: "-", ctx: " " }

export function lineMark(kind: DiffLineKind): string {
  return MARK[kind] ?? " "
}
