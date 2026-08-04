import { useQuery } from "@tanstack/react-query"
import { useServer } from "./useServer"
import { fetchBoard } from "../lib/board"
import { queryKeys } from "../lib/query"

export function useBoard(workspace?: string) {
  const { baseUrl } = useServer()
  const query = useQuery({
    queryKey: queryKeys.board(baseUrl, workspace),
    queryFn: () => fetchBoard(baseUrl, workspace),
  })
  return {
    columns: query.data?.columns ?? [],
    workspaces: query.data?.workspaces ?? [],
    loading: query.isPending,
    error: query.error instanceof Error ? query.error.message : null,
    refresh: async () => { await query.refetch() },
  }
}
