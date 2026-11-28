import { useSyncExternalStore } from "react"

// Whether the person has focus mode on (components/QuestionFocus.tsx). It
// lives outside the tray because Layout drops the tray while nothing is
// pending, and focus mode outlives that: the next question opens it again.
// For this page only; a reload turns it off.
let on = false
// The approval focus mode opens at, when a question's own Focus turned it
// on; null opens at the oldest question.
let start: string | null = null
const listeners = new Set<() => void>()

// setFocusMode turns focus mode on or off; turned on with an approval's ID,
// it opens at that approval's questions first.
export function setFocusMode(value: boolean, startAt: string | null = null): void {
  const nextStart = value ? startAt : null
  if (on === value && start === nextStart) return
  on = value
  start = nextStart
  listeners.forEach((listener) => listener())
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}

export function useFocusMode(): boolean {
  return useSyncExternalStore(subscribe, () => on)
}

// useFocusStart is the approval focus mode opens at, or null.
export function useFocusStart(): string | null {
  return useSyncExternalStore(subscribe, () => start)
}

// getFocusMode is whether focus mode is on now, outside React.
export function getFocusMode(): boolean {
  return on
}

// getFocusStart is the approval focus mode opens at now, outside React.
export function getFocusStart(): string | null {
  return start
}
