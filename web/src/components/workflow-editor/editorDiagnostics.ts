import type { Diagnostic } from "@codemirror/lint"
import { offsetOf, type WorkflowDiagnostic } from "../../lib/workflowFiles"

/** toDiagnostics turns the server's line-and-column diagnostics into the
 * editor's offsets. */
export function toDiagnostics(text: string, server: WorkflowDiagnostic[]): Diagnostic[] {
  return server.map(d => {
    const from = offsetOf(text, d.line, d.column)
    const to = Math.max(from + 1, offsetOf(text, d.end_line || d.line, d.end_column || d.column + 1))
    return { from: Math.min(from, text.length), to: Math.min(to, text.length), severity: d.severity, message: d.message, source: "Legato" }
  })
}

/** mergeDiagnostics keeps the server's, then the schema's on lines the
 * server says nothing about. */
export function mergeDiagnostics(text: string, server: Diagnostic[], schema: Diagnostic[]): Diagnostic[] {
  const lineOf = (offset: number) => text.slice(0, offset).split("\n").length
  const lines = new Set(server.map(d => lineOf(d.from)))
  return [...server, ...schema.filter(d => !lines.has(lineOf(d.from)))]
}
