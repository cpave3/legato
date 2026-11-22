import type { WorkflowDiagnostic } from "../../lib/workflowFiles"

export const fieldClass = "min-h-9 w-full rounded-md border border-lw-line-strong bg-lw-ground px-2 text-sm text-lw-text"
export const labelClass = "flex flex-col gap-1 text-xs text-lw-text-2"

/** problemsAt are the diagnostics on a path or under it. */
export function problemsAt(diagnostics: WorkflowDiagnostic[], path: string): WorkflowDiagnostic[] {
  return diagnostics.filter(d => d.path === path || d.path?.startsWith(path + ".") || d.path?.startsWith(path + "["))
}

/** valueText is a value as a field shows it. */
export function valueText(value: unknown): string {
  if (value === undefined || value === null) return ""
  return typeof value === "string" ? value : JSON.stringify(value)
}
