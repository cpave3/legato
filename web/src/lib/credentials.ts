import { apiFetch } from "./api"

export type CredentialRule = { operation?: string[]; resource?: string; approve: string; window?: string; signed?: boolean }
export type Credential = { name: string; profile: string; store: string; secret: string; rules: CredentialRule[] }
export type CredentialGrant = {
  id: string
  credential: string
  task_id: string
  execution_id: string
  operations: string[]
  resources: string[]
  mode: string
  uses_left?: number
  expires_at?: string
  approval_id?: string
  created_at: string
}
export type CredentialAuditEntry = {
  seq: number
  at: string
  event: string
  credential?: string
  operation?: string
  resource?: string
  task_id?: string
  approval_id?: string
  decided_by?: string
  detail?: string
}

async function get<T>(baseUrl: string, path: string, key: string): Promise<T[]> {
  const resp = await apiFetch(baseUrl, path)
  if (!resp.ok) throw new Error(await resp.text())
  const body = await resp.json()
  return body[key] ?? []
}

export const listCredentials = (baseUrl: string) => get<Credential>(baseUrl, "/api/credentials", "credentials")
export const listCredentialGrants = (baseUrl: string) => get<CredentialGrant>(baseUrl, "/api/credentials/grants", "grants")
export const listCredentialAudit = (baseUrl: string, limit = 20) => get<CredentialAuditEntry>(baseUrl, `/api/credentials/audit?limit=${limit}`, "entries")

export async function endCredentialGrant(baseUrl: string, id: string): Promise<void> {
  const resp = await apiFetch(baseUrl, `/api/credentials/grants/${encodeURIComponent(id)}`, { method: "DELETE" })
  if (!resp.ok && resp.status !== 204) throw new Error(await resp.text())
}

/** How long a grant lasts, in words. */
export function grantLasts(grant: CredentialGrant): string {
  if (grant.mode === "session") return "this session"
  if (grant.uses_left !== undefined) return grant.uses_left === 1 ? "one use" : `${grant.uses_left} uses`
  if (grant.expires_at) return `until ${new Date(grant.expires_at).toLocaleTimeString()}`
  return grant.mode
}
