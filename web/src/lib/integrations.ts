import { apiFetch } from "./api"

/** A scoped token an integration uses; the token itself is never sent. */
export interface IntegrationToken {
  id: string
  name: string
  scopes: string[]
  created_at: string
  last_used_at?: string
  revoked_at?: string
}

export interface ArgusOverview {
  /** integrations.argus is set in config.yaml */
  configured: boolean
  url?: string
  /** auto: briefs reach agents at once; ask: after you approve each */
  delivery?: string
  agent?: string
  /** Legato's OAuth connection to Argus, which sends agents' replies */
  connection: {
    state: "none" | "connected" | "disconnected" | string
    url?: string
    connected_at?: string
    /** replies by state: pending, sent, failed */
    replies: Record<string, number>
  }
  /** Argus's cases open in Attention */
  open_cases: number
}

export interface IntegrationsOverview {
  tokens: IntegrationToken[]
  argus: ArgusOverview
}

/** What the server has of integrations: its tokens and Argus. */
export async function fetchIntegrations(baseUrl: string): Promise<IntegrationsOverview> {
  const resp = await apiFetch(baseUrl, "/api/settings/integrations")
  if (!resp.ok) {
    let message = `${resp.status} ${resp.statusText}`
    try {
      const body = await resp.json()
      if (body?.error) message = body.error
    } catch { /* not JSON */ }
    throw new Error(message)
  }
  return resp.json()
}
