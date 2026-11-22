import { workflowSchema } from "../../lib/workflowSchema"

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
