import { isAlias, isMap, isScalar, isSeq, parseDocument, stringify, visit, type Document, type Node as YamlNode, type Pair, type YAMLMap, type YAMLSeq } from "yaml"
import { offsetOf, type WorkflowDiagnostic } from "./workflowFiles"

// A workflow file as the builder edits it (LEG-470, decided in LEG-462):
// the file's text is the source of truth, and every operation splices the
// text, so the bytes of every node it does not touch, comments included,
// stay as they were. A node an operation writes is written in one style: 2
// spaces, block collections, plain scalars where safe, | for multi-line
// text. A file with anchors, aliases or merge keys is read-only here, since
// editing through an alias is ambiguous. Steps are found by ID, never by
// their place alone, so later schema versions can add steps that are not
// in one list.

export type YamlPath = (string | number)[]

export type WorkflowEdit = { text: string; undo: { label: string; before: string; after: string } }

export class WorkflowDocumentError extends Error {}

export type WorkflowValues = {
  schema_version?: number; name?: string; title?: string; description?: string; run_title?: string; after_start?: string
  inputs?: Record<string, Record<string, unknown>> | null
  on?: Record<string, Record<string, unknown> | null> | null
  steps?: (Record<string, unknown> & { id?: string })[] | null
  [key: string]: unknown
}

export type ParsedWorkflow = {
  text: string
  /** errors are the YAML's syntax errors: the builder cannot open it. */
  errors: string[]
  /** readOnly says why the builder can't change the file, or is null. */
  readOnly: string | null
  values: WorkflowValues
}

function parse(text: string): Document {
  return parseDocument(text, { keepSourceTokens: true, uniqueKeys: false })
}

/** parseWorkflow reads a file for the builder. */
export function parseWorkflow(text: string): ParsedWorkflow {
  const doc = parse(text)
  const errors = doc.errors.map(error => error.message.split("\n")[0])
  if (errors.length > 0) return { text, errors, readOnly: "the YAML does not parse", values: {} }
  let readOnly: string | null = null
  visit(doc, {
    Alias() { readOnly = "the file uses YAML aliases: edit it as YAML"; return visit.BREAK },
    Node(_, node) {
      if ((node as { anchor?: string }).anchor) { readOnly = "the file uses YAML anchors: edit it as YAML"; return visit.BREAK }
    },
    Pair(_, pair) {
      if (isScalar(pair.key) && pair.key.value === "<<") { readOnly = "the file uses YAML merge keys: edit it as YAML"; return visit.BREAK }
    },
  })
  let values: WorkflowValues = {}
  try {
    values = (doc.toJS({ maxAliasCount: 50 }) ?? {}) as WorkflowValues
  } catch (error) {
    readOnly ??= error instanceof Error ? error.message : String(error)
  }
  return { text, errors, readOnly, values: values && typeof values === "object" ? values : {} }
}

const isObject = (value: unknown): value is Record<string, unknown> => value !== null && typeof value === "object" && !Array.isArray(value)

// conditionShape says what of a condition tree the form can't draw.
function conditionShape(condition: unknown): string | null {
  if (!isObject(condition)) return "a condition is not a mapping"
  for (const group of ["all", "any"] as const) {
    const items = condition[group]
    if (items == null) continue
    if (!Array.isArray(items) || items.length === 0) return `${group} is not a list of conditions`
    for (const item of items) { const problem = conditionShape(item); if (problem) return problem }
  }
  if (condition.not != null) return conditionShape(condition.not)
  if (condition.in != null && !Array.isArray(condition.in)) return "in is not a list"
  return null
}

/** shapeProblem says what of a parsed file is not of the shape a
 * workflow's parts have (a step that is not a mapping, say), so the
 * builder sends the operator to YAML rather than drawing it; null when the
 * shapes are right. Values the schema refuses are the check's business. */
export function shapeProblem(values: unknown): string | null {
  if (!isObject(values)) return "the file is not a mapping of a workflow's fields"
  const { inputs, steps, on } = values as WorkflowValues
  if (inputs != null) {
    if (!isObject(inputs)) return "inputs is not a mapping of inputs"
    for (const [name, input] of Object.entries(inputs)) if (!isObject(input)) return `input ${name} is not a mapping`
  }
  if (steps != null) {
    if (!Array.isArray(steps)) return "steps is not a list of steps"
    for (const [i, step] of steps.entries()) {
      if (!isObject(step)) return `step ${i + 1} is not a mapping`
      if (step.with != null && !isObject(step.with)) return `the with of step ${i + 1} is not a mapping`
      if (step.outputs != null && !isObject(step.outputs)) return `the outputs of step ${i + 1} are not a mapping`
      for (const [key, output] of Object.entries(isObject(step.outputs) ? step.outputs : {})) if (output != null && !isObject(output)) return `output ${key} of step ${i + 1} is not a mapping`
    }
  }
  if (on != null) {
    if (!isObject(on)) return "on is not a mapping of triggers"
    for (const [kind, trigger] of Object.entries(on)) {
      if (trigger != null && !isObject(trigger)) return `the ${kind} trigger is not a mapping`
      if (isObject(trigger) && trigger.inputs != null && !isObject(trigger.inputs)) return `the inputs of the ${kind} trigger are not a mapping`
      const conditions = isObject(trigger) ? trigger.conditions : undefined
      if (conditions != null) { const problem = conditionShape(conditions); if (problem) return `the ${kind} trigger's conditions: ${problem}` }
    }
  }
  return null
}

