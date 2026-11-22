import { deref, executors, workflowSchema, type Schema } from "../../lib/workflowSchema"

/** BuilderOptions are what the pickers offer: this Legato's macros, agent
 * tools with their tiers, and board columns. A value may still be typed. */
export type BuilderOptions = { macros: string[]; agents: string[]; tiers: Record<string, string[]>; columns: string[] }

export const noOptions: BuilderOptions = { macros: [], agents: [], tiers: {}, columns: [] }

export type StepValue = Record<string, unknown> & { id?: string; title?: string; type?: string; trigger?: string; with?: Record<string, unknown> | null; outputs?: Record<string, Record<string, unknown> | null> | null }

export type FieldSpec = { name: string; kind: string; description: string; required: boolean; expressions: boolean; enum?: string[]; requiredAtRun: boolean }
export type Group = { kind: "exactly_one" | "at_least_one"; fields: string[] }

export const STEP_KEYS = ["id", "title", "type", "trigger", "with", "outputs"]
const definitions: Record<string, Schema> = workflowSchema.definitions

/** stepForm is what a step type's form holds, from the schema: its with
 * fields by kind and its exclusive groups. */
export function stepForm(type: string): { fields: FieldSpec[]; groups: Group[] } | null {
  const schema = definitions[`with.${type}`]
  if (!schema) return null
  const fields = Object.entries<Schema>(schema.properties ?? {}).map(([name, raw]) => {
    const s = deref(raw) ?? raw
    const extension = s["x-legato"] ?? {}
    const enumValues = s.enum ?? (Array.isArray(s.anyOf) ? s.anyOf.find((b: Schema) => b.enum)?.enum : undefined)
    return { name, kind: String(extension.kind ?? "string"), description: String(s.description ?? ""), required: (schema.required ?? []).includes(name), expressions: Boolean(extension.expressions), enum: enumValues, requiredAtRun: Boolean(extension.required_at_run) }
  })
  const groups: Group[] = []
  for (const branch of schema.allOf ?? []) {
    if (branch.oneOf) groups.push({ kind: "exactly_one", fields: branch.oneOf.flatMap((b: Schema) => b.required ?? []) })
    if (branch.anyOf) groups.push({ kind: "at_least_one", fields: branch.anyOf.flatMap((b: Schema) => b.required ?? []) })
  }
  return { fields, groups }
}

/** The kinds a with field may be; a test fails when the schema has one the
 * form can't draw. */
export const FIELD_KINDS = ["string", "text", "integer", "boolean", "task", "task_or_id", "repository", "path", "column", "macro", "agent", "tier", "json"]

// stepReferences are the steps.<id>.outputs references in a step.
export function stepReferences(step: StepValue): { id: string; expression: string }[] {
  const out: { id: string; expression: string }[] = []
  const walk = (value: unknown) => {
    if (typeof value === "string") {
      // The engine's own reading: the expression split at its dots, so an
      // ID is anything without a dot (read/pr, say).
      for (const match of value.matchAll(/\$\{\{\s*([^}]+?)\s*\}\}/g)) {
        const parts = match[1].split(".")
        if ((parts.length === 4 || parts.length === 5) && parts[0] === "steps" && parts[2] === "outputs" && parts[1] !== "") out.push({ id: parts[1], expression: match[1] })
      }
    } else if (value && typeof value === "object") {
      Object.values(value).forEach(walk)
    }
  }
  walk(step.with)
  walk(step.outputs)
  return out
}

/** brokenReferences are the references an order of steps would break: a
 * step reading the outputs of one that would not come before it. */
export function brokenReferences(steps: StepValue[], order: string[]): string[] {
  const position = new Map(order.map((id, i) => [id, i]))
  const out: string[] = []
  for (const step of steps) {
    const at = position.get(step.id ?? "") ?? -1
    for (const ref of stepReferences(step)) {
      const source = position.get(ref.id)
      if (source !== undefined && source >= at) out.push(`step ${step.id} reads ${ref.expression}`)
    }
  }
  return out
}

/** newStep is a step of a type with what it needs to start from. */
export function newStep(type: string, taken: string[]): StepValue {
  const executor = executors.find(e => e.type === type)
  const base = type.replace(/[^a-z0-9]+/g, "-")
  let id = base
  for (let n = 2; taken.includes(id); n++) id = `${base}-${n}`
  const form = stepForm(type)
  const withValues: Record<string, unknown> = {}
  for (const field of form?.fields ?? []) {
    if (!field.required) continue
    withValues[field.name] = field.kind === "task" ? "${{ workflow.task }}" : field.kind === "boolean" ? false : ""
  }
  // An exactly-one group starts with its first choice.
  for (const group of form?.groups ?? []) if (group.kind === "exactly_one" && !group.fields.some(f => f in withValues)) withValues[group.fields[0]] = ""
  return { id, title: executor?.title ?? type, type, with: withValues }
}

