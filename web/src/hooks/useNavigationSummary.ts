import { useQuery } from "@tanstack/react-query"
import { queryKeys } from "../lib/query"
import { useServer } from "./useServer"
import { fetchNavigationSummary } from "../lib/navigationSummary"

const NAVIGATION_SUMMARY_REFETCH_INTERVAL_MS = 60_000

export function useNavigationSummary() {
  const { baseUrl } = useServer()
  return useQuery({
    queryKey: queryKeys.navigationSummary(baseUrl),
    queryFn: () => fetchNavigationSummary(baseUrl),
    refetchInterval: NAVIGATION_SUMMARY_REFETCH_INTERVAL_MS,
  })
}
