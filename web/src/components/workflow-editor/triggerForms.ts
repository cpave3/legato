import { workflowSchema } from "../../lib/workflowSchema"
import type { BuilderOptions } from "./stepForms"

export { KIND_TITLES } from "./triggerTitles"

/** triggerKinds are the kinds under on:, from the schema. */
export const triggerKinds: { kind: string; description: string }[] = Object.entries<{ description?: string }>(workflowSchema.definitions.triggers.properties)
  .map(([kind, property]) => ({ kind, description: String(property.description ?? "") }))

/** conditionFields are what a condition may compare, from the schema. */
export const conditionFields: string[] = [...workflowSchema["x-legato"].condition_fields, "task.metadata."]

export type Condition = { all?: Condition[]; any?: Condition[]; not?: Condition; field?: string; exists?: boolean; equals?: unknown; not_equals?: unknown; in?: unknown[] }

export const OPERATORS = ["equals", "not_equals", "exists", "in"] as const
export type Operator = typeof OPERATORS[number]

export function operatorOf(condition: Condition): Operator {
  return OPERATORS.find(op => op in condition) ?? "equals"
}

/** scalar reads a typed value as YAML would: true, false and numbers are
 * themselves, anything else text. */
export function scalar(text: string): unknown {
  const value = text.trim()
  if (value === "true") return true
  if (value === "false") return false
  if (value !== "" && /^-?\d+(\.\d+)?$/.test(value)) return Number(value)
  return value
}

export function scalarText(value: unknown): string {
  if (value === undefined || value === null) return ""
  return typeof value === "string" ? value : String(value)
}

/** withOperator is a field condition with a new operator and value. */
export function withOperator(condition: Condition, operator: Operator, value: unknown): Condition {
  const out: Condition = { field: condition.field ?? "" }
  if (operator === "exists") out.exists = value === undefined ? true : Boolean(value)
  else if (operator === "in") out.in = Array.isArray(value) ? value : [value].filter(v => v !== "" && v !== undefined)
  else out[operator] = value ?? ""
  return out
}

/** OperandKind is the type a condition's value is written as: chosen in
 * the form, so editing a value keeps its type ("123" stays text). */
export type OperandKind = "text" | "number" | "boolean"

export function operandKind(value: unknown): OperandKind {
  return typeof value === "number" ? "number" : typeof value === "boolean" ? "boolean" : "text"
}

/** operandOf is typed text as a value of a kind; text that is not a
 * number stays text, as it is not one. */
export function operandOf(raw: string, kind: OperandKind): unknown {
  if (kind === "number") return raw.trim() !== "" && Number.isFinite(Number(raw)) ? Number(raw) : raw
  if (kind === "boolean") return raw.trim() === "true" ? true : raw.trim() === "false" ? false : raw
  return raw
}

/** newTrigger is a trigger of a kind as it is added: paused, so the
 * operator switches it on (LEG-459). */
export function newTrigger(kind: string, options: BuilderOptions): Record<string, unknown> {
  return kind === "task.entered_column" ? { column: options.columns[0] ?? "", enabled: false } : kind === "schedule" ? { every: "weekdays", at: "09:00", enabled: false } : { enabled: false }
}


export type SentencePart = { text: string; strong?: boolean }

const fieldName = (field: unknown) => String(field ?? "").split(".").pop() || "a field"
const valueOf = (value: unknown) => Array.isArray(value) ? value.map(String).join(", ") : String(value)

// conditionSentence is a condition tree in words: field rows as "priority
// is high", all as " · ", any as " or ", not as "not (…)".
function conditionSentence(condition: Condition | undefined): SentencePart[] {
  if (!condition || typeof condition !== "object") return []
  // A group that is not a list is the check's to report; here it says nothing.
  const join = (items: Condition[] | undefined, between: string) => (Array.isArray(items) ? items : []).map(conditionSentence).filter(p => p.length > 0).flatMap((parts, i) => i === 0 ? parts : [{ text: between }, ...parts])
  if (condition.all) return join(condition.all, " · ")
  if (condition.any) return join(condition.any, " or ")
  if (condition.not) return [{ text: "not (" }, ...conditionSentence(condition.not), { text: ")" }]
  const name = fieldName(condition.field)
  if ("exists" in condition) return [{ text: `${name} is ${condition.exists === false ? "not set" : "set"}` }]
  if ("not_equals" in condition) return [{ text: `${name} is not ` }, { text: valueOf(condition.not_equals), strong: true }]
  if ("in" in condition) return [{ text: `${name} is one of ` }, { text: valueOf(condition.in), strong: true }]
  return [{ text: `${name} is ` }, { text: valueOf(condition.equals ?? ""), strong: true }]
}

const EVERY: Record<string, string> = { daily: "day", weekdays: "weekday" }

/** triggerSentence is a trigger as its When row reads: "A card enters
 * Review · priority is high", "Every weekday at 09:00". */
export function triggerSentence(kind: string, trigger: Record<string, unknown> | null | undefined): SentencePart[] {
  const t = trigger ?? {}
  if (kind === "task.entered_column") {
    const conditions = conditionSentence(t.conditions as Condition | undefined)
    return [{ text: "A card enters " }, { text: String(t.column || "a column"), strong: true }, ...(conditions.length > 0 ? [{ text: " · " }, ...conditions] : [])]
  }
  if (kind === "schedule") {
    if (t.cron) return [{ text: "On the cron schedule " }, { text: String(t.cron), strong: true }]
    const every = String(t.every ?? "")
    const when = every === "weekly" ? String(t.on ?? "week") : EVERY[every] ?? every
    return [{ text: "Every " }, { text: when, strong: true }, ...(t.at ? [{ text: " at " }, { text: String(t.at), strong: true }] : [])]
  }
  if (kind === "pr.changes_requested") return [{ text: "A PR gets changes requested" }]
  return [{ text: kind, strong: true }]
}