// --- Reads ----------------------------------------------------------------

export function metadata(values: WorkflowValues) {
  const { schema_version, name, title, description, run_title, after_start } = values
  return { schema_version, name, title, description, run_title, after_start }
}

export function inputsOf(values: WorkflowValues): Record<string, Record<string, unknown>> {
  return values.inputs && typeof values.inputs === "object" ? values.inputs : {}
}

export function stepsOf(values: WorkflowValues): (Record<string, unknown> & { id?: string })[] {
  return Array.isArray(values.steps) ? values.steps : []
}

export function stepById(values: WorkflowValues, id: string) {
  return stepsOf(values).find(step => step?.id === id)
}

export function triggersOf(values: WorkflowValues): Record<string, Record<string, unknown> | null> {
  return values.on && typeof values.on === "object" ? values.on : {}
}

// --- Text helpers ---------------------------------------------------------

const lineStart = (text: string, pos: number) => text.lastIndexOf("\n", pos - 1) + 1
function lineEnd(text: string, pos: number) {
  const end = text.indexOf("\n", pos)
  return end < 0 ? text.length : end + 1
}
const columnOf = (text: string, pos: number) => pos - lineStart(text, pos)
const splice = (text: string, from: number, to: number, insert: string) => text.slice(0, from) + insert + text.slice(to)

function range(node: YamlNode | null | undefined): [number, number, number] {
  const r = (node as { range?: [number, number, number] } | null | undefined)?.range
  if (!r) throw new WorkflowDocumentError("a node has no place in the file")
  return r
}

// contentEnd is where a node's text ends, with an inline comment on its last
// line, as the start of the next line.
function contentEnd(text: string, node: YamlNode): number {
  const [, valueEnd] = range(node)
  // A block scalar's range takes its last line break; stay on that line.
  const last = text[valueEnd - 1] === "\n" ? valueEnd - 1 : valueEnd
  return lineEnd(text, Math.max(0, last))
}

/** render writes a value in the builder's style, its lines indented. */
export function render(value: unknown, indent: number): string {
  const text = stringify(value, { indent: 2, lineWidth: 0, flowCollectionPadding: false, blockQuote: "literal", defaultKeyType: "PLAIN", defaultStringType: "PLAIN", nullStr: "" }).replace(/\n$/, "")
  const pad = " ".repeat(indent)
  return text.split("\n").map((line, i) => (i === 0 || line === "" ? line : pad + line)).join("\n")
}

function isCollection(value: unknown): boolean {
  return value !== null && typeof value === "object" && (Array.isArray(value) ? value.length > 0 : Object.keys(value as object).length > 0)
}

// valueText is how a value follows "key:": on the line, or as a block
// under it at indent.
function valueText(value: unknown, indent: number): string {
  if (isCollection(value)) return "\n" + " ".repeat(indent) + render(value, indent)
  const rendered = render(value, indent)
  if (rendered === "") return ""
  // A block scalar's header stays on the key's line.
  return " " + rendered
}

// --- Finding nodes ---------------------------------------------------------

type Located = { doc: Document; node: YamlNode | null; pair?: Pair; parent: YAMLMap | YAMLSeq | null }

function locate(doc: Document, path: YamlPath): Located | null {
  let node = doc.contents as YamlNode | null
  let parent: YAMLMap | YAMLSeq | null = null
  let pair: Pair | undefined
  for (const key of path) {
    if (typeof key === "number") {
      if (!isSeq(node) || key >= node.items.length) return null
      parent = node
      pair = undefined
      node = node.items[key] as YamlNode
    } else {
      if (!isMap(node)) return null
      const found = node.items.find(p => isScalar(p.key) && String(p.key.value) === key)
      if (!found) return null
      parent = node
      pair = found
      node = (found.value ?? null) as YamlNode | null
    }
  }
  return { doc, node, pair, parent }
}

function flowAncestor(doc: Document, path: YamlPath): YamlPath | null {
  let node = doc.contents as YamlNode | null
  for (let i = 0; i <= path.length; i++) {
    if ((isMap(node) || isSeq(node)) && node.flow) return path.slice(0, i)
    if (i === path.length) break
    const key = path[i]
    if (typeof key === "number") node = isSeq(node) ? node.items[key] as YamlNode : null
    else node = isMap(node) ? (node.items.find(p => isScalar(p.key) && String(p.key.value) === key)?.value ?? null) as YamlNode | null : null
    if (!node) break
  }
  return null
}

function valueAt(values: unknown, path: YamlPath): unknown {
  let value = values
  for (const key of path) {
    if (value === null || typeof value !== "object") return undefined
    value = (value as Record<string | number, unknown>)[key]
  }
  return value
}

// --- Operations -----------------------------------------------------------

function edit(label: string, before: string, after: string): WorkflowEdit {
  return { text: after, undo: { label, before, after } }
}

function writable(text: string): Document {
  const parsed = parseWorkflow(text)
  if (parsed.errors.length > 0) throw new WorkflowDocumentError(`the YAML does not parse: ${parsed.errors[0]}`)
  if (parsed.readOnly) throw new WorkflowDocumentError(parsed.readOnly)
  return parse(text)
}

