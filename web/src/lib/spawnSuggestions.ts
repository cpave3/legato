import {
  completionOf,
  presetFor,
  suggestionsWhile,
  type DirectoryPreset,
  type DirectoryPresets,
  type DirectorySuggestion,
  type DirectorySuggestions,
} from "./spawnDirectories"

// The spawn dialog's path field, the parts that are worked out rather than
// kept: what it offers to start in, what is listed under the path, which
// suggestion the keys are on, what a key does, and where the list floats.

// The suggestion list: its distance from the path field and from the edge of
// the window, how tall it may get, and the room below the field under which
// it opens upward when there is more room there.
const listGap = 6
const listMargin = 8
const listMaxHeight = 264
const listMinRoom = 132

// On a phone the presets past this many wait behind a row.
export const presetsShownOnPhone = 3

// How long the path rests before its suggestions are asked for.
export const suggestDelayMs = 120

// What the path field keeps between keys: the last answer, whether its
// list is open, the suggestion the keys are on, and whether the arrow keys
// chose it, which enter then takes.
export interface PathFieldState {
  found: DirectorySuggestions | null
  listOpen: boolean
  active: number
  chosen: boolean
}

export const initialPathField: PathFieldState = { found: null, listOpen: false, active: 0, chosen: false }

export type PathFieldEvent =
  // The dialog opened afresh.
  | { type: "reset" }
  // The path changed, by typing or by taking a suggestion.
  | { type: "edit" }
  | { type: "answer"; found: DirectorySuggestions }
  | { type: "open" }
  | { type: "close" }
  // The arrow keys moved to a suggestion.
  | { type: "move"; to: number }

export function pathFieldReducer(state: PathFieldState, event: PathFieldEvent): PathFieldState {
  switch (event.type) {
    case "reset":
      return { ...state, found: null, listOpen: false }
    case "edit":
      return { ...state, active: 0, chosen: false, listOpen: true }
    case "answer":
      return { ...state, found: event.found, active: 0, chosen: false }
    case "open":
      return { ...state, listOpen: true }
    case "close":
      return { ...state, listOpen: false }
    case "move":
      return { ...state, active: event.to, chosen: true }
  }
}

// The suggestion one step along from current, staying on the list.
export function stepCursor(current: number, step: number, length: number): number {
  return Math.min(Math.max(current + step, 0), length - 1)
}

// What the field starts with when the dialog opens: the presets offered,
// the path, and whether the server completes paths at all.
export interface DirectoryStart {
  presets: DirectoryPreset[]
  path: string
  // False for a server with no directory service: its presets are the
  // configured repositories, and a path is typed without help.
  serverKnowsDirectories: boolean
  // A start that is no preset is a custom path, so the phone shows it.
  showAll: boolean
}

// The settings it falls back on: the configured repositories with a folder,
// and the server's working directory.
export interface DirectorySettings {
  working_dir?: string
  repositories?: { name: string; primary_dir: string }[]
}

export function directoryStart(listed: DirectoryPresets | null, configured: DirectorySettings | undefined, initialWorkingDir: string): DirectoryStart {
  const presets = listed?.presets ?? (configured?.repositories ?? [])
    .filter((repository) => repository.primary_dir)
    .map((repository) => ({ name: repository.name, path: repository.primary_dir, display: repository.primary_dir, saved: false }))
  const path = initialWorkingDir || (listed?.default.display ?? configured?.working_dir ?? "")
  return { presets, path, serverKnowsDirectories: listed !== null, showAll: !presetFor(path, presets) }
}

// An answer with no suggestions for path, which a failed request stands for.
export function noSuggestions(path: string): DirectorySuggestions {
  return { query: path, resolved: "", exists: false, base: "", prefix: "", items: [], total: 0 }
}

// What is listed under the path, and what of it the keys can take. The
// last answer stays up, dimmed, while the one for another directory is on
// its way; see suggestionsWhile.
export interface SuggestionView {
  held: boolean
  // The directory the answer lists, as typed, and the part of a name typed.
  base: string
  prefix: string
  shown: DirectorySuggestion[]
  takeable: DirectorySuggestion[]
  // True when there are more than are shown, so typing on narrows them.
  more: boolean
  // Stands for shown: the same suggestions are the same height.
  shownKey: string
}

