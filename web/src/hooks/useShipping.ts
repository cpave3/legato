import { useQuery } from "@tanstack/react-query"
import { fetchShipping, type ShippingLoop } from "../lib/shipping"
import { queryKeys } from "../lib/query"
import { useServer } from "./useServer"

// useShipping is every review loop Legato looks after (GET /api/shipping).
// Push messages about runs, Attention and agents read it again (querySync).
export function useShipping() {
  const { baseUrl } = useServer()
  const query = useQuery<{ loops: ShippingLoop[] }, Error>({
    queryKey: queryKeys.shipping(baseUrl),
    queryFn: () => fetchShipping(baseUrl),
  })
  return {
    data: query.data ?? null,
    loading: query.isPending,
    error: query.error?.message ?? null,
    refresh: async () => { await query.refetch() },
  }
}
