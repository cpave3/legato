import { useQuery } from "@tanstack/react-query"
import { useEffect, useState } from "react"
import { fetchReviewBriefing, fetchReviewBriefingPage, type ReviewBriefing, type ReviewBriefingPage, type ReviewBriefingPageContent } from "../lib/reviewBriefing"
import { queryKeys } from "../lib/query"
import { useServer } from "./useServer"

// How often the agent's checkout is read again. Nothing tells Legato when a
// file changes in a checkout, and a briefing is built by a command the agent
// runs, so the list is read again while it is in view.
const RECHECK_MS = 15_000

const NONE: ReviewBriefing = { pages: [] }

// The review briefing on the branch of one agent's checkout. A failure, or
// an agent with no checkout, reads as no briefing: it is a convenience beside
// what the agent showed, and must never stand in its way.
export function useReviewBriefing(taskId: string | null | undefined) {
  const { baseUrl } = useServer()
  const query = useQuery<ReviewBriefing, Error>({
    queryKey: queryKeys.reviewBriefing(baseUrl, taskId ?? ""),
    queryFn: () => fetchReviewBriefing(baseUrl, taskId ?? ""),
    enabled: Boolean(taskId),
    refetchInterval: RECHECK_MS,
    refetchOnWindowFocus: "always",
  })
  return query.data ?? NONE
}

// One page, read again when its version changes.
export function useReviewBriefingPage(taskId: string, path: string | null, version: string) {
  const { baseUrl } = useServer()
  const query = useQuery<ReviewBriefingPageContent, Error>({
    queryKey: queryKeys.reviewBriefingPage(baseUrl, taskId, path ?? "", version),
    queryFn: () => fetchReviewBriefingPage(baseUrl, taskId, path ?? ""),
    enabled: Boolean(path),
  })
  return { page: query.data ?? null, loading: query.isPending && Boolean(path), error: query.error?.message ?? null }
}

type Seen = Record<string, string>

const SEEN_KEY = "legato:review-briefing-seen:"

function storedSeen(taskId: string): Seen {
  try {
    const stored = JSON.parse(localStorage.getItem(SEEN_KEY + taskId) ?? "{}") as unknown
    return stored && typeof stored === "object" ? stored as Seen : {}
  } catch {
    return {}
  }
}

// useUnseenBriefingPages counts the pages of an agent's briefing that
// appeared or were rebuilt since the person last had the panel open on that
// agent, for the Artifacts switch's mark. viewing: the panel is open on it
// now. What was seen is remembered in this browser only.
export function useUnseenBriefingPages(taskId: string | null | undefined, pages: ReviewBriefingPage[], viewing: boolean): number {
  const [state, setState] = useState<{ taskId: string; seen: Seen }>(() => ({ taskId: taskId ?? "", seen: taskId ? storedSeen(taskId) : {} }))
  const current = state.taskId === (taskId ?? "") ? state.seen : taskId ? storedSeen(taskId) : {}
  const unseen = pages.filter((page) => current[page.path] !== page.version).length

  useEffect(() => {
    if (!taskId || !viewing || unseen === 0) return
    const seen = Object.fromEntries(pages.map((page) => [page.path, page.version]))
    try {
      localStorage.setItem(SEEN_KEY + taskId, JSON.stringify(seen))
    } catch {
      // Not remembered: the mark comes back on the next visit, nothing more.
    }
    setState({ taskId, seen })
  }, [taskId, viewing, unseen, pages])

  return unseen
}