export function suggestionView(found: DirectorySuggestions | null, path: string, suggests: boolean): SuggestionView {
  const listed = suggestionsWhile(found, path)
  const shown = suggests ? listed.items : []
  const more = found !== null && !listed.held && found.query === path && found.total > shown.length
  return {
    held: listed.held,
    base: found?.base ?? "",
    prefix: listed.prefix,
    shown,
    takeable: listed.held ? [] : shown,
    more,
    shownKey: shown.map((item) => item.path).join("\n") + (more ? "\n+" : ""),
  }
}

// The suggestion the keys are on (-1 for none), and what taking it would
// add to the path. Under a path that ends in a slash the suggestions are
// what is inside the directory, and none of them is highlighted until the
// arrow keys pick one.
export function suggestionCursor(path: string, takeable: DirectorySuggestion[], active: number, chosen: boolean): { current: number; ghost: string } {
  const unpicked = path.endsWith("/") && !chosen
  const current = takeable.length > 0 && !unpicked ? Math.min(Math.max(active, 0), takeable.length - 1) : -1
  return { current, ghost: completionOf(path, takeable[current]) }
}

// The keys the path field handles itself: escape closes the list, the
// arrows move along it, tab completes the path with the suggestion the
// keys are on (else the first), and enter takes one the arrows chose.
export type PathKeyAction = "close" | "up" | "down" | "complete" | "take"

interface PathKey {
  key: string
  shiftKey: boolean
  metaKey: boolean
  ctrlKey: boolean
}

const arrows = new Map<string, PathKeyAction>([["ArrowDown", "down"], ["ArrowUp", "up"]])

// Tab, or enter on a suggestion the arrows chose; not the spawn chord.
function completionKey(event: PathKey, chosen: boolean): PathKeyAction | null {
  if (event.key === "Tab") return event.shiftKey ? null : "complete"
  return event.key === "Enter" && chosen && !event.metaKey && !event.ctrlKey ? "take" : null
}

export function pathKeyAction(event: PathKey, listShown: boolean, suggesting: boolean, chosen: boolean): PathKeyAction | null {
  if (event.key === "Escape") return listShown ? "close" : null
  if (!suggesting) return null
  return arrows.get(event.key) ?? completionKey(event, chosen)
}

// Where the list floats: under the path field, or over it when the window
// ends too soon below; room is how tall it may be there.
export interface ListPlace {
  left: number
  width: number
  top?: number
  bottom?: number
  room: number
}

interface Box {
  left: number
  width: number
  top: number
  bottom: number
}

// viewTop and viewHeight are the visual viewport's, which on a phone is
// what the keyboard leaves; windowHeight is the layout viewport's.
export function listPlacement(field: Box, viewTop: number, viewHeight: number, windowHeight: number): ListPlace {
  const below = viewTop + viewHeight - field.bottom - listGap - listMargin
  const above = field.top - viewTop - listGap - listMargin
  const opensUp = below < listMinRoom && above > below
  return {
    left: field.left,
    width: field.width,
    room: Math.max(Math.min(listMaxHeight, opensUp ? above : below), 0),
    ...(opensUp ? { bottom: windowHeight - field.top + listGap } : { top: field.bottom + listGap }),
  }
}

// The frame's place and height: no height while nothing is listed, so that
// it eases from one answer to the next. The border adds 2.
export function listFrameStyle(place: ListPlace | null, listing: boolean, listHeight: number) {
  return {
    position: "fixed" as const,
    left: place?.left ?? 0,
    width: place?.width ?? 0,
    top: place?.top,
    bottom: place?.bottom,
    height: listing ? Math.min(listHeight, place?.room ?? 0) + 2 : 0,
  }
}

// What a suggestion is besides a folder: "git", "preset", or both; empty
// for a plain folder.
export function suggestionMarks(item: DirectorySuggestion): string {
  return [item.git && "git", item.preset > 0 && "preset"].filter(Boolean).join(" · ")
}

// How many presets a phone keeps behind the "more" row.
export function presetsHiddenOnPhone(presets: DirectoryPreset[], selected: DirectoryPreset | undefined, showAll: boolean): number {
  if (showAll) return 0
  return presets.filter((preset, index) => index >= presetsShownOnPhone && preset !== selected).length
}
