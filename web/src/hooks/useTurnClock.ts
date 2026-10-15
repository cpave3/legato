import { useEffect, useState } from "react"

// useTurnClock counts the seconds of the turn in progress, from when this
// page saw it start. A turn already running when the page opened has no
// start the page knows of, so it has no count (null). Until `known`, the page
// has not read whether the agent works, and an idle agent is not taken for one.
export function useTurnClock(busy: boolean, known = true): number | null {
  const [seconds, setSeconds] = useState<number | null>(null)
  // Only a turn that starts after the page saw the agent idle is counted.
  const [seenIdle, setSeenIdle] = useState(known && !busy)
  if (known && !busy && !seenIdle) setSeenIdle(true)

  useEffect(() => {
    if (!busy || !seenIdle) return
    const started = Date.now()
    const timer = window.setInterval(() => setSeconds(Math.floor((Date.now() - started) / 1000)), 1000)
    return () => {
      window.clearInterval(timer)
      setSeconds(null)
    }
  }, [busy, seenIdle])

  return busy && seenIdle ? seconds ?? 0 : null
}
