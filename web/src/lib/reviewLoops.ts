import { useEffect, useState } from "react"
import { apiFetch } from "./api"

// The review loop that holds a pull request, whichever caller opened it (a
// workflow run, a child run, a handoff), as GET /api/review-loops gives it
// (spec LEG-559 § API contract).
export interface FoundReviewLoop {
  loop_id: string
  kind: "run" | "child_run" | "handoff"
  run_id: string
  step_path: string
  phase: string
  state: string
  held: boolean
  outcome: string
  // Where the web UI shows the loop: its run's page, or the handed-off card.
  link: string
}

// What a launcher needs of a pull request: the loop that holds it, or, with
// none, whether a review started now treats it as an agent's (true only
// when Legato's author App opened it, D8).
export interface ReviewLoopLookup {
  loop: FoundReviewLoop | null
  agent_author: boolean
}

export const reviewLoopKey = (pr: { repo: string; pr_number: number }) => `${pr.repo}#${pr.pr_number}`

export async function lookupReviewLoop(baseUrl: string, repo: string, pr: number): Promise<ReviewLoopLookup> {
  const query = new URLSearchParams({ repo, pr: String(pr) })
  const response = await apiFetch(baseUrl, `/api/review-loops?${query}`)
  if (!response.ok) throw new Error(`review loops: ${response.status}`)
  const body = await response.json() as Partial<ReviewLoopLookup>
  return { loop: body.loop ?? null, agent_author: body.agent_author === true }
}

// useReviewLoops looks each pull request up as a menu opens (opened is the
// open menu, a new object each time, or null): by reviewLoopKey, null until
// every one is read. One that cannot be read is left out.
export function useReviewLoops(baseUrl: string, prs: { repo: string; pr_number: number }[], opened: object | null): Record<string, ReviewLoopLookup> | null {
  const [found, setFound] = useState<Record<string, ReviewLoopLookup> | null>(null)
  const keys = prs.map(reviewLoopKey).join(",")
  useEffect(() => {
    if (opened === null) return
    let current = true
    setFound(null)
    const wanted = keys === "" ? [] : keys.split(",")
    Promise.all(wanted.map(async key => {
      const at = key.lastIndexOf("#")
      try {
        return [key, await lookupReviewLoop(baseUrl, key.slice(0, at), Number(key.slice(at + 1)))] as const
      } catch {
        return null
      }
    })).then(results => {
      if (current) setFound(Object.fromEntries(results.filter(result => result !== null)))
    })
    return () => { current = false }
  }, [baseUrl, keys, opened])
  return found
}
