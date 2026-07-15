import { useCallback, useEffect, useState } from "react"
import { useServer } from "./useServer"
import { useWebSocket } from "./useWebSocket"
import { fetchReviewQueue, fetchReviewTour, type ReviewQueueItem, type ReviewTourView } from "../lib/review"

interface AsyncReviewState<T> {
  data: T | null
  loading: boolean
  error: string | null
  refresh: () => Promise<void>
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

export function useReviewQueue(): AsyncReviewState<ReviewQueueItem[]> {
  const { baseUrl } = useServer()
  const { subscribe } = useWebSocket()
  const [data, setData] = useState<ReviewQueueItem[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    try {
      setError(null)
      setData(await fetchReviewQueue(baseUrl))
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setLoading(false)
    }
  }, [baseUrl])

  useEffect(() => { void refresh() }, [refresh])
  useEffect(() => subscribe((message) => {
    if (message.type === "review_changed") void refresh()
  }), [refresh, subscribe])

  return { data, loading, error, refresh }
}

export function useReviewTour(taskId: string): AsyncReviewState<ReviewTourView> {
  const { baseUrl } = useServer()
  const { subscribe } = useWebSocket()
  const [data, setData] = useState<ReviewTourView | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    if (!taskId) return
    try {
      setError(null)
      setData(await fetchReviewTour(baseUrl, taskId))
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setLoading(false)
    }
  }, [baseUrl, taskId])

  useEffect(() => { void refresh() }, [refresh])
  useEffect(() => subscribe((message) => {
    if (message.type === "review_changed" && message.task_id === taskId) void refresh()
  }), [refresh, subscribe, taskId])

  return { data, loading, error, refresh }
}
