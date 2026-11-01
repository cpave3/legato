import { useEffect, useState } from "react"
import { onShellLeave, onShellReturn } from "../lib/mobileShell"

// How long a page may be hidden before it counts as out of view: a quick
// look at another tab or app should not cost a resubscription.
export const HIDDEN_GRACE_MS = 10_000

// usePageInView says whether the person can see the page. A page hidden for
// longer than the grace period is out of view; one the mobile app has put in
// the background is out of view at once. Views that follow a stream stop
// while it is out of view and catch up when it is back, so a phone with its
// screen off does not keep receiving it.
export function usePageInView(graceMs = HIDDEN_GRACE_MS): boolean {
  const [inView, setInView] = useState(() => !document.hidden)

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | null = null
    const clear = () => {
      if (timer != null) clearTimeout(timer)
      timer = null
    }
    const onVisibility = () => {
      clear()
      if (!document.hidden) {
        setInView(true)
        return
      }
      timer = setTimeout(() => {
        timer = null
        setInView(false)
      }, graceMs)
    }
    document.addEventListener("visibilitychange", onVisibility)
    const stopLeave = onShellLeave(() => {
      clear()
      setInView(false)
    })
    const stopReturn = onShellReturn(() => {
      clear()
      setInView(true)
    })
    return () => {
      clear()
      document.removeEventListener("visibilitychange", onVisibility)
      stopLeave()
      stopReturn()
    }
  }, [graceMs])

  return inView
}
