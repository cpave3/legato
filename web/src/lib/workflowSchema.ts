import { isMap, isScalar, isSeq, parseDocument, type Node as YamlNode } from "yaml"
import schemaV1JSON from "../generated/workflow.schema.json"
import schemaJSON from "../generated/workflow-v2.schema.json"

// The workflow JSON Schemas, generated from Go (internal/engine/workflow,
// go test -run TestTheGeneratedSchemaIsCurrent -update), read here for the
// YAML editor's completion and its checks as you type. The server's check
// stays the authority: only it knows references and executor rules.
// workflowSchema is version 2's, the version Legato writes and the builder
// edits (LEG-484); a file that says schema_version: 1 is read against
// version 1's (schemaFor).

/* eslint-disable @typescript-eslint/no-explicit-any */
export type Schema = Record<string, any>
export const workflowSchema: Schema = schemaJSON as Schema
export const workflowSchemaV1: Schema = schemaV1JSON as Schema

/** schemaFor is the schema of a file's schema_version: version 1's for 1,
 * else version 2's. */
export function schemaFor(version: unknown): Schema {
  return version === 1 ? workflowSchemaV1 : workflowSchema
}

const versionOf = (doc: unknown): unknown => doc && typeof doc === "object" ? (doc as { schema_version?: unknown }).schema_version : undefined

export type YamlPath = (string | number)[]

export type Executor = { type: string; title: string; description: string; group?: string; summary?: string[] }

export const executors: Executor[] = workflowSchema["x-legato"].executors

/** deref follows a $ref in root's definitions, and a lone $ref wrapped in
 * allOf for its description. */
export function deref(schema: Schema | undefined, root: Schema = workflowSchema): Schema | undefined {
  const definitions: Record<string, Schema> = root.definitions
  let s = schema
  for (let i = 0; s && i < 8; i++) {
    if (typeof s.$ref === "string") {
      s = definitions[s.$ref.replace("#/definitions/", "")]
      continue
    }
    if (Array.isArray(s.allOf) && s.allOf.length === 1 && s.allOf[0].$ref && !s.properties) {
      s = { ...deref(s.allOf[0], root), description: s.description ?? deref(s.allOf[0], root)?.description }
      continue
    }
    break
  }
  return s
}

function matches(condition: Schema, value: unknown): boolean {
  if (!value || typeof value !== "object") return false
  for (const key of condition.required ?? []) if (!(key in (value as object))) return false
  for (const [key, rule] of Object.entries<Schema>(condition.properties ?? {})) {
    if ("const" in rule && (value as Record<string, unknown>)[key] !== rule.const) return false
  }
  return true
}

/** properties are an object schema's properties for a value: its own, and
 * those of each allOf branch whose if the value matches. */
export function properties(schema: Schema | undefined, value?: unknown, root: Schema = workflowSchema): Record<string, Schema> {
  const s = deref(schema, root)
  if (!s) return {}
  const out: Record<string, Schema> = { ...(s.properties ?? {}) }
  for (const branch of s.allOf ?? []) {
    if (branch.if) {
      if (matches(branch.if, value)) Object.assign(out, branch.then?.properties ?? {})
    } else if (branch.properties) {
      Object.assign(out, branch.properties)
    }
  }
  return out
}

/** requiredOf is an object schema's required keys for a value: its own,
 * and those of each allOf branch whose if the value matches. */
export function requiredOf(schema: Schema | undefined, value?: unknown, root: Schema = workflowSchema): string[] {
  const s = deref(schema, root)
  if (!s) return []
  const out: string[] = [...(s.required ?? [])]
  for (const branch of s.allOf ?? []) if (branch.if && matches(branch.if, value)) out.push(...(branch.then?.required ?? []))
  return out
}

function childSchema(schema: Schema | undefined, key: string | number, value: unknown, root: Schema): Schema | undefined {
  const s = deref(schema, root)
  if (!s) return undefined
  if (typeof key === "number") return s.items
  const props = properties(s, value, root)
  if (props[key]) return props[key]
  if (s.additionalProperties && typeof s.additionalProperties === "object") return s.additionalProperties
  return undefined
}

