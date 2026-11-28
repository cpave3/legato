import { useCallback, useState } from "react"
import { addInput, inputsOf, moveStep, pastedAs, removeStep, setTrigger, insertStep, WorkflowDocumentError, type WorkflowEdit, type WorkflowValues } from "../../lib/workflowDocument"
import type { Selection } from "../../lib/workflowAddress"
import { isTyping } from "./outlineRows"
import { pasteSteps } from "./pasteSteps"
import { brokenReferences, newStep, stepReferences, type BuilderOptions, type StepValue } from "./stepForms"
import { duplicateOf, idFromTitle } from "./stepRows"
import { newTrigger } from "./triggerForms"

export type Apply = (make: () => WorkflowEdit, how?: { typing?: boolean }) => void
type Select = (next: Selection, open?: boolean) => void
export type Adding = { kind: "step"; at: number } | { kind: "trigger" } | null

/** StepList is the file's steps: placed keeps a step that is not a mapping
 * in its place (it is no one's reader), steps are the mappings, ids
 * theirs. */
export type StepList = { placed: (StepValue | null)[]; steps: StepValue[]; ids: string[] }

export function stepListOf(placed: (StepValue | null)[]): StepList {
  const steps = placed.filter((step): step is StepValue => step !== null && typeof step === "object")
  return { placed, steps, ids: steps.map(step => step.id ?? "") }
}

/** readersOf are the steps that read a step's outputs. */
export const readersOf = (steps: StepValue[], id: string) => steps.filter(other => other.id !== id && stepReferences(other).some(ref => ref.id === id))

/** moveProblem is why moving a step to a place would break a reference,
 * or "" when it would not. */
export function moveProblem({ steps, ids }: StepList, id: string, to: number): string {
  const order = ids.filter(other => other !== id)
  order.splice(to, 0, id)
  const broken = brokenReferences(steps, order)
  return broken.length > 0 ? `That move would break a reference: ${broken.join("; ")}.` : ""
}

/** useApply makes each edit, telling onEdit a change and keeping why one
 * could not be made as error. */
export function useApply(text: string, onEdit: (edit: WorkflowEdit, how?: { typing?: boolean }) => void) {
  const [error, setError] = useState("")
  const apply: Apply = useCallback((make, how) => {
    try {
      setError("")
      const result = make()
      if (result.text !== text) onEdit(result, how)
    } catch (reason) {
      setError(reason instanceof WorkflowDocumentError ? reason.message : String(reason))
    }
  }, [text, onEdit])
  return { error, apply }
}

// pasteAndChoose pastes steps and, once the edit is in, chooses the first.
function pasteAndChoose(text: string, list: StepList, path: string | null, raws: Record<string, unknown>[], select: Select): WorkflowEdit {
  const { edit, added } = pasteSteps(text, list.placed, list.ids, path, raws)
  if (added[0]) setTimeout(() => select({ kind: "step", path: added[0] }), 0)
  return edit
}

type PasteOptions ={ editing: boolean; text: string; list: StepList; selection: Selection | null; apply: Apply; select: Select; onEdit: (edit: WorkflowEdit) => void }

/** usePaste: what is pasted on the forms (not into a field). A whole
 * workflow asks to replace this one (pasted, until replace or cancel);
 * steps go in after the step chosen. */
export function usePaste({ editing, text, list, selection, apply, select, onEdit }: PasteOptions) {
  const [pasted, setPasted] = useState<string | null>(null)
  const onPaste = (event: React.ClipboardEvent) => {
    if (!editing || isTyping(event.target)) return
    const clip = event.clipboardData.getData("text/plain")
    const what = pastedAs(clip)
    if (what.kind === "other") return
    event.preventDefault()
    if (what.kind === "workflow") { setPasted(clip); return }
    const path = selection?.kind === "step" ? selection.path : null
    apply(() => pasteAndChoose(text, list, path, what.steps, select))
  }
  const replace = () => {
    if (pasted === null) return
    setPasted(null)
    onEdit({ text: pasted, undo: { label: "Paste a workflow", before: text, after: pasted } })
  }
  return { pasted, onPaste, replace, cancel: () => setPasted(null) }
}

