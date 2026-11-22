import { isMap, isScalar, isSeq, parseDocument, stringify, visit, type Document, type Node as YamlNode, type Pair, type YAMLMap, type YAMLSeq } from "yaml"

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
  let comment: string | undefined
  if (!node || (isScalar(node) && node.value === null && range(node)[0] === range(node)[1])) {
    to = colon + 1
  } else if (isScalar(node) && (node.type === "BLOCK_LITERAL" || node.type === "BLOCK_FOLDED")) {
    // The header's comment: key: |- # why
    comment = /^[ \t]*[|>][-+0-9]*([ \t]+#.*)$/.exec(text.slice(colon + 1, eolOf(colon)))?.[1]
    to = blockEnd(text, node)
  } else if (lineStart(text, range(node)[0]) === lineStart(text, colon) && lineStart(text, Math.max(range(node)[0], range(node)[1] - 1)) === lineStart(text, colon)) {
    // A one-line value (a scalar or a flow collection) on the key's line.
    to = range(node)[1]
    if (written.includes("\n")) {
      comment = /^([ \t]+#.*)$/.exec(text.slice(to, eolOf(to)))?.[1]
      if (comment) to = eolOf(to)
    }
  } else {
    // A block under the key: a comment on the key's line.
    comment = /^([ \t]*#.*)$/.exec(text.slice(colon + 1, eolOf(colon)))?.[1]
    to = blockEnd(text, node)
  }
  if (comment) {
    const spaced = /^[ \t]/.test(comment) ? comment : " " + comment
    const lineBreak = written.indexOf("\n")
    written = lineBreak < 0 ? written + spaced : written.slice(0, lineBreak) + spaced + written.slice(lineBreak)
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
  const start = lineStart(text, range(found.pair.key as YamlNode)[0])
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
