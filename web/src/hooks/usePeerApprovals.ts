import { useQuery } from "@tanstack/react-query"
import { fetchPeerApprovals, type PeerApproval } from "../lib/reeve"
import { queryKeys } from "../lib/query"
import { useServer } from "./useServer"

/** Paired instances' Reeve change sets waiting for their person, as the server last heard; approvals_changed refreshes them. */
export function usePeerApprovals(): PeerApproval[] {
  const { baseUrl } = useServer()
  const query = useQuery<PeerApproval[], Error>({
    queryKey: queryKeys.peerApprovals(baseUrl),
    queryFn: () => fetchPeerApprovals(baseUrl),
    retry: false,
  })
  return query.data ?? []
}