type StepOptions = { text: string; list: StepList; apply: Apply; select: Select; selection: Selection | null; onSelect: (selection: Selection | null) => void }

/** useStepActions adds, moves, duplicates and removes steps from the
 * list: a move that would break a reference is a warning, and removing a
 * step others read asks first (confirmRemove). */
export function useStepActions({ text, list, apply, select, selection, onSelect }: StepOptions) {
  const [confirmRemove, setConfirmRemove] = useState<string | null>(null)
  const [warning, setWarning] = useState("")
  // A step just added has its first essential field focused.
  const [focusNew, setFocusNew] = useState<string | null>(null)
  // A new step's ID is made from its title, and follows it until saved.
  const addStep = (type: string, at: number) => {
    const made = newStep(type, list.ids)
    const step = { ...made, id: idFromTitle(String(made.title ?? type), list.ids) }
    apply(() => insertStep(text, at, step))
    select({ kind: "step", path: step.id }, true)
    setFocusNew(step.id)
  }
  const moveTo = (id: string, to: number) => {
    const problem = moveProblem(list, id, to)
    setWarning(problem)
    if (!problem) apply(() => moveStep(text, id, to))
  }
  const removeIt = (id: string) => {
    if (readersOf(list.steps, id).length > 0 && confirmRemove !== id) { setConfirmRemove(id); return }
    setConfirmRemove(null)
    apply(() => removeStep(text, id))
    if (selection?.kind === "step" && selection.path === id) onSelect(null)
  }
  const duplicate = (step: StepValue, index: number) => {
    const copy = duplicateOf(step, list.ids)
    apply(() => insertStep(text, index + 1, copy))
    select({ kind: "step", path: String(copy.id) }, true)
  }
  return { addStep, moveTo, removeIt, duplicate, warning, confirmRemove, keep: () => setConfirmRemove(null), focusNew, focused: () => setFocusNew(null) }
}

// freeName is name, or name-2, name-3 and on, whichever is not taken.
export function freeName(name: string, taken: string[]): string {
  let free = name
  for (let n = 2; taken.includes(free); n++) free = `${name}-${n}`
  return free
}

type Options = {
  editing: boolean
  text: string
  values: WorkflowValues
  list: StepList
  selection: Selection | null
  onEdit: (edit: WorkflowEdit, how?: { typing?: boolean }) => void
  onSelect: (selection: Selection | null) => void
  choose: Select
  options: BuilderOptions
}

/** useOutlineEditing is what the outline's own controls change: the add
 * menus (adding), a step's menu, dragging, pasting, and the step, trigger
 * and input actions, each through apply. */
export function useOutlineEditing({ editing, text, values, list, selection, onEdit, onSelect, choose, options }: Options) {
  const { error, apply } = useApply(text, onEdit)
  const [adding, setAdding] = useState<Adding>(null)
  const [menu, setMenu] = useState<string | null>(null)
  const [dragged, setDragged] = useState<string | null>(null)
  const select: Select = (next, open = false) => {
    setAdding(null)
    choose(next, open)
  }
  const paste = usePaste({ editing, text, list, selection, apply, select, onEdit })
  const stepActions = useStepActions({ text, list, apply, select, selection, onSelect })
  const addTrigger = (kind: string) => {
    apply(() => setTrigger(text, kind, newTrigger(kind, options)))
    select({ kind: "trigger", name: kind }, true)
  }
  const addAnInput = () => {
    const name = freeName("input", Object.keys(inputsOf(values)))
    apply(() => addInput(text, name, { type: "string" }))
    select({ kind: "input", name }, true)
  }
  return { error, apply, adding, setAdding, menu, setMenu, dragged, setDragged, select, paste, ...stepActions, addTrigger, addAnInput }
}
