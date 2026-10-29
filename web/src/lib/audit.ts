import { apiFetch } from "./api"

// One row of the credential audit log: who (the agent of a task) did what
// with which credential, and when.
export interface AuditRow {
  seq: number
  at: string
  event: string
  execution_id?: string
  task_id?: string
  task_title?: string
  agent_kind?: string
  credential?: string
  operation?: string
  resource?: string
  amount?: string
  approval_id?: string
  decided_by?: string
  detail?: string
}

export interface AuditFilter {
  credential?: string
  event?: string
  task?: string
  before?: number
  limit?: number
}

export const AUDIT_EVENTS = ["use", "grant", "request", "pending", "deny", "refuse", "revoke"] as const

export async function fetchAudit(baseUrl: string, filter: AuditFilter = {}): Promise<AuditRow[]> {
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(filter)) {
    if (value !== undefined && value !== "") query.set(key, String(value))
  }
  const resp = await apiFetch(baseUrl, `/api/credentials/audit?${query}`)
  if (!resp.ok) throw new Error(await resp.text())
  const body = await resp.json()
  return body.entries ?? []
}

export interface AuditChain {
  intact: boolean
  broken_at?: number
}

export async function verifyAudit(baseUrl: string): Promise<AuditChain> {
  const resp = await apiFetch(baseUrl, "/api/credentials/audit/verify")
  if (!resp.ok) throw new Error(await resp.text())
  return resp.json()
}

// A run of rows shown as one: the newest is first, the oldest last.
export interface FoldedAuditRow {
  first: AuditRow
  last: AuditRow
  count: number
}

// foldAuditRows folds a run of the same use (same agent, credential,
// operation and resource) into one row, as an agent's burst of reads is.
// Decisions are never folded: each says something of its own.
export function foldAuditRows(rows: AuditRow[]): FoldedAuditRow[] {
  const out: FoldedAuditRow[] = []
  for (const row of rows) {
    const prev = out[out.length - 1]
    if (prev && row.event === "use" && sameUse(prev.last, row)) {
      prev.last = row
      prev.count++
      continue
    }
    out.push({ first: row, last: row, count: 1 })
  }
  return out
}

function sameUse(a: AuditRow, b: AuditRow): boolean {
  return a.event === "use" && a.task_id === b.task_id && a.execution_id === b.execution_id &&
    a.credential === b.credential && a.operation === b.operation && a.resource === b.resource
}

// eventTone is the colour of an event's chip.
export const eventTone: Record<string, string> = {
  use: "bg-lw-live-soft text-lw-live",
  grant: "bg-lw-accent/15 text-lw-accent",
  request: "bg-lw-line text-lw-text-2",
  pending: "bg-lw-needs-soft text-lw-needs",
  deny: "bg-lw-error-soft text-lw-error",
  refuse: "bg-lw-error-soft text-lw-error",
  revoke: "bg-lw-line text-lw-text-2",
}
