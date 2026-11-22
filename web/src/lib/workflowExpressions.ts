import { lenientValues, valuePathAt, workflowSchema, type YamlPath } from "./workflowSchema"

// What ${{ }} may read at each place in a workflow file, from the schema's
// x-legato expression contexts (internal/engine/workflow/expressions.go):
// the same rules as the engine's validateReferences, so the editor's
// completion and checks agree with what the engine accepts. The table in
// internal/engine/workflow/testdata/expressions.json is checked by both.

export type ExpressionLocation =
  | { kind: "run_title" }
  | { kind: "with"; step: number }
  | { kind: "output_path"; step: number; output?: string }
  | { kind: "trigger"; trigger: string }

export type Reference = { expression: string; description: string }

type Root = { pattern: string; description: string }
type Context = { location: string; description: string; roots: Root[] }

const extension = workflowSchema["x-legato"]
const contexts: Record<string, Context> = extension.expressions
const taskFields: string[] = extension.task_fields
const repositoryFields: string[] = extension.repository_fields

/** ExpressionDefinition is what of a definition the rules read: its inputs
 * and its steps' IDs and outputs. */
export type ExpressionDefinition = {
  inputs?: Record<string, { type?: string; description?: string } | null | undefined> | null
  steps?: ({ id?: string; outputs?: Record<string, { type?: string; description?: string } | null | undefined> | null } | null)[] | null
}
type Definition = ExpressionDefinition

function contextFor(location: ExpressionLocation): Context | undefined {
  return contexts[location.kind === "trigger" ? `trigger.${location.trigger}` : location.kind]
}

/** locationOf is the expression context of a value at a YAML path. */
export function locationOf(path: YamlPath): ExpressionLocation | null {
  const [first, second, third, , fifth] = path
  if (first === "run_title" && path.length === 1) return { kind: "run_title" }
  if (first === "steps" && typeof second === "number" && third === "with") return { kind: "with", step: second }
  if (first === "steps" && typeof second === "number" && third === "outputs" && fifth === "path" && path.length === 5) return { kind: "output_path", step: second, output: String(path[3]) }
  if (first === "on" && typeof second === "string" && third === "inputs") return { kind: "trigger", trigger: second }
  return null
}

// What each placeholder of a root's pattern may be, at a location.
function placeholder(name: string, definition: Definition, location: ExpressionLocation, chosen: string[]): string[] | "path" {
  const inputs = Object.entries(definition.inputs ?? {})
  const step = "step" in location ? location.step : -1
  const steps = definition.steps ?? []
  const earlier = steps.slice(0, Math.max(0, step)).map(s => s?.id).filter((id): id is string => Boolean(id))
  // The outputs of the step the pattern has named so far, else this one's.
  const named = chosen.length >= 1 && chosen[0] && earlier.includes(chosen[0]) ? steps.find(s => s?.id === chosen[0]) : steps[step]
  const outputs = Object.entries(named?.outputs ?? {})
  switch (name) {
    case "input": return inputs.map(([key]) => key)
    case "task input": return inputs.filter(([, input]) => input?.type === "task").map(([key]) => key)
    case "repository input": return inputs.filter(([, input]) => input?.type === "repository").map(([key]) => key)
    case "task field": return taskFields
    case "repository field": return repositoryFields
    case "earlier step": return earlier
    case "output": return outputs.map(([key]) => key)
    case "file output": return outputs.filter(([, output]) => output?.type === "file").map(([key]) => key)
    case "path": return "path"
  }
  return []
}

type Part = { literal: string } | { slot: string }

function parts(pattern: string): Part[] {
  return pattern.split(".").map(part => {
    const slot = /^<(.+)>$/.exec(part)
    return slot ? { slot: slot[1] } : { literal: part }
  })
}

/** matchRoot reports whether an expression has the form of root here. */
function matchRoot(root: Root, segments: string[], definition: Definition, location: ExpressionLocation): boolean {
  const pattern = parts(root.pattern)
  const chosen: string[] = []
  for (let i = 0; i < pattern.length; i++) {
    const part = pattern[i]
    if ("literal" in part) {
      if (segments[i] !== part.literal) return false
      continue
    }
    const allowed = placeholder(part.slot, definition, location, chosen)
    if (allowed === "path") return segments.length > i && segments.slice(i).every(Boolean)
    if (!allowed.includes(segments[i])) return false
    chosen.push(segments[i])
  }
  return segments.length === pattern.length
}

/** checkExpression says what is wrong with an expression here, or null
 * when the engine would accept it. */
