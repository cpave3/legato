import { useQuery } from "@tanstack/react-query"
import { fetchPendingApprovals, type Approval } from "../lib/approvals"
import { queryKeys } from "../lib/query"
import { useServer } from "./useServer"

/** The approvals waiting for the person, oldest first. approvals_changed refreshes them. */
export function useApprovals() {
  const { baseUrl } = useServer()
  const query = useQuery<Approval[], Error>({
    queryKey: queryKeys.approvals(baseUrl),
    queryFn: () => fetchPendingApprovals(baseUrl),
  })
  return {
    data: query.data ?? [],
    error: query.error?.message ?? null,
    refresh: async () => { await query.refetch() },
  }
}
