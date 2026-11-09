import { apiFetch } from "./api"

// restartLegato asks every Legato process of the server's instance to
// restart in place (POST /api/restart). The connection drops for a moment.
export async function restartLegato(baseUrl: string): Promise<void> {
  const response = await apiFetch(baseUrl, "/api/restart", { method: "POST" })
  if (!response.ok) {
    const body = await response.json().catch(() => null) as { error?: string } | null
    throw new Error(body?.error || response.statusText)
  }
}
