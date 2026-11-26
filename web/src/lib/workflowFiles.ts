import { apiFetch } from "./api"
import type { WorkflowDefinition } from "./workflow"
import type { WorkflowGraphModel } from "./workflowGraph"

// The workflow file API (internal/server/workflow_files.go): a workflow's
// file as its bytes, a check of what is typed, and the writes. A refusal
// carries a code the editor acts on.

// fix names a change Legato makes for the operator: "upgrade" is the
// server's upgrade (upgradeWorkflowFile), as on the deprecated inline review
// step's warning (LEG-599), which the editor offers as a quick fix.
export type WorkflowDiagnosticFix = "upgrade"
export type WorkflowDiagnostic = { severity: "error" | "warning"; message: string; path?: string; line: number; column: number; end_line: number; end_column: number; fix?: WorkflowDiagnosticFix }

/** fixLabel is what a diagnostic's quick fix is called. */
export const fixLabel = (fix: WorkflowDiagnosticFix) => fix === "upgrade" ? "Rewrite to call review-loop" : fix

export type WorkflowFile = { name: string; yaml: string; path: string; folder: string; digest: string; editable: boolean; reason?: string }

export type SchedulePreview = { description: string; timezone: string; next: string[] }

export type WorkflowCheck = { definition: WorkflowDefinition | null; graph: WorkflowGraphModel | null; diagnostics: WorkflowDiagnostic[]; schedule?: SchedulePreview | null }

export type WorkflowSaved = { name: string; path: string; folder: string; digest: string; diagnostics: WorkflowDiagnostic[] }

export type WorkflowFileErrorCode = "not_found" | "not_editable" | "invalid" | "rename" | "name_taken" | "stale" | "confirm_triggers" | "folder" | "not_builtin"

export class WorkflowFileError extends Error {
  readonly status: number
  readonly code?: WorkflowFileErrorCode
  readonly body: Record<string, unknown>
  constructor(status: number, body: Record<string, unknown>) {
    super(typeof body.error === "string" ? body.error : `request failed (${status})`)
    this.status = status
    this.code = typeof body.code === "string" ? body.code as WorkflowFileErrorCode : undefined
    this.body = body
  }
  /** triggers are those a confirm_triggers refusal names. */
  get triggers(): string[] { return Array.isArray(this.body.triggers) ? this.body.triggers as string[] : [] }
  get diagnostics(): WorkflowDiagnostic[] { return Array.isArray(this.body.diagnostics) ? this.body.diagnostics as WorkflowDiagnostic[] : [] }
}

async function call<T>(baseUrl: string, path: string, init?: RequestInit): Promise<T> {
  const response = await apiFetch(baseUrl, path, init)
  let body: Record<string, unknown> = {}
  try { body = await response.json() } catch { /* not JSON */ }
  if (!response.ok) throw new WorkflowFileError(response.status, body)
  return body as T
}

const json = (method: string, body: unknown): RequestInit => ({ method, headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) })
const named = (name: string) => `/api/workflows/${encodeURIComponent(name)}`

export const fetchWorkflowFile = (baseUrl: string, name: string) => call<WorkflowFile>(baseUrl, `${named(name)}/file`)

export const checkWorkflowFile = (baseUrl: string, yaml: string, signal?: AbortSignal) =>
  call<WorkflowCheck>(baseUrl, "/api/workflow-files/check", { ...json("POST", { yaml }), signal })

export const createWorkflowFile = (baseUrl: string, yaml: string, folder: string, confirmTriggers = false) =>
  call<WorkflowSaved>(baseUrl, "/api/workflow-files", json("POST", { yaml, folder, confirm_triggers: confirmTriggers }))

export const saveWorkflowFile = (baseUrl: string, name: string, yaml: string, digest: string, confirmTriggers = false) =>
  call<WorkflowSaved>(baseUrl, `${named(name)}/file`, json("PUT", { yaml, digest, confirm_triggers: confirmTriggers }))

export const deleteWorkflowFile = (baseUrl: string, name: string, digest: string) =>
  call<{ name: string; removed: string; remains: boolean }>(baseUrl, `${named(name)}/file`, json("DELETE", { digest }))

/** WorkflowUpgrade is a file upgraded to schema_version 2 (LEG-484): its
 * text, whether it changed, and what the upgrade left to do by hand, each
 * at its line in the upgraded text. */
export type WorkflowUpgrade = { yaml: string; changed: boolean; notes: WorkflowDiagnostic[] }

export const upgradeWorkflowFile = (baseUrl: string, yaml: string) =>
  call<WorkflowUpgrade>(baseUrl, "/api/workflow-files/upgrade", json("POST", { yaml }))

export const copyBuiltInWorkflow = (baseUrl: string, name: string, folder: string) =>
  call<WorkflowSaved>(baseUrl, `${named(name)}/copy`, json("POST", { folder }))

/** offsetOf turns a 1-based line and column into an offset in text. */
export function offsetOf(text: string, line: number, column: number): number {
  let offset = 0
  for (let l = 1; l < line; l++) {
    const next = text.indexOf("\n", offset)
    if (next < 0) return text.length
    offset = next + 1
  }
  return Math.min(text.length, offset + Math.max(0, column - 1))
}
