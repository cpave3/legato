import { useContext, useEffect, useId, useState } from "react"
import { PendingDrafts } from "./pendingDrafts"
import type { WorkflowDiagnostic } from "../../lib/workflowFiles"
import { Button } from "../ui/Button"
import { fieldClass } from "./builderStyles"

export function Problems({ diagnostics, id }: { diagnostics: WorkflowDiagnostic[]; id?: string }) {
  if (diagnostics.length === 0) return null
  return <ul id={id} className="flex flex-col gap-0.5 text-xs">
    {diagnostics.map((d, i) => <li key={i} className={d.severity === "error" ? "text-lw-danger" : "text-lw-needs"}>{d.message}</li>)}
  </ul>
}

type CommitFieldProps = { value: string; onCommit: (value: string) => void; multiline?: boolean; list?: string } & Omit<React.InputHTMLAttributes<HTMLInputElement>, "value" | "onChange" | "list">

// CommitField is a text field that writes its value when it is left or
// Enter is pressed, so a word typed is one undo step, not one a letter.
export function CommitField({ value, onCommit, multiline, list, ...rest }: CommitFieldProps) {
  const [draft, setDraft] = useState(value)
  useEffect(() => setDraft(value), [value])
  // Text typed and not yet written counts as unsaved on the page.
  const report = useContext(PendingDrafts)
  const id = useId()
  useEffect(() => { report(id, draft !== value) }, [report, id, draft, value])
  useEffect(() => () => report(id, false), [report, id])
  const commit = () => { if (draft !== value) onCommit(draft) }
  if (multiline) {
    return <textarea aria-label={rest["aria-label"]} aria-describedby={rest["aria-describedby"]} aria-invalid={rest["aria-invalid"]} readOnly={rest.readOnly} className={`${fieldClass} min-h-20 py-1.5 font-lw-mono text-xs`} value={draft} onChange={event => setDraft(event.target.value)} onBlur={commit} />
  }
  return <input {...rest} list={list} className={fieldClass} value={draft} onChange={event => setDraft(event.target.value)} onBlur={commit} onKeyDown={event => { if (event.key === "Enter") { event.preventDefault(); commit() } }} />
}

/** ReferencePicker inserts a ${{ }} that can be read at a place. */
export function ReferencePicker({ options, onPick, name }: { options: string[]; onPick: (expression: string) => void; name: string }) {
  const [open, setOpen] = useState(false)
  if (options.length === 0) return null
  return <div className="relative">
    <Button type="button" variant="ghost" size="sm" aria-expanded={open} aria-label={`Insert a reference into ${name}`} onClick={() => setOpen(!open)}>{"${{ }}"}</Button>
    {open && <ul role="listbox" aria-label={`References for ${name}`} className="absolute right-0 z-10 mt-1 max-h-60 w-72 overflow-auto rounded-md border border-lw-line bg-lw-surface p-1 text-xs shadow-xl">
      {options.map(option => <li key={option} role="option" aria-selected="false" tabIndex={0} className="cursor-pointer rounded px-2 py-1 font-lw-mono hover:bg-lw-raised"
        onClick={() => { onPick(option); setOpen(false) }} onKeyDown={event => { if (event.key === "Enter") { onPick(option); setOpen(false) } }}>{option}</li>)}
    </ul>}
  </div>
}
