import { apiFetch } from "./api"

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
  // Paired instances' Reeve change sets waiting for their person.
  peer_approvals?: number
  // What the needs-you rail shows (its view of Attention); absent from an
  // older server.
  rail?: number
}

export async function fetchNavigationSummary(baseUrl: string): Promise<NavigationSummary> {
  const response = await apiFetch(baseUrl, "/api/navigation-summary")
  if (!response.ok) throw new Error(await response.text())
  return response.json()
}
