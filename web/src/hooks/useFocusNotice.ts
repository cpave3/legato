import { createContext, useContext, useEffect } from "react"
import { useApprovals } from "./useApprovals"
import { useFocusMode } from "../lib/focusMode"

// Where the "Focus is on" notice shows (components/FocusNotice.tsx): docked by
// the layout, unless a page claims it for its own header.

export interface FocusNoticeSlot {
  claimed: boolean
  claim: () => () => void
}

export const FocusNoticeContext = createContext<FocusNoticeSlot>({ claimed: false, claim: () => () => {} })

// useClaimFocusNotice says the calling page shows the notice in its own
// header for as long as it is mounted.
export function useClaimFocusNotice(): void {
  const { claim } = useContext(FocusNoticeContext)
  // eslint-disable-next-line react-hooks/exhaustive-deps -- claim once, for as long as the page is mounted.
  useEffect(() => claim(), [])
}

// useFocusStandby is whether the notice should show: focus mode is on and no
// approval waits (focus itself shows a question; the rail, the rest).
export function useFocusStandby(): boolean {
  const focusMode = useFocusMode()
  const { data: approvals } = useApprovals(focusMode)
  return focusMode && approvals.length === 0
}


// useFocusNoticeSlot is whether a page shows the notice itself.
export function useFocusNoticeSlot(): FocusNoticeSlot {
  return useContext(FocusNoticeContext)
}
