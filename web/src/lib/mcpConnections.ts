import { apiFetch } from "./api"

/** Which agents may use which of a connection's tools, and how a use is approved. */
export interface MCPRule {
  /** a pattern of tool names */
  tools: string
  /** owner/name pattern of the repository the agent works in; "-/-" for none */
  repository: string
  /** read, write, or "" for both */
  access?: string
  /** auto, session, window, request or deny */
  approve: string
  window?: string
}

export interface MCPProblem {
  code: string
  message: string
}

/** An upstream MCP server the operator added, as the server keeps it. */
export interface MCPConnection {
  name: string
  url: string
  issuer?: string
  auth: "oauth" | "none" | string
  enabled: boolean
  revision: number
  private_networks?: string[]
  rules: MCPRule[]
  read_only_tools?: string[]
  /** not_required, required, pending, awaiting_confirmation, authorized, reauth_required */
  authorization: string
  /** the unfinished authorization attempt, for a page reloaded mid-way */
  attempt?: string
  /** unknown, connected, failed */
  connection: string
  last_contact_at?: string
  problem?: MCPProblem
}

export interface MCPConnectionInput {
  name: string
  url: string
  auth: "oauth" | "none"
  issuer?: string
  private_networks?: string[]
}

export interface MCPConnectionChange {
  enabled?: boolean
  rules?: MCPRule[]
  read_only_tools?: string[]
  expected_revision: number
}

/** What the operator is asked to confirm: never a token. */
export interface OAuthEvidence {
  issuer: string
  resource: string
  requested_scope?: string
  granted_scope?: string
  refreshable: boolean
  expires_at?: string
  account?: string
}

export interface OAuthAttempt {
  id: string
  connection: string
  /** pending, exchanging, awaiting_confirmation, authorized, denied, failed, expired, cancelled, interrupted, superseded */
  status: string
  scope?: string
  error_code?: string
  /** Unix seconds */
  expires_at: number
  evidence?: OAuthEvidence
}

export interface OAuthBegin {
  id: string
  authorize_url: string
  expires_at: number
}

const settings = "/api/settings/mcp/connections"
const json = { "Content-Type": "application/json" }

async function ok(response: Response): Promise<Response> {
  if (response.ok) return response
  let message = response.statusText
  try {
    const body = await response.json()
    if (body && typeof body.error === "string") message = body.error
  } catch {
    // Not JSON; the status says enough.
  }
  throw new Error(message || `request failed (${response.status})`)
}

const named = (name: string) => `${settings}/${encodeURIComponent(name)}`

export async function listMCPConnections(baseUrl: string): Promise<MCPConnection[]> {
  const body = await (await ok(await apiFetch(baseUrl, settings))).json()
  return Array.isArray(body?.connections) ? body.connections : []
}

export async function getMCPConnection(baseUrl: string, name: string): Promise<MCPConnection> {
  return (await ok(await apiFetch(baseUrl, named(name)))).json()
}

export async function addMCPConnection(baseUrl: string, input: MCPConnectionInput): Promise<MCPConnection> {
  return (await ok(await apiFetch(baseUrl, settings, { method: "POST", headers: json, body: JSON.stringify(input) }))).json()
}

export async function changeMCPConnection(baseUrl: string, name: string, change: MCPConnectionChange): Promise<MCPConnection> {
  return (await ok(await apiFetch(baseUrl, named(name), { method: "PATCH", headers: json, body: JSON.stringify(change) }))).json()
}

export async function removeMCPConnection(baseUrl: string, name: string): Promise<void> {
  await ok(await apiFetch(baseUrl, named(name), { method: "DELETE" }))
}

const oauth = (name: string, what: string) => `/api/mcp/connections/${encodeURIComponent(name)}/oauth/${what}`
const attempt = (id: string, what = "") => `/api/mcp/oauth/attempts/${encodeURIComponent(id)}${what ? `/${what}` : ""}`
const post = (body: unknown = {}): RequestInit => ({ method: "POST", headers: json, body: JSON.stringify(body) })

/** Starts authorizing from this browser: Legato listens on its own loopback address for the consent page's answer. */
export async function beginWebAuthorization(baseUrl: string, name: string): Promise<OAuthBegin> {
  return (await ok(await apiFetch(baseUrl, oauth(name, "begin"), post({ mode: "web" })))).json()
}

export async function getAuthorization(baseUrl: string, id: string): Promise<OAuthAttempt> {
  return (await ok(await apiFetch(baseUrl, attempt(id)))).json()
}

export async function confirmAuthorization(baseUrl: string, id: string): Promise<OAuthAttempt> {
  return (await ok(await apiFetch(baseUrl, attempt(id, "confirm"), post()))).json()
}

export async function cancelAuthorization(baseUrl: string, id: string): Promise<void> {
  await ok(await apiFetch(baseUrl, attempt(id, "cancel"), post()))
}

/** Ends Legato's authorization; upstream says whether the server revoked it. */
export async function disconnectMCPConnection(baseUrl: string, name: string): Promise<{ upstream: string }> {
  return (await ok(await apiFetch(baseUrl, oauth(name, "disconnect"), post()))).json()
}

type Shell = { legatoShell?: { mcp?: number; key?: string }; ReactNativeWebView?: { postMessage?: (message: string) => void } }

/** The Legato app this page runs in can connect MCP servers itself. */
export function appCanConnect(): boolean {
  const w = window as unknown as Shell
  return Boolean(w.legatoShell?.mcp && w.legatoShell.key && w.ReactNativeWebView?.postMessage)
}

export interface AppConnectOutcome {
  connection: string
  /** the attempt's status once the app handed the answer over */
  status?: string
  error?: string
}

let appRequests = 0

/**
 * Asks the Legato app to connect a server: it listens on its own loopback
 * address, opens consent in the system browser and hands the answer to this
 * server. Resolves with what came of it; the page then confirms the account.
 */
export function connectThroughApp(name: string, timeoutMs = 11 * 60_000): Promise<AppConnectOutcome> {
  const w = window as unknown as Shell
  const id = `mcp-${++appRequests}-${Date.now()}`
  return new Promise((resolve) => {
    const done = (outcome: AppConnectOutcome) => {
      window.removeEventListener("legato:shell-mcp", listen)
      clearTimeout(timer)
      resolve(outcome)
    }
    const listen = (event: Event) => {
      const detail = (event as CustomEvent).detail as { id?: unknown; connection?: string; status?: string; error?: string } | undefined
      if (!detail || detail.id !== id) return
      done({ connection: name, status: detail.status, error: detail.error })
    }
    const timer = setTimeout(() => done({ connection: name, error: "The Legato app did not finish connecting in time." }), timeoutMs)
    window.addEventListener("legato:shell-mcp", listen)
    w.ReactNativeWebView!.postMessage!(JSON.stringify({ legatoShell: "mcpConnect", type: "mcpConnect", key: w.legatoShell!.key, id, connection: name }))
  })
}
