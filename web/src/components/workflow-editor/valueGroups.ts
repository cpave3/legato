import { references, type ExpressionDefinition, type ExpressionLocation } from "../../lib/workflowExpressions"

// The values a field can read (LEG-510), from workflowExpressions'
// references (backward only, as the engine checks them), grouped by where
// they come from: This run (inputs, the task), What started it (the
// trigger's context), then each earlier step by its title.

export type ValueEntry = { expression: string; name: string; type: string; description: string; wrongType?: string }
export type ValueGroup = { group: string; entries: ValueEntry[] }

/** ValueSource is what of a workflow the picker reads: its inputs, and its
 * steps' IDs, titles and outputs. */
export type ValueSource = { inputs?: ExpressionDefinition["inputs"]; steps?: ({ id?: string; title?: string; outputs?: Record<string, { type?: string; description?: string } | null | undefined> | null } | null)[] | null }
type Values = ValueSource

const KIND_NAMES: Record<string, string> = { integer: "a whole number", boolean: "true or false" }
const TYPE_NAMES: Record<string, string> = { string: "text", integer: "a whole number", boolean: "a boolean", json: "JSON", file: "a file", task: "a task", repository: "a repository" }

// wrongType says why a value of type does not suit a field of kind.
function wrongTypeFor(type: string, kind: string): string | undefined {
  const wanted = KIND_NAMES[kind]
  if (!wanted || type === "" || type === "json") return undefined
  if (kind === "integer" && type === "integer") return undefined
  if (kind === "boolean" && type === "boolean") return undefined
  return `${TYPE_NAMES[type] ?? type}, not ${wanted}`
}

/** valueGroups are the values readable at location, by where they come
 * from, each named plainly with its type, those of the wrong type for a
 * field of kind marked with why. */
export function valueGroups(values: Values, location: ExpressionLocation, kind: string): ValueGroup[] {
  const inputs = values.inputs ?? {}
  const steps = values.steps ?? []
  const run: ValueEntry[] = []
  const started: ValueEntry[] = []
  const byStep = new Map<string, ValueEntry[]>()
  for (const ref of references(values, location)) {
    const parts = ref.expression.split(".")
    let entry: ValueEntry
    if (parts[0] === "workflow" && parts[1] === "inputs") {
      const input = inputs[parts[2]]
      const field = parts.slice(3).filter(Boolean).join(".")
      entry = { expression: ref.expression, name: field ? `${parts[2]} · ${field}` : parts[2], type: field ? "" : String(input?.type ?? ""), description: field ? ref.description : String(input?.description ?? ref.description) }
      run.push(entry)
    } else if (parts[0] === "workflow" && parts[1] === "task") {
      const field = parts.slice(2).filter(Boolean).join(".")
      entry = { expression: ref.expression, name: field ? `The workflow's task · ${field}` : "The workflow's task", type: field ? "" : "task", description: ref.description }
      run.push(entry)
    } else if (parts[0] === "steps") {
      const step = steps.find(s => s?.id === parts[1])
      const output = step?.outputs?.[parts[3]]
      entry = { expression: ref.expression, name: parts.slice(3).join("."), type: String(output?.type ?? ""), description: String(output?.description || ref.description) }
      const title = String(step?.title || parts[1])
      byStep.set(title, [...(byStep.get(title) ?? []), entry])
    } else {
      entry = { expression: ref.expression, name: ref.expression, type: "", description: ref.description }
      started.push(entry)
    }
    const wrong = wrongTypeFor(entry.type, kind)
    if (wrong) entry.wrongType = wrong
  }
  const groups: ValueGroup[] = [{ group: "This run", entries: run }, { group: "What started it", entries: started }, ...[...byStep].map(([group, entries]) => ({ group, entries }))]
  return groups.filter(g => g.entries.length > 0)
}
