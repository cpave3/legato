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
}

export async function fetchNavigationSummary(baseUrl: string): Promise<NavigationSummary> {
  const response = await apiFetch(baseUrl, "/api/navigation-summary")
  if (!response.ok) throw new Error(await response.text())
  return response.json()
}

export function useNavigationSummary() {
  const { baseUrl } = useServer()
  return useQuery({
    queryKey: queryKeys.navigationSummary(baseUrl),
    queryFn: () => fetchNavigationSummary(baseUrl),
  })
}
