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

export function useReviewTour(tourId: string): AsyncReviewState<ReviewTourView> {
  const { baseUrl } = useServer()
  const { subscribe } = useWebSocket()
  const [data, setData] = useState<ReviewTourView | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    if (!tourId) return
    try {
      setError(null)
      setData(await fetchReviewTour(baseUrl, tourId))
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setLoading(false)
    }
  }, [baseUrl, tourId])

  useEffect(() => { void refresh() }, [refresh])
  useEffect(() => subscribe((message) => {
    if (message.type === "review_changed" && message.tour_id === tourId) void refresh()
  }), [refresh, subscribe, tourId])

  return { data, loading, error, refresh }
}
