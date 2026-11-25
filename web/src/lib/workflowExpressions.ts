import { findExpressions, isParseError, parseExpression, parsePath, pathNames, pathString, type PathPart } from "./workflowExpressionParse"
import { lenientValues, valuePathAt, workflowSchema, type YamlPath } from "./workflowSchema"

// What ${{ }} may read at each place in a workflow file, from the schema's
// x-legato expression contexts (internal/engine/workflow/expressions.go):
// the same rules as the engine's validateReferences, so the editor's
// completion and checks agree with what the engine accepts. The table in
// internal/engine/workflow/testdata/expressions.json is checked by both.

// A location with body is in a loop's pass: the step of that index in the
// loop step's steps, which reads the loop's context (LEG-490).
export type ExpressionLocation =
  | { kind: "run_title" }
  | { kind: "with"; step: number; body?: number }
  | { kind: "output_path"; step: number; output?: string }
  | { kind: "if"; step: number; body?: number }
  | { kind: "trigger"; trigger: string }

export type Reference = { expression: string; description: string }

type Root = { pattern: string; description: string }
type Context = { location: string; description: string; roots: Root[] }

const extension = workflowSchema["x-legato"]
const contexts: Record<string, Context> = extension.expressions
const taskFields: string[] = extension.task_fields
const repositoryFields: string[] = extension.repository_fields
const failureOutputs: string[] = extension.failure_outputs ?? []
const catalogOutputs: Record<string, { name: string; type: string }[]> = Object.fromEntries(
  (extension.executors as { type: string; outputs?: { name: string; type: string }[] }[]).map(executor => [executor.type, executor.outputs ?? []]))

/** ExpressionDefinition is what of a definition the rules read: its inputs
 * and its steps' IDs and outputs. */
type ExpressionStep = { id?: string; type?: string; on_failure?: string; outputs?: Record<string, { type?: string; description?: string } | null | undefined> | null; loop?: unknown; steps?: (ExpressionStep | null)[] | null }
export type ExpressionDefinition = {
  inputs?: Record<string, { type?: string; description?: string } | null | undefined> | null
  steps?: (ExpressionStep | null)[] | null
}
type Definition = ExpressionDefinition

// A loop step's own outputs, which later steps read (LoopOutputs in the
// engine).
const loopOutputs: Record<string, string> = { results: "json", last: "json", count: "integer", succeeded: "integer", failed: "integer", ended_by: "string" }

const inPass = (location: ExpressionLocation): location is { kind: "with" | "if"; step: number; body: number } =>
  (location.kind === "with" || location.kind === "if") && location.body !== undefined

function contextFor(location: ExpressionLocation): Context | undefined {
  if (inPass(location)) return contexts.pass
  return contexts[location.kind === "trigger" ? `trigger.${location.trigger}` : location.kind]
}

/** earlierSteps are the steps a location reads, the nearest of each ID
 * first: in a pass its loop's steps before it, then the steps before the
 * loop. */
function earlierSteps(definition: Definition, location: ExpressionLocation): ExpressionStep[] {
  const steps = definition.steps ?? []
  const step = "step" in location ? location.step : -1
  const before = steps.slice(0, Math.max(0, step)).filter((s): s is ExpressionStep => Boolean(s))
  if (!inPass(location)) return before
  const body = (steps[step]?.steps ?? []).slice(0, Math.max(0, location.body)).filter((s): s is ExpressionStep => Boolean(s))
  return [...body.reverse(), ...before.reverse()]
}

/** passOutputs are what a loop's pass result holds: its outputs, and
 * status and error. */
function passOutputs(definition: Definition, location: ExpressionLocation): Record<string, string> {
  const loop = "step" in location ? definition.steps?.[location.step] : undefined
  const out: Record<string, string> = { status: "string", error: "string" }
  for (const [name, output] of Object.entries(loop?.outputs ?? {})) out[name] = output?.type ?? ""
  return out
}

/** locationOf is the expression context of a value at a YAML path. */
export function locationOf(path: YamlPath): ExpressionLocation | null {
  const [first, second, third, , fifth] = path
  if (first === "run_title" && path.length === 1) return { kind: "run_title" }
  if (first === "steps" && typeof second === "number" && third === "with") return { kind: "with", step: second }
  if (first === "steps" && typeof second === "number" && third === "steps" && typeof path[3] === "number" && path[4] === "with") return { kind: "with", step: second, body: path[3] }
  if (first === "steps" && typeof second === "number" && third === "outputs" && fifth === "path" && path.length === 5) return { kind: "output_path", step: second, output: String(path[3]) }
  if (first === "on" && typeof second === "string" && third === "inputs") return { kind: "trigger", trigger: second }
  return null
}

