import { useSyncExternalStore } from "react"
import { readRailFolded, writeRailFolded } from "./needsYou"

// The needs-you rail's state that other parts of the page follow: whether
// it is folded (kept in this browser), and the approval the page was asked
// to show (?approval=, a notification), which the rail or the phone's tray
// opens on.
// present: the rail is on the page (from md up the layout draws it).
// opens counts the requests to show the rail's content, so the phone's
// sheet opens on each, the same approval asked for again included.
type RailState = { folded: boolean; wanted: string | null; present: boolean; opens: number }

let state: RailState = { folded: readRailFolded(), wanted: null, present: false, opens: 0 }
const listeners = new Set<() => void>()

function set(next: Partial<RailState>) {
  state = { ...state, ...next }
  listeners.forEach((listener) => listener())
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}

export function useRailState(): RailState {
  return useSyncExternalStore(subscribe, () => state, () => state)
}

export function setRailFolded(folded: boolean) {
  writeRailFolded(folded)
  set({ folded })
}

// showApproval opens the rail on an approval.
export function showApproval(id: string) {
  writeRailFolded(false)
  set({ folded: false, wanted: id, opens: state.opens + 1 })
}

// openNeedsYou shows what waits on the operator: the rail unfolded from md
// up, the sheet opened on a phone.
export function openNeedsYou() {
  writeRailFolded(false)
  set({ folded: false, opens: state.opens + 1 })
}

// setRailPresent says the rail is mounted, so what sits on the right edge
// moves left of it.
export function setRailPresent(present: boolean) {
  if (state.present !== present) set({ present })
}

// railEdgeStyle is the style that keeps a toast or banner on the right edge
// clear of the rail from md up, as the class md:right-[var(--lw-rail-offset)].
export function railEdgeStyle(rail: RailState): Record<string, string> {
  return { "--lw-rail-offset": rail.present ? railOffset(rail.folded) : "1rem" }
}

// getRailStateForTests reads the state outside React, for tests.
export function getRailStateForTests(): RailState {
  return state
}

// resetRailState is for tests: the state as a fresh page has it.
export function resetRailState() {
  state = { folded: readRailFolded(), wanted: null, present: false, opens: 0 }
  listeners.forEach((listener) => listener())
}

// The open and folded widths of the rail, and the room a toast or banner on
// the right edge leaves for it from md up.
export const RAIL_OPEN = 320
export const RAIL_FOLDED = 52

export function railOffset(folded: boolean): string {
  return `${(folded ? RAIL_FOLDED : RAIL_OPEN) + 16}px`
}
