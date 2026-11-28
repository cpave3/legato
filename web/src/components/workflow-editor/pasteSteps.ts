import { insertStep, type WorkflowEdit } from "../../lib/workflowDocument"
import type { StepValue } from "./stepForms"
import { idFromTitle } from "./stepRows"

type Raw = Record<string, unknown>

/** pastedIds are pasted steps' IDs: each its own when it has one, else
 * made from its title or type, numbered past any taken. */
export function pastedIds(raws: Raw[], ids: string[]): string[] {
  const taken = [...ids]
  return raws.map(raw => {
    const base = typeof raw.id === "string" && raw.id !== "" ? raw.id : idFromTitle(String(raw.title ?? raw.type ?? "step"), taken)
    let id = base
    for (let n = 2; taken.includes(id); n++) id = `${base}-${n}`
    taken.push(id)
    return id
  })
}

// renameReferences points a string's steps.<id>.outputs references at the
// renamed IDs, inside its expressions only.
function renameReferences(value: string, renamed: Map<string, string>): string {
  if (renamed.size === 0) return value
  return value.replace(/\$\{\{[^}]*\}\}/g, expression => expression.replace(/(\bsteps\.)([^.\s}]+)(?=\.outputs\b)/g, (all, head: string, id: string) => renamed.has(id) ? head + renamed.get(id) : all))
}

/** followRenamed is a pasted value with its references to the group's own
 * renamed steps following them; a reference to a step not pasted is left
 * alone. */
export function followRenamed(value: unknown, renamed: Map<string, string>): unknown {
  if (typeof value === "string") return renameReferences(value, renamed)
  if (Array.isArray(value)) return value.map(item => followRenamed(item, renamed))
  if (value && typeof value === "object") return Object.fromEntries(Object.entries(value).map(([k, v]) => [k, followRenamed(v, renamed)]))
  return value
}

/** pasteSteps inserts pasted steps after the step chosen (path), or at the
 * end: the IDs first, then the group's references to its own renamed
 * steps follow them. */
export function pasteSteps(text: string, placed: (StepValue | null)[], ids: string[], path: string | null, raws: Raw[]): { edit: WorkflowEdit; added: string[] } {
  const after = path === null ? placed.length : placed.findIndex(step => step?.id === path.split("/")[0]) + 1
  const added = pastedIds(raws, ids)
  const renamed = new Map(raws.flatMap((raw, i) => typeof raw.id === "string" && raw.id !== added[i] ? [[raw.id, added[i]] as const] : []))
  let working = text
  raws.forEach((raw, i) => {
    working = insertStep(working, (after <= 0 ? placed.length : after) + i, { ...(followRenamed(raw, renamed) as Raw), id: added[i] }).text
  })
  const label = `Paste ${added.length === 1 ? `step ${added[0]}` : `${added.length} steps`}`
  return { edit: { text: working, undo: { label, before: text, after: working } }, added }
}
