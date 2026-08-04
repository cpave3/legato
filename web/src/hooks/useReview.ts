import { useQuery, type UseQueryResult } from "@tanstack/react-query"
import { useServer } from "./useServer"
import { fetchReviewQueue, fetchReviewTour, type ReviewQueueItem, type ReviewTourView } from "../lib/review"
import { queryKeys } from "../lib/query"

interface AsyncReviewState<T> {
  data: T | null
  loading: boolean
  error: string | null
  refresh: () => Promise<void>
}

function queryState<T>(query: UseQueryResult<T, Error>): AsyncReviewState<T> {
  return {
    data: query.data ?? null,
    loading: query.isPending,
    error: query.error?.message ?? null,
    refresh: async () => { await query.refetch() },
  }
}

export function useReviewQueue(includeCapturing = false): AsyncReviewState<ReviewQueueItem[]> {
  const { baseUrl } = useServer()
  return queryState(useQuery<ReviewQueueItem[], Error>({
    queryKey: queryKeys.reviewQueue(baseUrl, includeCapturing),
    queryFn: () => fetchReviewQueue(baseUrl, includeCapturing),
  }))
}

export function useReviewTour(tourId: string): AsyncReviewState<ReviewTourView> {
  const { baseUrl } = useServer()
  return queryState(useQuery<ReviewTourView, Error>({
    queryKey: queryKeys.reviewTour(baseUrl, tourId),
    queryFn: () => fetchReviewTour(baseUrl, tourId),
    enabled: Boolean(tourId),
  }))
}
