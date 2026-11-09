import { apiFetch } from "./api"

/** One way to decide an approval: allow it a certain way, or deny it. */
export type ApprovalOption = { id: string; label: string; kind: "allow" | "deny"; description?: string }

/** A question an agent asks, as the native protocol carries it. */
export type ApprovalQuestion = {
  id: string
  header?: string
  question: string
  options: { label: string; description?: string; preview?: string }[]
  multiSelect?: boolean
  allowOther?: boolean
}

/** What an approval carries beyond its title. Which fields are set depends on its source. */
export type ApprovalDetails = {
  request_id?: string
  agent_kind?: string
  task_title?: string
  tool?: string
  target?: string
  command?: string
  reason?: string
  cwd?: string
  plan?: string
  questions?: ApprovalQuestion[]
  // A credential request (source "credential").
  credential?: string
  operations?: string[]
  resources?: string[]
  broader?: boolean
  signed?: boolean
  // A Reeve change set (source reeve_change_set): its ID, the instance, its
  // level and every call it will run.
  id?: string
  instance?: string
  level?: "approve" | "signed" | "verified"
  calls?: ChangeSetCall[]
  [key: string]: unknown
}

export type Approval = {
  id: string
  source: string
  source_key: string
  task_id?: string
  execution_id?: string
  title: string
  summary?: string
  details: ApprovalDetails
  options: ApprovalOption[]
  blocking: boolean
  status: string
  decided_by?: string
  error?: string
  revision: number
  created_at: string
  /** Allowed only with a decision signed on an enrolled phone. */
  signed?: boolean
}

export type ApprovalDecision =
  | { decision: "allow"; option?: string }
  | { decision: "deny"; feedback?: string }
  | { decision: "answer"; answers: Record<string, string[]> }

export async function fetchPendingApprovals(baseUrl: string): Promise<Approval[]> {
  const resp = await apiFetch(baseUrl, "/api/approvals?status=pending")
  if (!resp.ok) throw new Error(await errorText(resp))
  const body = await resp.json()
  return body.approvals ?? []
}

export async function decideApproval(baseUrl: string, id: string, decision: ApprovalDecision): Promise<Approval> {
  const resp = await apiFetch(baseUrl, `/api/approvals/${encodeURIComponent(id)}/decision`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ ...decision, by: "web" }),
  })
  if (!resp.ok) throw new Error(await errorText(resp))
  return resp.json()
}

export async function dismissApproval(baseUrl: string, id: string, note = ""): Promise<Approval> {
  const resp = await apiFetch(baseUrl, `/api/approvals/${encodeURIComponent(id)}/dismiss`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ note, by: "web" }),
  })
  if (!resp.ok) throw new Error(await errorText(resp))
  return resp.json()
}

async function errorText(resp: Response): Promise<string> {
  const text = await resp.text()
  try {
    const body = JSON.parse(text)
    if (typeof body.error === "string") return resp.status === 409 ? "Already decided somewhere else." : body.error
  } catch { /* not JSON */ }
  return text || `HTTP ${resp.status}`
}

/** Who asks: the task's title (or ID) and the agent tool. */
export function approvalAsker(approval: Approval): string {
  const who = approval.details.task_title || approval.task_id || ""
  return [who, approval.details.agent_kind].filter(Boolean).join(" · ")
}

/** The line above an approval's title: what kind of request it is. */
// One call of a Reeve change set, as its approval shows it.
export interface ChangeSetCall {
  kind: "api" | "restart" | string
  method?: string
  path?: string
  body?: unknown
  class: "read" | "change" | "risky" | string
  description: string
  level: string
  setting?: string
  before?: unknown
  after?: unknown
  applies_at_restart?: boolean
  unreached?: string[]
  as_root?: boolean
}

export function approvalKind(approval: Approval): string {
  switch (approval.source) {
    case "reeve_change_set": return "Change set"
    case "reeve_rules": return "The Reeve's rules"
    case "agent_permission": return approval.details.plan ? "Plan" : "Permission"
    case "agent_question": return "Question"
    case "credential": return "Credential"
    default: return "Approval"
  }
}