function valueAt(doc: unknown, path: YamlPath): unknown {
  let value = doc
  for (const key of path) {
    if (value === null || typeof value !== "object") return undefined
    value = (value as Record<string | number, unknown>)[key]
  }
  return value
}

/** schemaAt is the schema of the value at path, given the document's
 * values (a step's with depends on its type, and the schema on its
 * schema_version). */
export function schemaAt(path: YamlPath, doc: unknown): Schema | undefined {
  const root = schemaFor(versionOf(doc))
  let schema: Schema | undefined = root
  for (let i = 0; i < path.length && schema; i++) {
    schema = childSchema(schema, path[i], valueAt(doc, path.slice(0, i)), root)
  }
  return schema ? deref(schema, root) : undefined
}

/** branches are a schema's alternatives (anyOf), or itself. */
function branches(schema: Schema, root: Schema = workflowSchema): Schema[] {
  return Array.isArray(schema.anyOf) ? schema.anyOf.map(b => deref(b, root) ?? b) : [schema]
}

export type Completion = { label: string; detail?: string; info?: string; apply?: string; type: "property" | "enum" | "keyword" }

/** keyCompletions are the keys an object at path may take that it does not
 * have yet. */
export function keyCompletions(path: YamlPath, doc: unknown): Completion[] {
  const root = schemaFor(versionOf(doc))
  const schema = schemaAt(path, doc)
  const present = valueAt(doc, path)
  const have = present && typeof present === "object" ? Object.keys(present as object) : []
  const props = properties(schema, present, root)
  return Object.entries(props).filter(([key]) => !have.includes(key)).map(([key, raw]) => {
    const s = deref(raw, root) ?? raw
    const kind = s["x-legato"]?.kind ?? branches(s, root).map(b => b.type).filter(Boolean)[0]
    const required = (schema?.required ?? []).includes(key)
    return { label: key, detail: [kind, required ? "required" : ""].filter(Boolean).join(" · "), info: s.description, apply: `${key}: `, type: "property" as const }
  })
}

/** valueCompletions are the values a scalar at path may take: enums
 * (step types with their titles), booleans. */
export function valueCompletions(path: YamlPath, doc: unknown): Completion[] {
  const last = path[path.length - 1]
  if (last === "type" && path[0] === "steps" && path.length === 3) {
    return executors.map(e => ({ label: e.type, detail: e.title, info: e.description, type: "enum" as const }))
  }
  const schema = schemaAt(path, doc)
  if (!schema) return []
  const out: Completion[] = []
  for (const branch of branches(schema, schemaFor(versionOf(doc)))) {
    for (const value of branch.enum ?? []) out.push({ label: String(value), info: schema.description, type: "enum" })
    if ("const" in branch) out.push({ label: String(branch.const), info: schema.description, type: "enum" })
    if (branch.type === "boolean") out.push({ label: "true", type: "keyword" }, { label: "false", type: "keyword" })
  }
  return out
}

/** describe is what the editor shows on hover: a key's description. */
export function describe(path: YamlPath, doc: unknown): string | undefined {
  const schema = schemaAt(path, doc)
  if (!schema) return undefined
  const kind = schema["x-legato"]?.kind
  return [schema.description, kind ? `(${kind})` : ""].filter(Boolean).join(" ")
}

// --- The cursor's place in the text -------------------------------------

export type CursorContext = { path: YamlPath; mode: "key" | "value"; key?: string; from: number; prefix: string }

const lineShape = /^( *)((?:- +)*)(.*)$/

type LineInfo = { indent: number; dashes: number[]; keyCol: number; key?: string; rest: string; blank: boolean }

