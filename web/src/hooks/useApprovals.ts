import { useQuery } from "@tanstack/react-query"
import { fetchPendingApprovals, type Approval } from "../lib/approvals"
import { queryKeys } from "../lib/query"
import { useServer } from "./useServer"

// NONE is the list before any has loaded: the same array every render, so
// what is derived from it is not remade each time.
const NONE: Approval[] = []

/** The approvals waiting for the person, oldest first. approvals_changed refreshes them. enabled false asks nothing (the navigation counts say none wait). */
export function useApprovals(enabled = true) {
  const { baseUrl } = useServer()
  const query = useQuery<Approval[], Error>({
    queryKey: queryKeys.approvals(baseUrl),
    queryFn: () => fetchPendingApprovals(baseUrl),
    enabled,
  })
  return {
    data: query.data ?? NONE,
    error: query.error?.message ?? null,
    refresh: async () => { await query.refetch() },
  }
}
