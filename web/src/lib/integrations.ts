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

/** Burnlite, a ticket provider Legato reads tasks from once connected. */
export interface BurnliteOverview {
  /** burnlite.url in config.yaml, which the address field starts from */
  configured_url?: string
  /** the project keys import searches; [] searches every project */
  projects: string[]
  /** burnlite.projects is set in config.yaml, so they cannot be changed here */
  projects_from_config: boolean
  /** Legato's OAuth connection to Burnlite */
  connection: {
    state: "none" | "connected" | "disconnected" | string
    url?: string
    connected_at?: string
  }
}

export interface IntegrationsOverview {
  tokens: IntegrationToken[]
  argus: ArgusOverview
  /** absent from a server without Burnlite */
  burnlite?: BurnliteOverview
}

// The server's {error} for a refusal, else its status.
async function ok(resp: Response): Promise<Response> {
  if (resp.ok) return resp
  let message = `${resp.status} ${resp.statusText}`.trim()
  try {
    const body = await resp.json()
    if (body?.error) message = body.error
  } catch { /* not JSON */ }
  throw new Error(message)
}

/** Starts connecting to the Burnlite at url; returns where the person consents. */
export async function connectBurnlite(baseUrl: string, url: string): Promise<string> {
  const resp = await ok(await apiFetch(baseUrl, "/api/settings/integrations/burnlite/connect", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ url }),
  }))
  const body = await resp.json()
  return body.authorize_url
}

/** Saves the project keys import searches ([] for every project). */
export async function saveBurnliteProjects(baseUrl: string, projects: string[]): Promise<BurnliteOverview> {
  const resp = await ok(await apiFetch(baseUrl, "/api/settings/integrations/burnlite", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ projects }),
  }))
  return resp.json()
}

/** Forgets Legato's connection to Burnlite. */
export async function disconnectBurnlite(baseUrl: string): Promise<void> {
  await ok(await apiFetch(baseUrl, "/api/settings/integrations/burnlite", { method: "DELETE" }))
}

/** Sends this window to url, as connecting to a service needs. */
export function leaveFor(url: string): void {
  window.location.assign(url)
}

/** Burnlite project keys from what the person typed: split on commas and spaces, upper case. */
export function parseProjectKeys(text: string): string[] {
  return text.split(/[\s,]+/).map((key) => key.trim().toUpperCase()).filter(Boolean)
}

/** What the server has of integrations: its tokens, Argus and Burnlite. */
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
