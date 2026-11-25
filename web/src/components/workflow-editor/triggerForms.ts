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
  if (kind === "chat.message" || kind === "chat.mention") return { bot: options.bots?.[0] ?? "", enabled: false }
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
  if (kind === "chat.message" || kind === "chat.mention") return chatSentence(kind, t)
  return [{ text: kind, strong: true }]
}

/** TextMatch is a chat trigger's text: contains or regex, at most one, and
 * not_contains or not_regex, which the text must not match. */
export type TextMatch = { contains?: string; regex?: string; not_contains?: string; not_regex?: string }

const textOf = (value: unknown) => (value && typeof value === "object" ? value : {}) as TextMatch

/** textMatchOf is how a chat trigger matches text: its mode and pattern. */
export function textMatchOf(value: unknown): { mode: "" | "contains" | "regex"; pattern: string } {
  const match = textOf(value)
  if ("regex" in match) return { mode: "regex", pattern: scalarText(match.regex) }
  if ("contains" in match) return { mode: "contains", pattern: scalarText(match.contains) }
  return { mode: "", pattern: "" }
}

/** textExclusionOf is the text a chat trigger refuses: its mode and pattern. */
export function textExclusionOf(value: unknown): { mode: "" | "not_contains" | "not_regex"; pattern: string } {
  const match = textOf(value)
  if ("not_regex" in match) return { mode: "not_regex", pattern: scalarText(match.not_regex) }
  if ("not_contains" in match) return { mode: "not_contains", pattern: scalarText(match.not_contains) }
  return { mode: "", pattern: "" }
}

/** withTextTest is a chat trigger's text with one side (the match, or the
 * exclusion) set to mode and pattern, or taken out for mode ""; the other
 * side is kept. undefined when nothing is left. */
export function withTextTest(value: unknown, side: "match" | "exclusion", mode: string, pattern: string): TextMatch | undefined {
  const keys = side === "match" ? ["contains", "regex"] : ["not_contains", "not_regex"]
  const next: Record<string, unknown> = Object.fromEntries(Object.entries(textOf(value)).filter(([key]) => !keys.includes(key)))
  if (mode) next[mode] = pattern
  return Object.keys(next).length > 0 ? next as TextMatch : undefined
}

// chatSentence is a chat trigger in words: "A message in C123 on slack ·
// containing deploy", "A mention of discord".
function chatSentence(kind: string, t: Record<string, unknown>): SentencePart[] {
  const bot: SentencePart = { text: String(t.bot || "a bot"), strong: true }
  const channel: SentencePart[] = t.channel ? [{ text: " in " }, { text: String(t.channel), strong: true }] : []
  const parts: SentencePart[] = kind === "chat.mention"
    ? [{ text: "A mention of " }, bot, ...channel]
    : [{ text: "A message" }, ...channel, { text: " on " }, bot]
  if (t.user) parts.push({ text: " from " }, { text: String(t.user), strong: true })
  const match = textMatchOf(t.text)
  if (match.mode) parts.push({ text: match.mode === "regex" ? " · matching " : " · containing " }, { text: match.pattern, strong: true })
  const exclusion = textExclusionOf(t.text)
  if (exclusion.mode) parts.push({ text: exclusion.mode === "not_regex" ? " · not matching " : " · not containing " }, { text: exclusion.pattern, strong: true })
  if (t.fallback === true) parts.push({ text: " · when no other trigger matched" })
  return parts
}
