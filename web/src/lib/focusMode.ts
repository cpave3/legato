import { useSyncExternalStore } from "react"

// Whether the person has focus mode on (components/QuestionFocus.tsx). It
// lives outside the tray because Layout drops the tray while nothing is
// pending, and focus mode outlives that: the next question opens it again.
// For this page only; a reload turns it off.
let on = false
const listeners = new Set<() => void>()

export function setFocusMode(value: boolean): void {
  if (on === value) return
  on = value
  listeners.forEach((listener) => listener())
}

export function useFocusMode(): boolean {
  return useSyncExternalStore(
    (listener) => {
      listeners.add(listener)
      return () => { listeners.delete(listener) }
    },
    () => on,
  )
}
