import { apiFetch } from "./api"

// The agent tools this Legato can start, and the one it starts when none
// is named (GET /api/adapters).
export interface Adapters {
  adapters: string[]
  default: string
  accounts?: { name: string; label: string; provider: string }[]
}

export async function fetchAdapters(baseUrl: string): Promise<Adapters> {
  const response = await apiFetch(baseUrl, "/api/adapters")
  if (!response.ok) return { adapters: [], default: "" }
  const body = await response.json() as Partial<Adapters>
  return { adapters: body.adapters ?? [], default: body.default ?? "", accounts: body.accounts ?? [] }
}
