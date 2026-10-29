import { useQuery } from "@tanstack/react-query"
import { apiFetch } from "../lib/api"
import { queryKeys } from "../lib/query"
import { useServer } from "./useServer"

export interface NavigationSummary {
  attention: number
  review: number
  plans: number
  workflow_attention: number
  agents: number
  // Learnings waiting for a decision; absent from an older server.
  learning?: number
  // Approvals waiting for the person; absent from an older server.
  approvals?: number
}

export async function fetchNavigationSummary(baseUrl: string): Promise<NavigationSummary> {
  const response = await apiFetch(baseUrl, "/api/navigation-summary")
  if (!response.ok) throw new Error(await response.text())
  return response.json()
}

const NAVIGATION_SUMMARY_REFETCH_INTERVAL_MS = 60_000

export function useNavigationSummary() {
  const { baseUrl } = useServer()
  return useQuery({
    queryKey: queryKeys.navigationSummary(baseUrl),
    queryFn: () => fetchNavigationSummary(baseUrl),
    refetchInterval: NAVIGATION_SUMMARY_REFETCH_INTERVAL_MS,
  })
}
