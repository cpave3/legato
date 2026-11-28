import type { WorkflowDefinition, WorkflowInput, WorkflowRepositoryPreset, WorkflowRunView } from "./workflow"

// Starting a workflow from its inputs' form, apart from the dialog: the
// inputs it asks for, their first values, what a start sends, and where it
// leads.

export type Inputs = Record<string, WorkflowInput>

/** shownInputs are a workflow's inputs but those left out of the form. */
export function shownInputs(definition: WorkflowDefinition, hidden: string[]): Inputs {
  return Object.fromEntries(Object.entries(definition.inputs ?? {}).filter(([key]) => !hidden.includes(key)))
}

// firstValueOf is an input's first value: the one given, its default, its
// first choice, or false for a switch.
function firstValueOf(input: WorkflowInput, given: string | undefined): unknown {
  return given ?? input.default ?? (input.choices?.[0]) ?? (input.type === "boolean" ? false : undefined)
}

/** firstValues are the inputs' first values as text, for those that have
 * one. */
export function firstValues(inputs: Inputs, given: Record<string, string>): Record<string, string> {
  return Object.fromEntries(Object.entries(inputs).flatMap(([key, input]) => {
    const value = firstValueOf(input, given[key])
    return value == null ? [] : [[key, String(value)]]
  }))
}

/** missingInput is the first required input with no value and no default. */
export function missingInput(inputs: Inputs, values: Record<string, string>): string | undefined {
  return Object.entries(inputs).find(([key, input]) => input.required && !values[key] && input.default == null)?.[0]
}

/** declaredValues are the values of the inputs the form asks for. */
export function declaredValues(values: Record<string, string>, inputs: Inputs): Record<string, string> {
  return Object.fromEntries(Object.entries(values).filter(([key]) => key in inputs))
}

/** shownValue is an input's value as its field shows it: its own, else its
 * default. */
export const shownValue = (values: Record<string, string>, key: string, input: WorkflowInput) => values[key] ?? String(input.default ?? "")

export const REPOSITORY_KEYS = ["repo", "primary_dir", "base_branch"] as const

/** presetValues are a repository preset's values for the repository
 * inputs the form has. */
export function presetValues(preset: WorkflowRepositoryPreset, keys: string[]): Record<string, string> {
  const of: Record<string, string> = { repo: preset.repo, primary_dir: preset.primary_dir, base_branch: preset.base_branch ?? "" }
  return Object.fromEntries(keys.map(key => [key, of[key]]))
}

export type Refusal = { error: string; link: { to: string; label: string } }

const LOOP_CODES = ["review_loop_active", "author_loop_active"]

/** refusalOf is why a start was refused with a link (review_loop_active
 * names the loop that holds the pull request), or null. */
export function refusalOf(view: WorkflowRunView): Refusal | null {
  if (view.run.status !== "failed") return null
  const refused = view.steps?.find(step => step.status === "failed" && step.error_link)
  if (!refused) return null
  const label = LOOP_CODES.includes(refused.error_code ?? "") ? "Open the review loop" : "Open"
  return { error: refused.error ?? view.run.error ?? "The workflow could not start", link: { to: refused.error_link ?? "", label } }
}

/** afterStartPath is where a start leads: the task's agent, nowhere (stay,
 * null), or the run. */
export function afterStartPath(definition: WorkflowDefinition, inputs: Inputs, declared: Record<string, string>, runId: string): string | null {
  if (definition.after_start === "stay") return null
  if (definition.after_start !== "agent") return `/workflows/runs/${runId}`
  const taskInput = Object.entries(inputs).find(([, input]) => input.type === "task")?.[0]
  return `/agents?task=${encodeURIComponent(taskInput ? declared[taskInput] : "")}`
}
