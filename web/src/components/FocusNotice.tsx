import { useMemo, useState, type ReactNode } from "react"
import { FocusNoticeContext, useFocusNoticeSlot, useFocusStandby, type FocusNoticeSlot } from "../hooks/useFocusNotice"
import { setFocusMode } from "../lib/focusMode"
import { cn } from "../lib/utils"

// The "Focus is on" notice: focus mode is on and nothing is asked, so the
// full-screen focus (QuestionFocus) waits for the next question. It is part
// of the page, never over it: the layout docks it at the top of the main
// area, and a page with a header of its own (the ensemble) claims it and
// shows it there as a compact pill.

// FocusNoticeProvider keeps whether a page shows the notice itself.
export function FocusNoticeProvider({ children }: { children: ReactNode }) {
  const [claims, setClaims] = useState(0)
  const value = useMemo<FocusNoticeSlot>(() => ({
    claimed: claims > 0,
    claim: () => {
      setClaims((count) => count + 1)
      return () => setClaims((count) => count - 1)
    },
  }), [claims])
  return <FocusNoticeContext.Provider value={value}>{children}</FocusNoticeContext.Provider>
}

// FocusStandby is the notice docked in flow at the top of the main area,
// unless the page in view shows it itself.
export function FocusStandby() {
  const standby = useFocusStandby()
  const { claimed } = useFocusNoticeSlot()
  if (!standby || claimed) return null
  return <div className="px-3 pt-2"><FocusNotice /></div>
}

// FocusNotice says focus is on, with the way out: a line with its
// explanation, or compact, a pill for a page's header.
export function FocusNotice({ compact = false }: { compact?: boolean }) {
  const leave = () => setFocusMode(false)
  if (compact) {
    return (
      <div role="status" className="flex h-8 items-center gap-2 rounded-full border border-lw-accent-ring bg-lw-surface pl-3 pr-1 text-xs text-lw-text max-md:h-11">
        <span aria-hidden className="size-2 shrink-0 rounded-full bg-lw-link" />
        <span className="shrink-0">Focus is on</span>
        <button type="button" aria-label="Leave focus" onClick={leave} className="h-6 rounded-full px-2 text-lw-link hover:bg-lw-raised max-md:min-h-11">Leave</button>
      </div>
    )
  }
  return (
    <div role="status" className={cn("flex items-center gap-3 rounded-xl border border-lw-accent-ring bg-lw-surface py-2 pl-4 pr-2 font-lw text-sm text-lw-text")}>
      <span aria-hidden className="h-2 w-2 shrink-0 rounded-full bg-lw-link" />
      <span className="min-w-0 flex-1">Focus is on. It opens again when a question comes in.</span>
      <button type="button" onClick={leave} className="min-h-11 shrink-0 rounded-lg border border-lw-line-strong px-3 hover:bg-lw-raised md:min-h-9">Leave focus</button>
    </div>
  )
}
