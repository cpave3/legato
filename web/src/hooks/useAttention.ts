import { useQuery } from "@tanstack/react-query"
import { fetchAttention, type AttentionItem } from "../lib/attention"
import { queryKeys } from "../lib/query"
import { useServer } from "./useServer"

export function useAttention() {
  const { baseUrl } = useServer()
  const query = useQuery<AttentionItem[], Error>({
    queryKey: queryKeys.attention(baseUrl),
    queryFn: () => fetchAttention(baseUrl),
  })
  return {
    data: query.data ?? null,
    loading: query.isPending,
    error: query.error?.message ?? null,
    refresh: async () => { await query.refetch() },
  }
}
