import { useEffect, useState } from "react"

// useNow is the time, read again every `interval` milliseconds, for a page
// that says how long ago something was or how long it has run.
export function useNow(interval: number): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), interval)
    return () => window.clearInterval(timer)
  }, [interval])
  return now
}