// What each placeholder of a root's pattern may be, at a location.
function placeholder(name: string, definition: Definition, location: ExpressionLocation, chosen: string[]): string[] | "path" {
  const inputs = Object.entries(definition.inputs ?? {})
  const step = "step" in location ? location.step : -1
  const steps = definition.steps ?? []
  const nearest = earlierSteps(definition, location)
  const earlier = [...new Set(nearest.map(s => s.id).filter((id): id is string => Boolean(id)))]
  // The outputs of the step the pattern has named so far, else this one's.
  const own = inPass(location) ? steps[step]?.steps?.[location.body] : steps[step]
  const named = chosen.length >= 1 && chosen[0] && earlier.includes(chosen[0]) ? nearest.find(s => s.id === chosen[0]) : own
  const outputs = Object.entries(named ? readableOutputs(named) : {}).map(([key, type]) => [key, { type }] as const)
  switch (name) {
    case "input": return inputs.map(([key]) => key)
    case "task input": return inputs.filter(([, input]) => input?.type === "task").map(([key]) => key)
    case "repository input": return inputs.filter(([, input]) => input?.type === "repository").map(([key]) => key)
    case "task field": return taskFields
    case "repository field": return repositoryFields
    case "earlier step": return earlier
    case "output": return outputs.map(([key]) => key)
    case "file output": return outputs.filter(([, output]) => output?.type === "file").map(([key]) => key)
    case "loop output": return Object.keys(passOutputs(definition, location))
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
 * when the engine would accept it. whole is a field's whole value, where
 * both sides of ?? must have the same type; within longer text they need
 * not. At an if: location the expression is a condition's field, a bare
 * path. */
export function checkExpression(definition: Definition, location: ExpressionLocation, expression: string, whole = false): string | null {
  const context = contextFor(location)
  if (!context) return "${{ }} is not read here"
  if (location.kind === "trigger") {
    const segments = expression.trim().split(".")
    return context.roots.some(root => matchRoot(root, segments, definition, location)) ? null : `${expression.trim()} can't be read here`
  }
  if (location.kind === "if") {
    const path = parsePath(expression)
    if (isParseError(path)) return path.error
    const read = pathType(path, definition, location, context)
    return "error" in read ? read.error : null
  }
  const parsed = parseExpression(expression)
  if (isParseError(parsed)) return parsed.error
  const types: string[] = []
  for (const operand of parsed.operands) {
    if ("literal" in operand) { types.push(literalType(operand.literal)); continue }
    const read = pathType(operand.path, definition, location, context)
    if ("error" in read) return read.error
    types.push(read.type)
  }
  if (whole) {
    for (const other of types.slice(1)) {
      if (!typesMatch(types[0], other)) return `${expression.trim()}: both sides of ?? must have the same type, not ${types[0]} and ${other}`
    }
  }
  return null
}

function literalType(value: unknown): string {
  if (typeof value === "string") return "string"
  if (typeof value === "boolean") return "boolean"
  if (typeof value === "number") return "integer"
  return ""
}

function typesMatch(a: string, b: string): boolean {
  return a === "" || b === "" || a === "json" || b === "json" || a === b
}

/** readableOutputs are the outputs a later step may read of a step, with
 * their types: its catalog outputs, those it declares, and with
 * on_failure: continue the failure's. */
function readableOutputs(step: ExpressionStep): Record<string, string> {
  const out: Record<string, string> = {}
  if (step.loop) {
    Object.assign(out, loopOutputs)
    if (step.on_failure === "continue") for (const name of failureOutputs) out[name] ??= "string"
    return out
  }
  for (const output of catalogOutputs[step.type ?? ""] ?? []) out[output.name] = output.type
  for (const [name, output] of Object.entries(step.outputs ?? {})) out[name] = output?.type ?? ""
  if (step.on_failure === "continue") for (const name of failureOutputs) out[name] ??= "string"
  return out
}

/** pathType checks a path can be read here, as the engine's pathType does,
 * and says its declared type. */
function pathType(path: PathPart[], definition: Definition, location: ExpressionLocation, context: Context): { type: string } | { error: string } {
  const names = pathNames(path)
  const rest = path.slice(names.length)
  const reads = (prefix: string) => context.roots.some(root => root.pattern.startsWith(prefix))
  const expression = pathString(path)
  const taskPath = (fields: string[], after: PathPart[], problem: string): { type: string } | { error: string } => {
    if (!taskFields.includes(fields[0]) || (fields[0] !== "metadata" && (fields.length > 1 || after.length > 0))) return { error: problem }
    return { type: fields[0] === "metadata" ? "json" : "string" }
  }
  if (names[0] === "workflow" && names[1] === "task" && names.length >= 2) {
    if (!reads("workflow.task")) return { error: location.kind === "run_title" ? "run_title: workflow.task is unavailable before the workflow task is created" : `${expression} can't be read here` }
    if (names.length === 2) return rest.length > 0 ? { error: `workflow task has no field path "${pathString(rest)}"` } : { type: "task" }
    return taskPath(names.slice(2), rest, `workflow task has no field path "${pathString(path.slice(2))}"`)
  }
  if (names[0] === "workflow" && names[1] === "inputs" && names.length >= 3) {
    const input = (definition.inputs ?? {})[names[2]]
    if (!input) return { error: `input ${names[2]} is not declared` }
    const fields = names.slice(3)
    if (fields.length === 0 && rest.length === 0) return { type: input.type ?? "" }
    const problem = `input ${names[2]} has no field path "${pathString(path.slice(3)).replace(/^\./, "")}"`
    if (input.type === "json") return { type: "json" }
    if (input.type === "task" && fields.length > 0) return taskPath(fields, rest, problem)
    if (input.type === "repository" && fields.length === 1 && rest.length === 0 && repositoryFields.includes(fields[0])) return { type: "string" }
    return { error: problem }
  }
  if (names[0] === "loop") {
    if (!inPass(location)) return { error: `${expression}: loop is read only inside a loop` }
    const field = path[1] && "name" in path[1] ? path[1].name : ""
    const after = path.slice(2)
    switch (field) {
      case "index": return after.length > 0 ? { error: `${expression}: loop.index is a number` } : { type: "integer" }
      case "item": case "results": return { type: "json" }
      case "last": return { error: `${expression}: loop.last is read only in a loop's until` }
      case "previous": {
        if (after.length === 0) return { type: "json" }
        if (!("name" in after[0])) return { error: `${expression}: a pass result is read by name` }
        const type = passOutputs(definition, location)[after[0].name]
        if (type === undefined) return { error: `${expression}: the loop's pass result has no "${after[0].name}"` }
        if (after.length === 1) return { type }
        return type === "json" ? { type: "json" } : { error: `${expression}: pass output ${after[0].name} has no field "${pathString(after.slice(1)).replace(/^\./, "")}"` }
      }
    }
    return { error: `${expression}: loop needs a field: index, item, previous or results` }
  }
  if (names[0] === "steps" && names[2] === "outputs" && names.length >= 4) {
    const earlier = reads("steps.") ? earlierSteps(definition, location) : []
    const named = earlier.find(s => s?.id === names[1])
    if (!named) return { error: `steps.${names[1]} is not an earlier step` }
    const outputs = readableOutputs(named)
    if (!(names[3] in outputs)) return { error: `steps.${names.slice(1, 4).join(".")} is not an output of an earlier step` }
    const after = path.slice(4)
    if (after.length === 0) return { type: outputs[names[3]] }
    if (after.length === 1 && "name" in after[0] && after[0].name === "path") return { type: "string" }
    if (outputs[names[3]] === "json") return { type: "json" }
    return { error: `output ${names[3]} has no field "${pathString(after).replace(/^\./, "")}"` }
  }
  if (names[0] === "outputs" && names.length === 3 && path.length === 3 && names[2] === "path" && reads("outputs.")) {
    const step = "step" in location ? definition.steps?.[location.step] : undefined
    if (step?.outputs?.[names[1]]?.type === "file") return { type: "string" }
    return { error: `file output ${names[1]} is not declared` }
  }
  return { error: `${expression} can't be read here` }
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
  const parsed = parseExpression(expression)
  const first = !isParseError(parsed) ? parsed.operands[0] : undefined
  const segments = first && "path" in first ? pathNames(first.path) : expression.trim().split(".")
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


export type ExpressionDiagnostic = { from: number; to: number; severity: "error"; message: string }

/** expressionDiagnostics checks each ${{ }} in a file where it is, as the
 * engine will. */
export function expressionDiagnostics(text: string): ExpressionDiagnostic[] {
  const definition = lenientValues(text) as Definition
  const out: ExpressionDiagnostic[] = []
  for (const match of findExpressions(text)) {
    const path = valuePathAt(text, match.start)
    const location = path && locationOf(path)
    if (!location) continue
    const problem = checkExpression(definition, location, match.inner)
    if (problem) out.push({ from: match.start, to: match.end, severity: "error", message: problem })
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
  for (const match of findExpressions(text.slice(lineStart, lineEnd))) {
    const from = lineStart + match.start
    const to = lineStart + match.end
    if (offset < from || offset > to) continue
    const path = valuePathAt(text, from)
    const location = path && locationOf(path)
    if (!location) return null
    const definition = lenientValues(text) as Definition
    return { from, to, description: describeReference(definition, location, match.inner) ?? checkExpression(definition, location, match.inner) ?? "" }
  }
  return null
}