function lineInfo(line: string): LineInfo {
  const [, spaces, dashText, content] = lineShape.exec(line) ?? ["", "", "", line]
  const dashes: number[] = []
  let col = spaces.length
  for (const part of dashText.match(/- +/g) ?? []) { dashes.push(col); col += part.length }
  const key = /^([A-Za-z0-9_.-]+):(?:\s|$)/.exec(content)?.[1]
  const trimmed = content.trim()
  return { indent: spaces.length, dashes, keyCol: col, key, rest: key ? content.slice(key.length + 1).trim() : content, blank: trimmed === "" || trimmed.startsWith("#") }
}

/** contextAt finds where in the YAML the cursor is: the path of the object
 * whose key is typed, or of the key whose value is typed. Block style only. */
export function contextAt(text: string, offset: number): CursorContext | null {
  const lineStart = text.lastIndexOf("\n", offset - 1) + 1
  const before = text.slice(lineStart, offset)
  const lines = text.slice(0, lineStart).split("\n")
  lines.pop()
  const here = lineInfo(before)
  let mode: "key" | "value"
  let key: string | undefined
  let from: number
  let prefix: string
  const valueMatch = /^( *(?:- +)*)([A-Za-z0-9_.-]+):[ \t]*([^\s#{[]*)$/.exec(before)
  const keyMatch = /^( *(?:- +)*)([A-Za-z0-9_.-]*)$/.exec(before)
  if (keyMatch) {
    mode = "key"
    prefix = keyMatch[2]
    from = offset - prefix.length
  } else if (valueMatch) {
    mode = "value"
    key = valueMatch[2]
    prefix = valueMatch[3]
    from = offset - prefix.length
  } else {
    return null
  }
  const path = parentPath(lines, here.keyCol, here.dashes)
  if (!path) return null
  if (mode === "value") path.push(key!)
  return { path, mode, key, from, prefix }
}

// parentPath is the path of the object a key at column col sits in, from
// the lines above it. dashes are the list marks on the key's own line: the
// innermost starts a new item of its list.
function parentPath(lines: string[], col: number, ownDashes: number[]): YamlPath | null {
  const path: YamlPath = []
  let target = col
  // Whether target is a list mark's column, whose list's key may sit at
  // the same column ("steps:\n- id: a").
  let atDash = false
  if (ownDashes.length > 0) {
    const dash = ownDashes[ownDashes.length - 1]
    path.push(countItems(lines, lines.length, dash))
    target = dash
    atDash = true
  }
  for (let i = lines.length - 1; i >= 0 && !(target === 0 && !atDash); i--) {
    const info = lineInfo(lines[i])
    if (info.blank) continue
    const lastDash = info.dashes[info.dashes.length - 1]
    if (info.dashes.length > 0 && lastDash < target && info.keyCol >= target) {
      // This line starts the list item the key is in.
      path.unshift(countItems(lines, i, lastDash))
      target = lastDash
      atDash = true
      continue
    }
    const parentKey = info.key && info.rest === "" && (info.keyCol < target || (atDash && info.dashes.length === 0 && info.keyCol === target))
    if (parentKey) {
      path.unshift(info.key!)
      if (info.dashes.length > 0) {
        path.unshift(countItems(lines, i, lastDash))
        target = lastDash
        atDash = true
      } else {
        target = info.keyCol
        atDash = false
      }
      continue
    }
    if (info.keyCol < target || (atDash && info.keyCol === target && info.dashes.length === 0)) return null
  }
  return path
}

// countItems is the index of the list item that line i starts: the items
// with a mark at the same column above it, up to the list's key.
function countItems(lines: string[], i: number, dash: number): number {
  let n = 0
  for (let j = i - 1; j >= 0; j--) {
    const info = lineInfo(lines[j])
    if (info.blank) continue
    if (info.dashes.includes(dash) && info.indent <= dash) { n++; continue }
    if (info.indent < dash || (info.indent === dash && info.dashes.length === 0)) break
  }
  return n
}

/** valuePathAt is the path of the value the cursor is in: the key on its
 * line, or the key of the block scalar (key: | or >) the line is part of. */
export function valuePathAt(text: string, offset: number): YamlPath | null {
  const lineStart = text.lastIndexOf("\n", offset - 1) + 1
  let start = lineStart
  let line = text.slice(lineStart, offset)
  let keyed = /^( *(?:- +)*)([A-Za-z0-9_.-]+):[ \t]/.exec(line)
  if (!keyed) {
    // A line of a block scalar: find the key that opened it.
    const indent = /^ */.exec(line)![0].length
    let end = lineStart - 1
    while (end > 0) {
      const begin = text.lastIndexOf("\n", end - 1) + 1
      const above = text.slice(begin, end)
      const opener = /^( *(?:- +)*)([A-Za-z0-9_.-]+):[ \t]*[|>][-+0-9]*[ \t]*$/.exec(above)
      const aboveIndent = /^ */.exec(above)![0].length
      if (opener && aboveIndent < indent) { start = begin; line = above; keyed = opener; break }
      if (above.trim() !== "" && aboveIndent < indent) return null
      end = begin - 1
    }
    if (!keyed) return null
  }
  const context = contextAt(text.slice(0, start) + keyed[0].replace(/[ \t]+$/, " "), start + keyed[0].replace(/[ \t]+$/, " ").length)
  return context && context.mode === "value" ? context.path : null
}

/** completionsAt is what the editor offers at offset. */
export function completionsAt(text: string, offset: number): { from: number; options: Completion[] } | null {
  const context = contextAt(text, offset)
  if (!context) return null
  const doc = lenientValues(text)
  const options = context.mode === "key" ? keyCompletions(context.path, doc) : valueCompletions(context.path, doc)
  return { from: context.from, options }
}

/** lenientValues are the document's values, or what parses of them. */
export function lenientValues(text: string): unknown {
  try {
    return parseDocument(text).toJS({ maxAliasCount: 50 }) ?? {}
  } catch {
    return {}
  }
}

// --- Checks as you type --------------------------------------------------

export type SchemaDiagnostic = { from: number; to: number; severity: "error" | "warning"; message: string; path: string }

export function pathText(path: YamlPath): string {
  return path.map((p, i) => typeof p === "number" ? `[${p}]` : i === 0 ? p : `.${p}`).join("")
}

type Range = [number, number]

function rangeOf(node: YamlNode | null | undefined, fallback: Range): Range {
  const range = (node as { range?: [number, number, number] } | null | undefined)?.range
  return range ? [range[0], Math.max(range[1], range[0] + 1)] : fallback
}

function typeName(node: YamlNode | null): string {
  if (isMap(node)) return "object"
  if (isSeq(node)) return "array"
  if (isScalar(node)) {
    const v = node.value
    if (v === null) return "null"
    if (typeof v === "number") return Number.isInteger(v) ? "integer" : "number"
    return typeof v
  }
  return "null"
}

function typeFits(type: string, actual: string): boolean {
  if (type === actual) return true
  if (type === "number") return actual === "integer"
  return false
}

const expression = /\$\{\{[^}]*\}\}/

/** schemaDiagnostics checks text against the schema: syntax, unknown keys
 * (a warning under with, LEG-461), types, enums, patterns and required
 * keys. */
export function schemaDiagnostics(text: string): SchemaDiagnostic[] {
  const doc = parseDocument(text, { prettyErrors: false, uniqueKeys: true })
  const out: SchemaDiagnostic[] = []
  for (const error of doc.errors) {
    const [from, to] = error.pos
    out.push({ from, to: Math.max(to, from + 1), severity: "error", message: error.message.split("\n")[0], path: "" })
  }
  if (out.length > 0 || !doc.contents) return out
  const contents = doc.contents as YamlNode
  const root = schemaFor(isMap(contents) ? (contents.get("schema_version") as unknown) : undefined)
  check(root, contents, root, [], [0, 1], out)
  return out
}

function check(root: Schema, node: YamlNode | null, raw: Schema | undefined, path: YamlPath, anchor: Range, out: SchemaDiagnostic[]) {
  const schema = deref(raw, root)
  if (!schema || node == null) return
  const at = rangeOf(node, anchor)
  const report = (message: string, range: Range = at, severity: "error" | "warning" = "error") => out.push({ from: range[0], to: range[1], severity, message, path: pathText(path) })
  if (Array.isArray(schema.anyOf)) {
    const options = schema.anyOf.map((b: Schema) => deref(b, root) ?? b)
    const fits = options.some((option: Schema) => {
      const trial: SchemaDiagnostic[] = []
      check(root, node, option, path, anchor, trial)
      return trial.length === 0
    })
    if (!fits) {
      const types = options.map((o: Schema) => o.pattern === expression.source ? "a ${{ }} expression" : o.enum ? `one of ${o.enum.join(", ")}` : o.type).filter(Boolean)
      report(`${pathText(path)} must be ${types.join(" or ")}`)
    }
    return
  }
  const actual = typeName(node)
  if (schema.type && !typeFits(schema.type, actual)) {
    report(`${pathText(path) || "the file"} must be ${article(schema.type)}, not ${article(actual)}`)
    return
  }
  if (isScalar(node)) {
    const value = node.value
    if ("const" in schema && value !== schema.const) report(`${pathText(path)} must be ${schema.const}`)
    if (schema.enum && !schema.enum.includes(value)) report(`${pathText(path)} must be one of ${schema.enum.join(", ")}`)
    if (typeof value === "string") {
      if (schema.pattern && !new RegExp(schema.pattern).test(value)) report(`${pathText(path)} does not have the expected form`)
      if (schema.minLength && value.length < schema.minLength) report(`${pathText(path)} must not be empty`)
    }
    if (typeof value === "number" && typeof schema.minimum === "number" && value < schema.minimum) report(`${pathText(path)} must be ${schema.minimum} or more`)
    return
  }
  if (isSeq(node)) {
    if (schema.minItems && node.items.length < schema.minItems) report(`${pathText(path)} needs at least ${schema.minItems} item`)
    node.items.forEach((item, i) => check(root, item as YamlNode, schema.items, [...path, i], at, out))
    return
  }
  if (!isMap(node)) return
  const value = node.toJSON()
  const props = properties(schema, value, root)
  const keys: string[] = []
  for (const pair of node.items) {
    const keyNode = pair.key as YamlNode | null
    const key = isScalar(keyNode) ? String(keyNode.value) : ""
    keys.push(key)
    const keyRange = rangeOf(keyNode, at)
    const child = props[key] ?? (typeof schema.additionalProperties === "object" ? schema.additionalProperties : undefined)
    if (child) {
      check(root, pair.value as YamlNode, child, [...path, key], keyRange, out)
    } else if (schema.additionalProperties === false) {
      const executor = schema["x-legato"]?.type
      if (executor) report(`${executor} takes no with field ${key}: it is ignored, and a later release will refuse it`, keyRange, "warning")
      else report(`${pathText(path) || "a workflow"} has no field ${key}`, keyRange)
    }
  }
  for (const key of requiredOf(schema, value, root)) if (!keys.includes(key)) report(`${pathText(path) || "a workflow"} needs ${key}`, anchor)
  for (const branch of schema.allOf ?? []) {
    if (Array.isArray(branch.oneOf)) {
      const names = branch.oneOf.flatMap((b: Schema) => b.required ?? [])
      const given = names.filter((n: string) => keys.includes(n))
      if (given.length === 0) report(`${pathText(path)} needs ${names.join(" or ")}`, anchor)
      if (given.length > 1) report(`${pathText(path)} takes ${names.join(" or ")}, not both`, anchor)
    } else if (Array.isArray(branch.anyOf)) {
      const names = branch.anyOf.flatMap((b: Schema) => b.required ?? [])
      if (!names.some((n: string) => keys.includes(n))) report(`${pathText(path)} needs ${names.join(" or ")}`, anchor)
    } else if (branch.not?.required) {
      if (branch.not.required.every((n: string) => keys.includes(n))) report(`${pathText(path)} takes ${branch.not.required.join(" or ")}, not both`, anchor)
    }
  }
}

function article(type: string): string {
  return /^[aeiou]/.test(type) ? `an ${type}` : `a ${type}`
}