// replaceValue writes value over the node at path, which exists.
function replaceValue(text: string, doc: Document, path: YamlPath, value: unknown): string {
  const flow = flowAncestor(doc, path)
  if (flow) {
    // A flow collection around the value is rewritten whole, in block style.
    const whole = valueAt(doc.toJS(), flow)
    const updated = structuredClone(whole)
    if (path.length === flow.length) return rewrite(text, doc, flow, value)
    setIn(updated, path.slice(flow.length), value)
    return rewrite(text, doc, flow, updated)
  }
  return rewrite(text, doc, path, value)
}

// rewrite writes value over the node at path in place of its text.
function rewrite(text: string, doc: Document, path: YamlPath, value: unknown): string {
  const found = locate(doc, path)
  if (!found) throw new WorkflowDocumentError(`nothing at ${path.join(".")}`)
  if (path.length === 0) return render(value, 0) + "\n"
  const last = path[path.length - 1]
  if (typeof last === "number") {
    const [start] = range(found.node)
    const indent = columnOf(text, start)
    const end = isScalar(found.node) && !(found.node.type === "BLOCK_LITERAL" || found.node.type === "BLOCK_FOLDED") ? range(found.node)[1] : blockEnd(text, found.node!)
    return splice(text, start, end, render(value, indent))
  }
  const key = found.pair!.key as YamlNode
  const [keyStart, keyEnd] = range(key)
  const indent = columnOf(text, keyStart) + 2
  const colon = text.indexOf(":", keyEnd)
  const node = found.node
  const lineOf = (pos: number) => lineEnd(text, pos)
  const eolOf = (pos: number) => { const e = lineOf(pos); return text[e - 1] === "\n" ? e - 1 : e }
  let written = valueText(value, indent)
  let to: number
  // A comment on the value being replaced: put back after the new value.
  // The value's own comments, each put back after the new value: one on
  // the key's line (a block's header, or above a value below the key), and
  // one after the value's end.
  const comments: string[] = []
  const keyLine = text.slice(colon + 1, eolOf(colon))
  if (!node || (isScalar(node) && node.value === null && range(node)[0] === range(node)[1])) {
    to = colon + 1
  } else if (isScalar(node) && (node.type === "BLOCK_LITERAL" || node.type === "BLOCK_FOLDED")) {
    // key: |- # why
    const header = /^[ \t]*[|>][-+0-9]*([ \t]+#.*)$/.exec(keyLine)?.[1]
    if (header) comments.push(header)
    to = blockEnd(text, node)
  } else {
    const below = lineStart(text, range(node)[0]) !== lineStart(text, colon)
    const onKeyLine = below ? /^[ \t]*(#.*)$/.exec(keyLine)?.[1] : undefined
    if (onKeyLine) comments.push(onKeyLine)
    if (isScalar(node) || ((isMap(node) || isSeq(node)) && node.flow)) {
      // A scalar that is not a block, or a flow collection, on one line or
      // several: it ends at its value; a comment after that is its own.
      to = range(node)[1]
      const after = /^([ \t]+#.*)$/.exec(text.slice(to, eolOf(to)))?.[1]
      if (after) { comments.push(after); to = eolOf(to) }
    } else {
      to = blockEnd(text, node)
    }
  }
  if (comments.length > 0) {
    const spaced = comments.map(c => c.trim()).join(" ")
    const lineBreak = written.indexOf("\n")
    written = lineBreak < 0 ? `${written} ${spaced}` : `${written.slice(0, lineBreak)} ${spaced}${written.slice(lineBreak)}`
  }
  return splice(text, colon + 1, Math.max(colon + 1, to), written)
}

// blockEnd is where a node's text ends: the end of its last line, before
// that line's break when it has one.
function blockEnd(text: string, node: YamlNode): number {
  const end = contentEnd(text, node)
  return text[end - 1] === "\n" ? end - 1 : end
}

function setIn(target: unknown, path: YamlPath, value: unknown) {
  let node = target as Record<string | number, unknown>
  for (const key of path.slice(0, -1)) {
    if (node[key] === null || typeof node[key] !== "object") node[key] = {}
    node = node[key] as Record<string | number, unknown>
  }
  const last = path[path.length - 1]
  if (value === undefined) delete node[last]
  else node[last] = value
}

// addPair adds key: value to the map at path, which exists, after its last
// pair.
function addPair(text: string, doc: Document, path: YamlPath, key: string, value: unknown): string {
  const flow = flowAncestor(doc, path)
  if (flow) {
    const updated = structuredClone(valueAt(doc.toJS(), flow)) ?? {}
    setIn(updated, [...path.slice(flow.length), key], value)
    return flow.length === 0 ? render(updated, 0) + "\n" : rewrite(text, doc, flow, updated)
  }
  const found = locate(doc, path)!
  const map = path.length === 0 ? doc.contents : found.node
  if (!isMap(map) || map.items.length === 0) {
    // An empty or null value becomes this one map.
    if (path.length === 0) return text + (text === "" || text.endsWith("\n") ? "" : "\n") + `${key}:${valueText(value, 2)}\n`
    return rewrite(text, doc, path, { [key]: value })
  }
  const first = map.items[0].key as YamlNode
  const indent = columnOf(text, range(first)[0])
  const lastPair = map.items[map.items.length - 1]
  const at = contentEnd(text, (lastPair.value ?? lastPair.key) as YamlNode)
  const line = `${" ".repeat(indent)}${render(key, indent)}:${valueText(value, indent + 2)}\n`
  const insertAt = at > text.length ? text.length : at
  const prefix = insertAt === text.length && !text.endsWith("\n") ? "\n" : ""
  return splice(text, insertAt, insertAt, prefix + line)
}

// removePair takes key and its value out of the map at path.
function removePair(text: string, doc: Document, path: YamlPath): string {
  const flow = flowAncestor(doc, path.slice(0, -1))
  if (flow) {
    const updated = structuredClone(valueAt(doc.toJS(), flow))
    setIn(updated, path.slice(flow.length), undefined)
    return flow.length === 0 ? render(updated, 0) + "\n" : rewrite(text, doc, flow, updated)
  }
  const found = locate(doc, path)
  if (!found?.pair) return text
  const map = found.parent as YAMLMap
  if (map.items.length === 1 && path.length > 1) {
    // The map's last pair: the map becomes empty.
    return rewrite(text, doc, path.slice(0, -1), {})
  }
  const keyStart = range(found.pair.key as YamlNode)[0]
  let start = lineStart(text, keyStart)
  // The comments written at the key's column just above it are its own.
  const column = columnOf(text, keyStart)
  while (start > 0) {
    const above = lineStart(text, start - 1)
    const line = text.slice(above, start - 1)
    if (/^\s*#/.test(line) && line.indexOf("#") === column) start = above
    else break
  }
  const end = contentEnd(text, (found.pair.value ?? found.pair.key) as YamlNode)
  return splice(text, start, end, "")
}

/** setField sets the value at path, making the maps on the way; undefined
 * removes it. Only that value's text changes. */
export function setField(text: string, path: YamlPath, value: unknown, label = `Set ${path.join(".")}`): WorkflowEdit {
  const doc = writable(text)
  const values = doc.toJS()
  if (value === undefined) return edit(label, text, valueAt(values, path) === undefined ? text : removePair(text, doc, path))
  if (JSON.stringify(valueAt(values, path)) === JSON.stringify(value)) return edit(label, text, text)
  if (locate(doc, path)) return edit(label, text, replaceValue(text, doc, path, value))
  // Make the parent first, then add the key to it.
  let at = path.length - 1
  while (at > 0 && !locate(doc, path.slice(0, at))) at--
  const parent = path.slice(0, at)
  const rest = path.slice(at)
  const nested: unknown = rest.slice(1).reduceRight<unknown>((inner, key) => ({ [key]: inner }), value)
  if (typeof rest[0] === "number") throw new WorkflowDocumentError("a list item is added with insertStep")
  return edit(label, text, addPair(text, doc, parent, String(rest[0]), nested))
}

// --- Steps ----------------------------------------------------------------

type Span = { start: number; end: number }

// stepSpans are the lines of each step: from its dash (with the comments
// written at the dash's column just above it) to the next step's.
function stepSpans(text: string, seq: YAMLSeq): Span[] {
  const starts = seq.items.map(item => {
    const itemStart = range(item as YamlNode)[0]
    let start = lineStart(text, itemStart)
    const dash = columnOf(text, text.lastIndexOf("-", itemStart))
    while (start > 0) {
      const above = lineStart(text, start - 1)
      const line = text.slice(above, start - 1)
      if (/^\s*#/.test(line) && line.indexOf("#") === dash) start = above
      else break
    }
    return start
  })
  return starts.map((start, i) => ({ start, end: i + 1 < starts.length ? starts[i + 1] : contentEnd(text, seq.items[i] as YamlNode) }))
}

function stepsSeq(doc: Document): YAMLSeq | null {
  const found = locate(doc, ["steps"])
  return found && isSeq(found.node) ? found.node : null
}

function stepIndex(doc: Document, id: string): number {
  const values = doc.toJS() as WorkflowValues
  const index = stepsOf(values).findIndex(step => step?.id === id)
  if (index < 0) throw new WorkflowDocumentError(`no step ${id}`)
  return index
}

function blockSteps(text: string, doc: Document): { text: string; doc: Document } {
  const seq = stepsSeq(doc)
  if (seq && !seq.flow) return { text, doc }
  const steps = stepsOf(doc.toJS() as WorkflowValues)
  const rewritten = locate(doc, ["steps"]) ? rewrite(text, doc, ["steps"], steps) : addPair(text, doc, [], "steps", steps)
  return { text: rewritten, doc: parse(rewritten) }
}

const withNewline = (text: string) => text.endsWith("\n") ? text : text + "\n"

/** insertStep puts a step at index at (the end when past it). */
export function insertStep(text: string, at: number, step: Record<string, unknown>): WorkflowEdit {
  let doc = writable(text)
  let working = text
  const steps = stepsOf(doc.toJS() as WorkflowValues)
  if (steps.length === 0) {
    const created = locate(doc, ["steps"]) ? rewrite(text, doc, ["steps"], [step]) : addPair(text, doc, [], "steps", [step])
    return edit(`Add step ${step.id ?? ""}`, text, created)
  }
  ;({ text: working, doc } = blockSteps(working, doc))
  const seq = stepsSeq(doc)!
  const spans = stepSpans(working, seq)
  const first = range(seq.items[0] as YamlNode)[0]
  const dash = columnOf(working, working.lastIndexOf("-", first))
  const block = " ".repeat(dash) + "- " + render(step, dash + 2) + "\n"
  const index = Math.max(0, Math.min(at, spans.length))
  const insertAt = index < spans.length ? spans[index].start : spans[spans.length - 1].end
  const before = insertAt === working.length && !working.endsWith("\n") ? "\n" : ""
  return edit(`Add step ${step.id ?? ""}`, text, splice(working, insertAt, insertAt, before + block))
}

/** removeStep takes a step out, with the comments that are its own. */
export function removeStep(text: string, id: string): WorkflowEdit {
  let doc = writable(text)
  const index = stepIndex(doc, id)
  let working = text
  ;({ text: working, doc } = blockSteps(working, doc))
  const seq = stepsSeq(doc)!
  const spans = stepSpans(working, seq)
  if (spans.length === 1) return edit(`Remove step ${id}`, text, rewrite(working, doc, ["steps"], []))
  const span = spans[index]
  let start = span.start
  if (index === spans.length - 1) {
    // The last step: the blank lines before it go too.
    start = Math.max(spans[index - 1].start, start)
    while (start > spans[index - 1].start && /^\s*\n$/.test(working.slice(lineStart(working, start - 1), start))) start = lineStart(working, start - 1)
  }
  return edit(`Remove step ${id}`, text, splice(working, start, span.end, ""))
}

/** moveStep moves a step to index to, comments and all. */
export function moveStep(text: string, id: string, to: number): WorkflowEdit {
  let doc = writable(text)
  const from = stepIndex(doc, id)
  let working = text
  ;({ text: working, doc } = blockSteps(working, doc))
  const spans = stepSpans(working, stepsSeq(doc)!)
  const target = Math.max(0, Math.min(to, spans.length - 1))
  if (target === from) return edit(`Move step ${id}`, text, text)
  const span = spans[from]
  const moved = withNewline(working.slice(span.start, span.end))
  const rest = splice(working, span.start, span.end, "")
  const restSpans = stepSpans(rest, stepsSeq(parse(rest))!)
  let base = rest
  let insertAt: number
  if (target < restSpans.length) {
    insertAt = restSpans[target].start
  } else {
    // After the last step, not at the end of the file: fields may follow.
    insertAt = restSpans[restSpans.length - 1].end
    if (base[insertAt - 1] !== "\n") { base = splice(base, insertAt, insertAt, "\n"); insertAt += 1 }
  }
  return edit(`Move step ${id}`, text, splice(base, insertAt, insertAt, moved))
}

// rewriteExpressions changes references inside ${{ }} only.
function rewriteExpressions(text: string, from: RegExp, to: string): string {
  return text.replace(/\$\{\{[^}]*\}\}/g, expression => expression.replace(from, to))
}

const escapeRegExp = (value: string) => value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")

/** renameStep gives a step a new ID, and every steps.<id>.outputs
 * reference with it. */
export function renameStep(text: string, id: string, newId: string): WorkflowEdit {
  const doc = writable(text)
  const index = stepIndex(doc, id)
  if (stepsOf(doc.toJS() as WorkflowValues).some(step => step?.id === newId)) throw new WorkflowDocumentError(`a step ${newId} exists`)
  const renamed = setField(text, ["steps", index, "id"], newId).text
  const rewritten = rewriteExpressions(renamed, new RegExp(`(\\bsteps\\.)${escapeRegExp(id)}(?=\\.outputs\\b)`, "g"), `$1${newId}`)
  return edit(`Rename step ${id} to ${newId}`, text, rewritten)
}

// --- Triggers and inputs ----------------------------------------------------

/** setTrigger writes a trigger under on:, or removes it with null (and on:
 * with its last trigger). */
export function setTrigger(text: string, kind: string, spec: Record<string, unknown> | null): WorkflowEdit {
  const label = spec ? `Set the ${kind} trigger` : `Remove the ${kind} trigger`
  if (spec) return { ...setField(text, ["on", kind], spec), undo: { label, before: text, after: setField(text, ["on", kind], spec).text } }
  const doc = writable(text)
  const triggers = Object.keys(triggersOf(doc.toJS() as WorkflowValues))
  if (!triggers.includes(kind)) return edit(label, text, text)
  const after = triggers.length === 1 ? removePair(text, doc, ["on"]) : removePair(text, doc, ["on", kind])
  return edit(label, text, after)
}

export function addInput(text: string, name: string, spec: Record<string, unknown>): WorkflowEdit {
  const doc = writable(text)
  if (inputsOf(doc.toJS() as WorkflowValues)[name]) throw new WorkflowDocumentError(`an input ${name} exists`)
  return { ...setField(text, ["inputs", name], spec), undo: { label: `Add input ${name}`, before: text, after: setField(text, ["inputs", name], spec).text } }
}

export function removeInput(text: string, name: string): WorkflowEdit {
  const doc = writable(text)
  const inputs = Object.keys(inputsOf(doc.toJS() as WorkflowValues))
  if (!inputs.includes(name)) return edit(`Remove input ${name}`, text, text)
  return edit(`Remove input ${name}`, text, inputs.length === 1 ? removePair(text, doc, ["inputs"]) : removePair(text, doc, ["inputs", name]))
}

// renameKey changes the text of the key at path.
function renameKey(text: string, doc: Document, path: YamlPath, key: string): string {
  const found = locate(doc, path)
  if (!found?.pair) return text
  const [start, end] = range(found.pair.key as YamlNode)
  return splice(text, start, end, render(key, 0))
}

/** renameInput gives an input a new name, with every
 * workflow.inputs.<name> reference and the triggers' mappings of it. */
export function renameInput(text: string, name: string, newName: string): WorkflowEdit {
  let doc = writable(text)
  const values = doc.toJS() as WorkflowValues
  if (!inputsOf(values)[name]) throw new WorkflowDocumentError(`no input ${name}`)
  if (inputsOf(values)[newName]) throw new WorkflowDocumentError(`an input ${newName} exists`)
  let working = renameKey(text, doc, ["inputs", name], newName)
  for (const kind of Object.keys(triggersOf(values))) {
    doc = parse(working)
    working = renameKey(working, doc, ["on", kind, "inputs", name], newName)
  }
  working = rewriteExpressions(working, new RegExp(`(\\bworkflow\\.inputs\\.)${escapeRegExp(name)}(?![A-Za-z0-9_-])`, "g"), `$1${newName}`)
  return edit(`Rename input ${name} to ${newName}`, text, working)
}

// --- Where things are (LEG-503) -----------------------------------------------
//
// The outline shows a file as items: its top-level fields (About), inputs,
// triggers and steps, nested steps included. A step is known by its
// StepPath, the engine's address (each/review), and a run's path with a
// loop pass in it (each/2/review) finds the same step. A step's children
// are the list under its own steps: key. Each item is checked on its own,
// so a step the forms can't draw (an anchor, an odd shape) is one item to
// edit as YAML while the rest stay visual: only a file that does not parse,
// or is not a mapping, has no items.

/** ItemRef names an item of the outline. */
export type ItemRef =
  | { kind: "about"; key: string }
  | { kind: "input"; name: string }
  | { kind: "trigger"; name: string }
  | { kind: "step"; path: string }

/** ItemPlace is an item and a field in it ([] for the item itself). */
export type ItemPlace = { item: ItemRef; field: YamlPath }

export type TextRange = { from: number; to: number }

/** Item is one item of the file: where it is, and why the forms can't
 * show it, or null. */
export type Item = { item: ItemRef; range: TextRange; problem: string | null }

const CONTAINERS = ["inputs", "on", "steps"]

/** itemKey is an item as a string, to key maps and lists by. */
export function itemKey(item: ItemRef): string {
  switch (item.kind) {
    case "about": return `about:${item.key}`
    case "input": return `input:${item.name}`
    case "trigger": return `trigger:${item.name}`
    case "step": return `step:${item.path}`
  }
}

function readable(text: string): Document | null {
  const doc = parse(text)
  return doc.errors.length > 0 ? null : doc
}

const keyOf = (pair: Pair) => isScalar(pair.key) ? String(pair.key.value) : ""
const pairOf = (map: unknown, key: string) => isMap(map) ? map.items.find(p => keyOf(p) === key) : undefined

// pairRange is a pair's text: its key to the end of its value, with an
// inline comment on the value's last line, without the line break.
function pairRange(text: string, pair: Pair): TextRange {
  return { from: range(pair.key as YamlNode)[0], to: blockEnd(text, (pair.value ?? pair.key) as YamlNode) }
}

// itemRange is a list item's text: from its dash to its end.
function itemRange(text: string, node: YamlNode): TextRange {
  const start = range(node)[0]
  const dash = text.lastIndexOf("-", start)
  return { from: dash >= 0 && text.slice(dash + 1, start).trim() === "" ? dash : start, to: blockEnd(text, node) }
}

const inRange = (r: TextRange, offset: number) => offset >= r.from && offset <= r.to

type StepNode = { path: string; yaml: YamlPath; node: YamlNode }

const stepSegment = (node: unknown, index: number) => {
  const id = isMap(node) ? node.get("id") : undefined
  return typeof id === "string" && id !== "" ? id : `#${index}`
}

// stepNodes are the steps of a list and theirs, parents first.
function stepNodes(seq: unknown, parent: string, yaml: YamlPath): StepNode[] {
  if (!isSeq(seq) || seq.flow) return []
  const out: StepNode[] = []
  seq.items.forEach((node, i) => {
    const path = parent === "" ? stepSegment(node, i) : `${parent}/${stepSegment(node, i)}`
    const at = [...yaml, i]
    out.push({ path, yaml: at, node: node as YamlNode })
    const children = pairOf(node, "steps")?.value
    out.push(...stepNodes(children, path, [...at, "steps"]))
  })
  return out
}

function findStep(doc: Document, path: string): StepNode | null {
  const all = stepNodes(pairOf(doc.contents, "steps")?.value, "", ["steps"])
  const exact = all.find(s => s.path === path)
  if (exact) return exact
  // A run's path names a loop's pass: each/2/review is each/review.
  const segments = path.split("/")
  const scope: string[] = []
  for (const segment of segments) {
    const candidate = [...scope, segment].join("/")
    if (all.some(s => s.path === candidate)) scope.push(segment)
    else if (!/^\d+$/.test(segment)) return null
  }
  return all.find(s => s.path === scope.join("/")) ?? null
}

/** stepYamlPath is where the step at a StepPath is in the file, such as
 * ["steps", 1, "steps", 0]; null when there is none. */
export function stepYamlPath(text: string, path: string): YamlPath | null {
  const doc = readable(text)
  return doc ? findStep(doc, path)?.yaml ?? null : null
}

function itemNode(doc: Document, item: ItemRef): { pair?: Pair; node: YamlNode | null; step?: StepNode } | null {
  switch (item.kind) {
    case "about": { const pair = pairOf(doc.contents, item.key); return pair ? { pair, node: pair.value as YamlNode } : null }
    case "input": { const pair = pairOf(pairOf(doc.contents, "inputs")?.value, item.name); return pair ? { pair, node: pair.value as YamlNode } : null }
    case "trigger": { const pair = pairOf(pairOf(doc.contents, "on")?.value, item.name); return pair ? { pair, node: pair.value as YamlNode } : null }
    case "step": { const step = findStep(doc, item.path); return step ? { node: step.node, step } : null }
  }
}

/** rangeOf is the text of an item, or of a field in it (["with",
 * "prompt"]); null when the file has no such thing or does not parse. */
export function rangeOf(text: string, item: ItemRef, field: YamlPath = []): TextRange | null {
  const doc = readable(text)
  if (!doc) return null
  const found = itemNode(doc, item)
  if (!found) return null
  if (field.length === 0) {
    if (found.pair) return pairRange(text, found.pair)
    return found.node ? itemRange(text, found.node) : null
  }
  let node: unknown = found.node
  let last: TextRange | null = null
  for (const key of field) {
    if (typeof key === "number") {
      if (!isSeq(node) || key >= node.items.length) return null
      node = node.items[key]
      last = itemRange(text, node as YamlNode)
    } else {
      const pair = pairOf(node, key)
      if (!pair) return null
      node = pair.value
      last = pairRange(text, pair)
    }
  }
  return last
}

// fieldAt is the deepest key path under node that holds offset.
function fieldAt(text: string, node: unknown, offset: number): YamlPath {
  if (isMap(node) && !node.flow) {
    for (const pair of node.items) {
      if (!inRange(pairRange(text, pair), offset)) continue
      return [keyOf(pair), ...fieldAt(text, pair.value, offset)]
    }
  } else if (isSeq(node) && !node.flow) {
    for (const [i, item] of node.items.entries()) {
      if (inRange(itemRange(text, item as YamlNode), offset)) return [i, ...fieldAt(text, item, offset)]
    }
  }
  return []
}

/** pathAt is the innermost item and field at a position in the text; null
 * between items, outside them, or in a file that does not parse. */
export function pathAt(text: string, offset: number): ItemPlace | null {
  const doc = readable(text)
  if (!doc || !isMap(doc.contents)) return null
  for (const pair of doc.contents.items) {
    if (!inRange(pairRange(text, pair), offset)) continue
    const key = keyOf(pair)
    if (key === "inputs" || key === "on") {
      const kind = key === "inputs" ? "input" : "trigger"
      if (!isMap(pair.value)) return { item: { kind: "about", key }, field: [] }
      for (const inner of pair.value.items) {
        if (inRange(pairRange(text, inner), offset)) return { item: { kind, name: keyOf(inner) } as ItemRef, field: fieldAt(text, inner.value, offset) }
      }
      return null
    }
    if (key === "steps") {
      if (!isSeq(pair.value) || pair.value.flow) return { item: { kind: "about", key }, field: [] }
      // The innermost step: the last in parents-first order that holds it.
      const holding = stepNodes(pair.value, "", ["steps"]).filter(s => inRange(itemRange(text, s.node), offset))
      const step = holding[holding.length - 1]
      return step ? { item: { kind: "step", path: step.path }, field: fieldAt(text, step.node, offset) } : null
    }
    return { item: { kind: "about", key }, field: fieldAt(text, pair.value, offset) }
  }
  return null
}

// yamlProblem says what in a node the forms can't edit: an anchor, an
// alias or a merge key.
function yamlProblem(node: unknown, skipChildSteps: boolean): string | null {
  let problem: string | null = null
  const check = (n: unknown) => {
    if (problem) return
    if (isAlias(n)) { problem = "it uses a YAML alias: edit it as YAML"; return }
    if ((n as { anchor?: string } | null)?.anchor) { problem = "it uses a YAML anchor: edit it as YAML"; return }
    if (isMap(n)) {
      for (const pair of n.items) {
        if (isScalar(pair.key) && pair.key.value === "<<") { problem = "it uses a YAML merge key: edit it as YAML"; return }
        if (skipChildSteps && n === node && keyOf(pair) === "steps" && isSeq(pair.value)) { if ((pair.value as { anchor?: string }).anchor) problem = "it uses a YAML anchor: edit it as YAML"; continue }
        check(pair.key); check(pair.value)
      }
    } else if (isSeq(n)) {
      n.items.forEach(check)
    }
  }
  check(node)
  return problem
}

function stepShape(step: unknown): string | null {
  if (!isObject(step)) return "the step is not a mapping"
  if (step.with != null && !isObject(step.with)) return "its with is not a mapping"
  if (step.outputs != null && !isObject(step.outputs)) return "its outputs are not a mapping"
  for (const [key, output] of Object.entries(isObject(step.outputs) ? step.outputs : {})) if (output != null && !isObject(output)) return `output ${key} is not a mapping`
  if (step.steps != null && !Array.isArray(step.steps)) return "its steps are not a list"
  return null
}

function triggerShape(trigger: unknown): string | null {
  if (trigger != null && !isObject(trigger)) return "the trigger is not a mapping"
  if (isObject(trigger) && trigger.inputs != null && !isObject(trigger.inputs)) return "its inputs are not a mapping"
  const conditions = isObject(trigger) ? trigger.conditions : undefined
  if (conditions != null) { const problem = conditionShape(conditions); if (problem) return `its conditions: ${problem}` }
  return null
}

const js = (node: unknown) => {
  try { return (node as { toJSON?: () => unknown } | null)?.toJSON?.() ?? (isScalar(node) ? node.value : node ?? null) } catch { return undefined }
}

/** itemsOf lists a file's items in the file's order, nested steps after
 * their parent, each with its range and why the forms can't show it. file
 * says why there are no items: the YAML does not parse, or the file is not
 * a mapping. */
export function itemsOf(text: string): { file: string | null; items: Item[] } {
  const doc = parse(text)
  if (doc.errors.length > 0) return { file: `the YAML does not parse: ${doc.errors[0].message.split("\n")[0]}`, items: [] }
  if (doc.contents == null) return { file: null, items: [] }
  if (!isMap(doc.contents)) return { file: "the file is not a mapping of a workflow's fields", items: [] }
  const items: Item[] = []
  for (const pair of doc.contents.items) {
    const key = keyOf(pair)
    const value = pair.value
    const container = CONTAINERS.includes(key)
    const shaped = key === "steps" ? isSeq(value) && !value.flow : isMap(value) && !value.flow
    if (container && value != null && !(isScalar(value) && value.value === null) && shaped && !(value as { anchor?: string }).anchor) {
      if (key === "steps") {
        for (const step of stepNodes(value, "", ["steps"])) {
          items.push({ item: { kind: "step", path: step.path }, range: itemRange(text, step.node), problem: yamlProblem(step.node, true) ?? stepShape(js(step.node)) })
        }
      } else {
        for (const inner of (value as YAMLMap).items) {
          const name = keyOf(inner)
          const shape = key === "inputs" ? (isObject(js(inner.value)) ? null : "the input is not a mapping") : triggerShape(js(inner.value))
          items.push({ item: { kind: key === "inputs" ? "input" : "trigger", name } as ItemRef, range: pairRange(text, inner), problem: yamlProblem(inner.value, false) ?? shape })
        }
      }
      continue
    }
    let problem = yamlProblem(value, false) ?? yamlProblem(pair.key, false)
    if (!problem && container && value != null && !(isScalar(value) && value.value === null)) {
      problem = key === "steps" ? (isSeq(value) ? "steps is a flow list: edit it as YAML" : "steps is not a list of steps")
        : isMap(value) ? `${key} is a flow mapping: edit it as YAML` : `${key} is not a mapping`
    }
    items.push({ item: { kind: "about", key }, range: pairRange(text, pair), problem })
  }
  return { file: null, items }
}

// diagnosticPath reads the server's path (steps[1].with.prompt) as a
// YamlPath.
function diagnosticPath(path: string): YamlPath {
  const out: YamlPath = []
  for (const match of path.matchAll(/([^.[\]]+)|\[(\d+)\]/g)) out.push(match[2] !== undefined ? Number(match[2]) : match[1])
  return out
}

// itemOfPath is the item a YamlPath falls in, or null for the file.
function itemOfPath(doc: Document, path: YamlPath): ItemRef | null {
  const [head, ...rest] = path
  if (typeof head !== "string") return null
  if (head === "inputs" || head === "on") {
    const map = pairOf(doc.contents, head)?.value
    if (!isMap(map) || rest.length === 0) return null
    // A trigger's kind has dots in it (task.entered_column): match the
    // file's own keys, the longest first.
    const names = map.items.map(keyOf).sort((a, b) => b.length - a.length)
    const joined = rest.map(String).join(".")
    const name = names.find(n => joined === n || joined.startsWith(n + "."))
    return name ? { kind: head === "inputs" ? "input" : "trigger", name } as ItemRef : null
  }
  if (head === "steps") {
    const steps = stepNodes(pairOf(doc.contents, "steps")?.value, "", ["steps"])
    let found: StepNode | undefined
    for (const step of steps) if (step.yaml.length <= path.length && step.yaml.every((k, i) => k === path[i])) found = step
    return found ? { kind: "step", path: found.path } : null
  }
  return pairOf(doc.contents, head) ? { kind: "about", key: head } : null
}

/** problemsByItem matches diagnostics to the items they are about, by
 * path, or by place when they have none; those of no item are under
 * "file". */
export function problemsByItem(text: string, diagnostics: WorkflowDiagnostic[]): Map<string, WorkflowDiagnostic[]> {
  const out = new Map<string, WorkflowDiagnostic[]>()
  const doc = readable(text)
  const add = (key: string, d: WorkflowDiagnostic) => out.set(key, [...(out.get(key) ?? []), d])
  for (const d of diagnostics) {
    let item: ItemRef | null = null
    if (doc && d.path) item = itemOfPath(doc, diagnosticPath(d.path))
    else if (doc && d.line > 0) item = pathAt(text, offsetOf(text, d.line, d.column))?.item ?? null
    add(item ? itemKey(item) : "file", d)
  }
  return out
}
