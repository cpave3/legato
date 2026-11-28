import type { BuilderOptions, FieldSpec } from "./stepForms"

// How a with field's value reads and is written, apart from the controls
// that draw it.

/** isExpressionValue is a value holding a ${{ }} expression, which is
 * always shown as text so it can be edited. */
export const isExpressionValue = (value: unknown): value is string => typeof value === "string" && value.includes("${{")

/** isWholeNumber is text that is a whole number. */
export const isWholeNumber = (raw: string) => /^-?\d+$/.test(raw.trim())

/** notANumber is a number field holding text that is neither a whole
 * number nor an expression: the engine may only find out when the step
 * runs. */
export function notANumber(kind: string, value: unknown): boolean {
  if (kind !== "integer" || typeof value !== "string") return false
  return value.trim() !== "" && !isExpressionValue(value) && !isWholeNumber(value)
}

// What this Legato offers for each kind of field: a tier the agent's (or
// every agent's) tiers.
const SUGGESTIONS: Record<string, (options: BuilderOptions, agent: string) => string[]> = {
  macro: options => options.macros,
  agent: options => options.agents,
  tier: (options, agent) => options.tiers[agent] ?? Object.values(options.tiers).flat(),
  column: options => options.columns,
  repository: options => options.repositories ?? [],
  chat_bot: options => options.bots ?? [],
}

/** suggestionsFor are the values this Legato suggests for a kind of
 * field. */
export const suggestionsFor = (kind: string, options: BuilderOptions, agent: string): string[] => SUGGESTIONS[kind]?.(options, agent) ?? []

/** jsonText is a JSON field's value as text to edit. */
export function jsonText(value: unknown): string {
  if (value === undefined) return ""
  return typeof value === "string" ? value : JSON.stringify(value, null, 2)
}

/** jsonValue is what a JSON field's text writes: nothing for none, the
 * value it parses to, or the text when it doesn't. */
export function jsonValue(raw: string): unknown {
  if (raw.trim() === "") return undefined
  try { return JSON.parse(raw) } catch { return raw }
}

// The kinds whose field is text, which in the inspector takes values at
// its cursor (LEG-510).
const TEXT_KINDS = ["string", "text", "path", "repository", "column", "macro", "tier", "agent", "chat_bot", "task_or_id"]

/** takesExpressionText says a field is drawn, in the inspector, as text
 * that takes values at its cursor: a text kind that can read values, and
 * not a choice from an enum (unless it holds an expression). */
export function takesExpressionText(spec: FieldSpec, rich: boolean, hasGroups: boolean, expression: boolean): boolean {
  if (!rich || !hasGroups || !TEXT_KINDS.includes(spec.kind)) return false
  return !spec.enum || expression
}

/** showsPicker says a field offers the reference picker: one that takes
 * expressions, outside the inspector (which has its own), and not buttons,
 * which take references row by row. */
export const showsPicker = (spec: FieldSpec, rich: boolean) => spec.expressions && !rich && spec.kind !== "chat_buttons"
