import { useQuery } from "@tanstack/react-query"
import { fetchCard } from "../lib/board"
import type { CardDetail } from "../lib/board-types"
import { queryKeys } from "../lib/query"
import { useServer } from "./useServer"

// The card of one task: its column and its pull request, for the agent that
// works on it. Kept current by `cards_changed`.
export function useTaskCard(taskId: string | null | undefined): CardDetail | null {
  const { baseUrl } = useServer()
  const query = useQuery<CardDetail, Error>({
    queryKey: queryKeys.task(baseUrl, taskId ?? ""),
    queryFn: () => fetchCard(baseUrl, taskId ?? ""),
    enabled: Boolean(taskId),
  })
  return query.data ?? null
}