export function checkExpression(definition: Definition, location: ExpressionLocation, expression: string): string | null {
  const context = contextFor(location)
  if (!context) return "${{ }} is not read here"
  const segments = expression.trim().split(".")
  if (context.roots.some(root => matchRoot(root, segments, definition, location))) return null
  if (location.kind === "run_title" && segments[0] === "workflow" && segments[1] === "task") return "run_title: workflow.task is unavailable before the workflow task is created"
  if (segments[0] === "steps" && location.kind !== "run_title") return `${segments.slice(0, 4).join(".")} is not an output of an earlier step`
  if (segments[0] === "workflow" && segments[1] === "inputs" && !(definition.inputs ?? {})[segments[2]]) return `input ${segments[2]} is not declared`
  return `${expression.trim()} can't be read here`
}

/** references are the expressions that can be read here, each spelled out
 * (a metadata path stops at metadata.). */
export function references(definition: Definition, location: ExpressionLocation): Reference[] {
  const context = contextFor(location)
  if (!context) return []
  const out: Reference[] = []
  for (const root of context.roots) {
    const pattern = parts(root.pattern)
    const expand = (i: number, done: string[]) => {
      if (i === pattern.length) { out.push({ expression: done.join("."), description: root.description }); return }
      const part = pattern[i]
      if ("literal" in part) { expand(i + 1, [...done, part.literal]); return }
      const chosen = done.filter((_, j) => "slot" in pattern[j])
      const allowed = placeholder(part.slot, definition, location, chosen)
      if (allowed === "path") { out.push({ expression: [...done, ""].join("."), description: root.description }); return }
      for (const value of allowed) expand(i + 1, [...done, value])
    }
    expand(0, [])
  }
  return out.filter((ref, i) => out.findIndex(other => other.expression === ref.expression) === i)
}

/** describeReference says what an expression reads, for a hover. */
export function describeReference(definition: Definition, location: ExpressionLocation, expression: string): string | null {
  const context = contextFor(location)
  if (!context) return null
  const segments = expression.trim().split(".")
  const root = context.roots.find(r => matchRoot(r, segments, definition, location))
  if (!root) return null
  if (segments[0] === "workflow" && segments[1] === "inputs") {
    const input = (definition.inputs ?? {})[segments[2]]
    return `${root.description}: ${segments[2]}${input?.type ? ` (${input.type})` : ""}${input?.description ? `, ${input.description}` : ""}`
  }
  if (segments[0] === "steps") {
    const output = (definition.steps ?? []).find(s => s?.id === segments[1])?.outputs?.[segments[3]]
    return `${root.description}: ${segments[3]} of step ${segments[1]}${output?.type ? ` (${output.type})` : ""}${output?.description ? `, ${output.description}` : ""}`
  }
  return root.description
}

/** openExpression is the ${{ the cursor is in on its line, and what is
 * typed after it, or null. */
export function openExpression(lineBefore: string): { prefix: string } | null {
  const match = /\$\{\{\s*([A-Za-z0-9_.-]*)$/.exec(lineBefore)
  return match ? { prefix: match[1] } : null
}

export const expressionPattern = /\$\{\{\s*([^}]+?)\s*\}\}/g

export type ExpressionDiagnostic = { from: number; to: number; severity: "error"; message: string }

/** expressionDiagnostics checks each ${{ }} in a file where it is, as the
 * engine will. */
export function expressionDiagnostics(text: string): ExpressionDiagnostic[] {
  const definition = lenientValues(text) as Definition
  const out: ExpressionDiagnostic[] = []
  for (const match of text.matchAll(expressionPattern)) {
    const offset = match.index ?? 0
    const path = valuePathAt(text, offset)
    const location = path && locationOf(path)
    if (!location) continue
    const problem = checkExpression(definition, location, match[1])
    if (problem) out.push({ from: offset, to: offset + match[0].length, severity: "error", message: problem })
  }
  return out
}

/** expressionCompletions are the references to offer at offset, inside an
 * open ${{ }}, or null outside one. */
export function expressionCompletions(text: string, offset: number): { from: number; options: Reference[] } | null {
  const lineStart = text.lastIndexOf("\n", offset - 1) + 1
  const open = openExpression(text.slice(lineStart, offset))
  if (!open) return null
  const path = valuePathAt(text, offset)
  const location = path && locationOf(path)
  if (!location) return null
  return { from: offset - open.prefix.length, options: references(lenientValues(text) as Definition, location) }
}

/** expressionAt is the ${{ }} the position is in, with what it reads. */
export function expressionAt(text: string, offset: number): { from: number; to: number; description: string } | null {
  const lineStart = text.lastIndexOf("\n", offset - 1) + 1
  const lineEnd = text.indexOf("\n", offset) < 0 ? text.length : text.indexOf("\n", offset)
  for (const match of text.slice(lineStart, lineEnd).matchAll(expressionPattern)) {
    const from = lineStart + (match.index ?? 0)
    const to = from + match[0].length
    if (offset < from || offset > to) continue
    const path = valuePathAt(text, from)
    const location = path && locationOf(path)
    if (!location) return null
    const definition = lenientValues(text) as Definition
    return { from, to, description: describeReference(definition, location, match[1]) ?? checkExpression(definition, location, match[1]) ?? "" }
  }
  return null
}
