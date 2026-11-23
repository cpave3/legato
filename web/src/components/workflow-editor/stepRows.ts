import { stepForm, type StepValue } from "./stepForms"

// What a step row of the outline says (LEG-506): a one-line summary from
// the catalog's summary fields, and, on the connector before it, how the
// step starts.

export type Start = "automatic" | "manual" | "agent_ended"

/** startOf is how a step starts: v2's start:, else v1's trigger:. */
export function startOf(step: Record<string, unknown>): Start | string {
  const start = step.start ?? step.trigger
  return typeof start === "string" && start !== "" ? start : "automatic"
}

const oneLine = (value: unknown): string => {
  if (value === undefined || value === null || value === "") return ""
  const text = typeof value === "string" ? value : JSON.stringify(value)
  return text.trim().split("\n")[0].trim()
}

/** stepSummary is the first of the step type's summary fields that has a
 * value, on one line; a wait (v2) says what it waits for. */
export function stepSummary(step: StepValue): string {
  const values = step.with && typeof step.with === "object" ? step.with : {}
  if (step.type === "wait") return oneLine(Object.values(values).find(v => v !== undefined && v !== null && v !== ""))
  for (const name of step.type ? stepForm(step.type)?.summary ?? [] : []) {
    const line = oneLine(values[name])
    if (line) return line
  }
  return ""
}

export type Connector = { kind: "first" | Start | string; text: string }

const runsAnAgent = (step: Record<string, unknown>) => step.type === "agent.run" || step.type === "agent.dispatch"

/** connectorOf says how the step at index starts, as the line before it
 * reads: "then", "waits for you", or "when Review's agent ends" (the
 * nearest step before it that runs an agent). */
export function connectorOf(steps: Record<string, unknown>[], index: number): Connector {
  const step = steps[index] ?? {}
  const start = startOf(step)
  if (start === "manual") return { kind: "manual", text: "waits for you" }
  if (start === "agent_ended") {
    const agent = steps.slice(0, index).reverse().find(runsAnAgent)
    const name = agent ? String(agent.title || agent.id || "") : ""
    return { kind: "agent_ended", text: name ? `when ${name}'s agent ends` : "when the agent ends" }
  }
  if (index === 0) return { kind: "first", text: "" }
  return { kind: start, text: start === "automatic" ? "then" : String(start) }
}

/** idFromTitle is a step ID made from its title, not one of taken. */
export function idFromTitle(title: string, taken: string[]): string {
  const base = title.toLowerCase().normalize("NFKD").replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "").slice(0, 40).replace(/-+$/, "") || "step"
  let id = base
  for (let n = 2; taken.includes(id); n++) id = `${base}-${n}`
  return id
}

/** duplicateOf is a copy of a step under a new ID. */
export function duplicateOf(step: StepValue, taken: string[]): StepValue {
  const copy = structuredClone(step)
  const id = String(step.id ?? "step")
  let next = `${id}-2`
  for (let n = 3; taken.includes(next); n++) next = `${id}-${n}`
  return { ...copy, id: next, title: step.title ? `${step.title} (copy)` : undefined }
}
