import { apiFetch } from "./api"
import { reeveName } from "./reeveName"

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
  // The name of the Reeve that asked, as it was then; absent when unnamed.
  reeve_name?: string
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
const reeveSources = new Set(["reeve_change_set", "reeve_rules", "reeve_secret", "reeve_seed"])

// isReeveApproval says whether the Reeve asked: its task is hidden, so it is
// named rather than its task.
export function isReeveApproval(approval: Approval): boolean {
  return reeveSources.has(approval.source)
}

export function approvalAsker(approval: Approval): string {
  if (isReeveApproval(approval)) return reeveName(approval.details.reeve_name).start
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
  // A config.yaml edit's diff, and what loading the edited copy said.
  diff?: string
  check?: string
  // A config edit that takes pinned settings out of config.yaml: the
  // file's values it keeps in the database, one line each.
  keeps?: string[]
  optional?: boolean
  // A legato command's arguments (kind legato).
  args?: string[]
  // A host call: the shell command (host_shell), the file (edit_file), the
  // service's unit and action, and whether it is a system one
  // (manage_service).
  command?: string
  file?: string
  unit?: string
  action?: string
  system?: boolean
}

export function approvalKind(approval: Approval): string {
  // The Reeve that asked, by the name it had then (details.reeve_name).
  const reeve = reeveName(approval.details.reeve_name)
  switch (approval.source) {
    case "reeve_change_set": return "Change set"
    case "reeve_rules": return `${reeve.start}'s rules`
    case "reeve_secret": return `Secret for ${reeve.mid}`
    case "reeve_seed": return `${reeve.start}'s seed`
    case "agent_permission": return approval.details.plan ? "Plan" : "Permission"
    case "agent_question": return "Question"
    case "credential": return "Credential"
    default: return "Approval"
  }
}
