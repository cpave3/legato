import { inputsOf, parseWorkflow, setField, stepsOf, triggersOf } from "../../lib/workflowDocument"
import { KIND_TITLES } from "./triggerTitles"
import { triggerSentence } from "./triggerForms"

// A workflow summed up, for the start page (LEG-514): a line for Paste
// YAML, and a template card's When, Asks for and Does; and the starting
// points' text.

function readable(text: string) {
  const parsed = parseWorkflow(text)
  if (parsed.errors.length > 0 || !parsed.values || typeof parsed.values !== "object" || Array.isArray(parsed.values)) return null
  if (!("steps" in parsed.values) && !("schema_version" in parsed.values)) return null
  return parsed.values
}

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`

/** summaryLine is "Starts when a card enters Review · asks for 2 inputs ·
 * 3 steps"; empty for text that is not a workflow. */
export function summaryLine(text: string): string {
  try { return summaryOf(text) } catch { return "" }
}

function summaryOf(text: string): string {
  const values = readable(text)
  if (!values) return ""
  const triggers = Object.entries(triggersOf(values))
  const starts = triggers.length === 0 ? "Runs by hand" : `Starts ${triggers.map(([kind, t]) => {
    const words = triggerSentence(kind, t).map(p => p.text).join("")
    return kind === "task.entered_column" ? `when ${words.charAt(0).toLowerCase()}${words.slice(1)}` : kind === "schedule" ? words.charAt(0).toLowerCase() + words.slice(1) : `when ${(KIND_TITLES[kind] ?? kind).replace(/^When /, "").toLowerCase()}`
  }).join(" or ")}`
  const inputs = Object.keys(inputsOf(values)).length
  return [starts, inputs === 0 ? "asks for nothing" : `asks for ${plural(inputs, "input", "inputs")}`, plural(stepsOf(values).length, "step", "steps")].join(" · ")
}

/** startLines are a template card's lines. */
export function startLines(text: string): { when: string; asks: string; does: string } {
  try { return linesOf(text) } catch { return { when: "", asks: "", does: "" } }
}

function linesOf(text: string): { when: string; asks: string; does: string } {
  const values = readable(text) ?? {}
  const triggers = Object.entries(triggersOf(values))
  const inputs = Object.keys(inputsOf(values))
  const steps = stepsOf(values).map(step => String(step?.title || step?.id || "")).filter(Boolean)
  return {
    when: triggers.length === 0 ? "By hand" : triggers.map(([kind, t]) => triggerSentence(kind, t).map(p => p.text).join("")).join("; "),
    asks: inputs.length === 0 ? "Nothing" : inputs.join(", "),
    does: steps.length === 0 ? "Nothing yet" : steps.join(" → "),
  }
}

const free = (base: string, taken: string[]) => {
  let name = base
  for (let n = 2; taken.includes(name); n++) name = `${base}-${n}`
  return name
}

/** blankWorkflow is a workflow with nothing in it yet, under a free name. */
export function blankWorkflow(taken: string[]): string {
  return `schema_version: 2\nname: ${free("new-workflow", taken)}\ntitle: New workflow\nsteps: []\n`
}

/** copyOf is one of yours as a new workflow: a free name, its title marked
 * a copy, every other byte as it was. */
export function copyOf(text: string, taken: string[]): string {
  const values = readable(text) ?? {}
  const name = free(`${String(values.name ?? "workflow")}-copy`, taken)
  const named = setField(text, ["name"], name).text
  return values.title ? setField(named, ["title"], `${values.title} (copy)`).text : named
}

/** asTemplate is a template as a new workflow: its name if free. */
export function asTemplate(text: string, taken: string[]): string {
  const values = readable(text) ?? {}
  const name = String(values.name ?? "workflow")
  return taken.includes(name) ? setField(text, ["name"], free(name, taken)).text : text
}
