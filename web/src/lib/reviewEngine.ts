import { useEffect, useState } from "react"
import { apiFetch } from "./api"

// Whether some Legato on the server's data folder runs the review engine,
// and why not when none does: the card menu offers review passes only while
// one runs (spec LEG-559 D9).
export interface ReviewEngineStatus {
  running: boolean
  reason?: string
}

export async function fetchReviewEngine(baseUrl: string): Promise<ReviewEngineStatus> {
  const response = await apiFetch(baseUrl, "/api/review-engine")
  if (!response.ok) throw new Error(`review engine: ${response.status}`)
  const body = await response.json() as Partial<ReviewEngineStatus>
  return { running: body.running === true, reason: body.reason }
}

// useReviewEngine reads the engine's status each time a menu opens (opened
// is the open menu, a new object each time, or null), so a holder started
// or stopped since shows; null until it is read. A server that cannot say (an
// older Legato) offers review as it always did.
export function useReviewEngine(baseUrl: string, opened: object | null): ReviewEngineStatus | null {
  const [status, setStatus] = useState<ReviewEngineStatus | null>(null)
  useEffect(() => {
    if (opened === null) return
    let current = true
    setStatus(null)
    fetchReviewEngine(baseUrl).then(next => { if (current) setStatus(next) }, () => { if (current) setStatus({ running: true }) })
    return () => { current = false }
  }, [baseUrl, opened])
  return status
}
