import { useCallback, useEffect, useRef, useState } from "react"
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

export function useReviewQueue(includeCapturing = false): AsyncReviewState<ReviewQueueItem[]> {
  const { baseUrl } = useServer()
  const { subscribe } = useWebSocket()
  const [data, setData] = useState<ReviewQueueItem[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const requestIdRef = useRef(0)
  const refreshTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const refresh = useCallback(async () => {
    if (refreshTimeoutRef.current !== null) {
      clearTimeout(refreshTimeoutRef.current)
      refreshTimeoutRef.current = null
    }
    const requestId = ++requestIdRef.current
    try {
      setError(null)
      const next = await fetchReviewQueue(baseUrl, includeCapturing)
      if (requestId === requestIdRef.current) setData(next)
    } catch (cause) {
      if (requestId === requestIdRef.current) setError(errorMessage(cause))
    } finally {
      if (requestId === requestIdRef.current) setLoading(false)
    }
  }, [baseUrl, includeCapturing])

  useEffect(() => { void refresh() }, [refresh])
  useEffect(() => {
    const unsubscribe = subscribe((message) => {
      if (message.type !== "review_changed") return
      if (refreshTimeoutRef.current !== null) clearTimeout(refreshTimeoutRef.current)
      refreshTimeoutRef.current = setTimeout(() => {
        refreshTimeoutRef.current = null
        void refresh()
      }, 50)
    })
    return () => {
      unsubscribe()
      if (refreshTimeoutRef.current !== null) clearTimeout(refreshTimeoutRef.current)
    }
  }, [refresh, subscribe])

  return { data, loading, error, refresh }
}

export function useReviewTour(tourId: string): AsyncReviewState<ReviewTourView> {
  const { baseUrl } = useServer()
  const { subscribe } = useWebSocket()
  const [data, setData] = useState<ReviewTourView | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const requestIdRef = useRef(0)

  const refresh = useCallback(async () => {
    if (!tourId) return
    const requestId = ++requestIdRef.current
    try {
      setError(null)
      const nextData = await fetchReviewTour(baseUrl, tourId)
      if (requestId === requestIdRef.current) setData(nextData)
    } catch (cause) {
      if (requestId === requestIdRef.current) setError(errorMessage(cause))
    } finally {
      if (requestId === requestIdRef.current) setLoading(false)
    }
  }, [baseUrl, tourId])

  useEffect(() => { void refresh() }, [refresh])
  useEffect(() => subscribe((message) => {
    if (message.type === "review_changed" && message.tour_id === tourId) void refresh()
  }), [refresh, subscribe, tourId])

  return { data, loading, error, refresh }
}
