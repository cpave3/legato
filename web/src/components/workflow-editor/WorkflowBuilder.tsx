import { useEffect, useMemo, useState } from "react"
import { Plus, Trash2 } from "lucide-react"
import { addInput, inputsOf, parseWorkflow, removeInput, renameInput, setField, stepsOf, triggersOf, WorkflowDocumentError, type WorkflowEdit, type YamlPath } from "../../lib/workflowDocument"
import { references } from "../../lib/workflowExpressions"
import { pathText } from "../../lib/workflowSchema"
import type { WorkflowDiagnostic } from "../../lib/workflowFiles"
import { Button } from "../ui/Button"

export type WorkflowBuilderProps = {
  text: string
  /** onEdit is told each change the builder makes, as one undo step. */
  onEdit: (edit: WorkflowEdit) => void
  diagnostics?: WorkflowDiagnostic[]
  /** nameFixed: an existing workflow's name can't change (LEG-460). */
  nameFixed?: boolean
  onEditYaml: () => void
}

const field = "min-h-9 w-full rounded-md border border-lw-line-strong bg-lw-ground px-2 text-sm text-lw-text"
const label = "flex flex-col gap-1 text-xs text-lw-text-2"
const INPUT_TYPES = ["string", "integer", "boolean", "json", "task", "repository"] as const

// problemsAt are the diagnostics on a path or under it.
function problemsAt(diagnostics: WorkflowDiagnostic[], path: string): WorkflowDiagnostic[] {
  return diagnostics.filter(d => d.path === path || d.path?.startsWith(path + ".") || d.path?.startsWith(path + "["))
}

function Problems({ diagnostics, id }: { diagnostics: WorkflowDiagnostic[]; id?: string }) {
  if (diagnostics.length === 0) return null
  return <ul id={id} className="flex flex-col gap-0.5 text-xs">
    {diagnostics.map((d, i) => <li key={i} className={d.severity === "error" ? "text-lw-danger" : "text-lw-needs"}>{d.message}</li>)}
  </ul>
}

// CommitField is a text field that writes its value when it is left or
// Enter is pressed, so a word typed is one undo step, not one a letter.
function CommitField({ value, onCommit, multiline, ...rest }: { value: string; onCommit: (value: string) => void; multiline?: boolean } & Omit<React.InputHTMLAttributes<HTMLInputElement>, "value" | "onChange">) {
  const [draft, setDraft] = useState(value)
  useEffect(() => setDraft(value), [value])
  const commit = () => { if (draft !== value) onCommit(draft) }
  if (multiline) {
    return <textarea aria-label={rest["aria-label"]} aria-describedby={rest["aria-describedby"]} aria-invalid={rest["aria-invalid"]} className={`${field} min-h-16 py-1.5`} value={draft} onChange={event => setDraft(event.target.value)} onBlur={commit} />
  }
  return <input {...rest} className={field} value={draft} onChange={event => setDraft(event.target.value)} onBlur={commit} onKeyDown={event => { if (event.key === "Enter") { event.preventDefault(); commit() } }} />
}

/** ReferencePicker inserts a ${{ }} that can be read at a place. */
function ReferencePicker({ options, onPick, name }: { options: string[]; onPick: (expression: string) => void; name: string }) {
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

function defaultText(value: unknown): string {
  if (value === undefined || value === null) return ""
  return typeof value === "string" ? value : JSON.stringify(value)
}

function parseDefault(type: string, text: string): unknown {
  if (text === "") return undefined
  if (type === "integer") return Number.isInteger(Number(text)) ? Number(text) : text
  if (type === "boolean") return text === "true" ? true : text === "false" ? false : text
  if (type === "json") { try { return JSON.parse(text) } catch { return text } }
  return text
}

// WorkflowBuilder is the Visual side of the editor: the workflow's details
// and inputs as forms, over the same text the YAML side edits
// (lib/workflowDocument). Steps and triggers are edited in YAML until
// their sections land.
export default function WorkflowBuilder({ text, onEdit, diagnostics = [], nameFixed, onEditYaml }: WorkflowBuilderProps) {
  const parsed = useMemo(() => parseWorkflow(text), [text])
  const [error, setError] = useState("")
  const [newInput, setNewInput] = useState("")
  if (parsed.errors.length > 0) {
    return <div role="alert" className="flex flex-col items-start gap-2 rounded-xl border border-lw-danger-line bg-lw-error-soft p-4 text-sm text-lw-danger">
      <p>The YAML doesn't parse, so it can't be shown here: {parsed.errors[0]}</p>
      <Button size="sm" variant="outline" onClick={onEditYaml}>Edit as YAML</Button>
    </div>
  }
  if (parsed.readOnly) {
    return <div role="alert" className="flex flex-col items-start gap-2 rounded-xl border border-lw-line bg-lw-surface p-4 text-sm">
      <p>This file can't be edited here: {parsed.readOnly}.</p>
      <Button size="sm" variant="outline" onClick={onEditYaml}>Edit as YAML</Button>
    </div>
  }
  const values = parsed.values
  const apply = (make: () => WorkflowEdit) => {
    try {
      setError("")
      const result = make()
      if (result.text !== text) onEdit(result)
    } catch (reason) {
      setError(reason instanceof WorkflowDocumentError ? reason.message : String(reason))
    }
  }
  const set = (path: YamlPath, value: unknown) => apply(() => setField(text, path, value === "" ? undefined : value))
  const at = (path: YamlPath) => problemsAt(diagnostics, pathText(path))
  const described = (path: YamlPath) => at(path).length > 0 ? { "aria-invalid": true, "aria-describedby": `problem-${pathText(path)}` } : {}
  const inputs = inputsOf(values)
  const runTitleRefs = references(values, { kind: "run_title" }).map(r => r.expression).filter(e => !e.endsWith("."))

  const text1 = (path: YamlPath, name: string, opts: { multiline?: boolean; readOnly?: boolean } = {}) => <label className={label}>{name}
    <CommitField aria-label={name} value={defaultText((values as Record<string, unknown>)[path[0] as string])} multiline={opts.multiline} readOnly={opts.readOnly} onCommit={value => set(path, value)} {...described(path)} />
    <Problems diagnostics={at(path)} id={`problem-${pathText(path)}`} />
  </label>

  return <div className="flex flex-col gap-5 text-sm">
    {error && <p role="alert" className="text-sm text-lw-danger">{error}</p>}
    <section aria-label="Details" className="flex flex-col gap-3 rounded-xl border border-lw-line bg-lw-surface p-4">
      <h2 className="text-xs font-semibold uppercase tracking-wide text-lw-text-2">Details</h2>
      <div className="grid gap-3 sm:grid-cols-2">
        {text1(["name"], "Name", { readOnly: nameFixed })}
        {text1(["title"], "Title")}
      </div>
      {nameFixed && <p className="-mt-2 text-xs text-lw-faint">A workflow's name is fixed once it is made.</p>}
      {text1(["description"], "Description", { multiline: true })}
      <div className="flex items-end gap-2">
        <div className="min-w-0 flex-1">{text1(["run_title"], "Run title")}</div>
        <ReferencePicker name="Run title" options={runTitleRefs} onPick={expression => set(["run_title"], `${values.run_title ?? ""}\${{ ${expression} }}`)} />
      </div>
      <label className={label}>After starting a run
        <select aria-label="After starting a run" className={field} value={values.after_start ?? ""} onChange={event => set(["after_start"], event.target.value)}>
          <option value="">Open the run (the default)</option>
          <option value="workflow">Open the run</option>
          <option value="agent">Open its agent</option>
          <option value="stay">Stay where I am</option>
        </select>
        <Problems diagnostics={at(["after_start"])} />
      </label>
    </section>

    <section aria-label="Inputs" className="flex flex-col gap-3 rounded-xl border border-lw-line bg-lw-surface p-4">
      <h2 className="text-xs font-semibold uppercase tracking-wide text-lw-text-2">Inputs</h2>
      {Object.keys(inputs).length === 0 && <p className="text-lw-text-2">This workflow takes no inputs.</p>}
      <ul className="flex flex-col gap-3">
        {Object.entries(inputs).map(([name, input]) => {
          const type = String(input?.type ?? "string")
          const path = ["inputs", name]
          return <li key={name} aria-label={`Input ${name}`} className="flex flex-col gap-2 rounded-lg border border-lw-line p-3">
            <div className="grid gap-2 sm:grid-cols-[1fr_10rem_auto]">
              <label className={label}>Name
                <CommitField aria-label={`Name of input ${name}`} value={name} onCommit={value => apply(() => renameInput(text, name, value.trim()))} />
              </label>
              <label className={label}>Type
                <select aria-label={`Type of input ${name}`} className={field} value={type} onChange={event => set([...path, "type"], event.target.value)}>
                  {INPUT_TYPES.map(t => <option key={t} value={t}>{t}</option>)}
                </select>
              </label>
              <Button type="button" variant="ghost" size="sm" className="self-end" aria-label={`Remove input ${name}`} onClick={() => apply(() => removeInput(text, name))}><Trash2 size={13} /></Button>
            </div>
            <div className="flex flex-wrap gap-4 text-xs text-lw-text-2">
              <label className="flex items-center gap-1.5"><input type="checkbox" aria-label={`Input ${name} is required`} checked={Boolean(input?.required)} onChange={event => set([...path, "required"], event.target.checked || undefined)} />Required</label>
              <label className="flex items-center gap-1.5"><input type="checkbox" aria-label={`Input ${name} is secret`} checked={Boolean(input?.secret)} onChange={event => set([...path, "secret"], event.target.checked || undefined)} />Secret</label>
            </div>
            <div className="grid gap-2 sm:grid-cols-2">
              <label className={label}>Default
                <CommitField aria-label={`Default of input ${name}`} value={defaultText(input?.default)} onCommit={value => set([...path, "default"], parseDefault(type, value))} />
              </label>
              <label className={label}>Choices, separated by commas
                <CommitField aria-label={`Choices of input ${name}`} value={Array.isArray(input?.choices) ? input.choices.join(", ") : ""} onCommit={value => set([...path, "choices"], value.trim() === "" ? undefined : value.split(",").map(choice => parseDefault(type, choice.trim())))} />
              </label>
            </div>
            <label className={label}>Description
              <CommitField aria-label={`Description of input ${name}`} value={defaultText(input?.description)} onCommit={value => set([...path, "description"], value)} />
            </label>
            <Problems diagnostics={at(path)} />
          </li>
        })}
      </ul>
      <form className="flex items-end gap-2" onSubmit={event => { event.preventDefault(); const name = newInput.trim(); if (name) { apply(() => addInput(text, name, { type: "string" })); setNewInput("") } }}>
        <label className={`${label} flex-1`}>New input
          <input aria-label="New input name" className={field} value={newInput} placeholder="repo" onChange={event => setNewInput(event.target.value)} />
        </label>
        <Button type="submit" size="sm" variant="outline" disabled={!newInput.trim()}><Plus size={13} />Add input</Button>
      </form>
    </section>

    <section aria-label="Steps" className="flex flex-col gap-2 rounded-xl border border-lw-line bg-lw-surface p-4">
      <h2 className="text-xs font-semibold uppercase tracking-wide text-lw-text-2">Steps</h2>
      <ol className="list-decimal pl-5 text-lw-text-2">{stepsOf(values).map((step, i) => <li key={`${step.id}-${i}`}><span className="text-lw-text">{String(step.title ?? step.id)}</span> <span className="font-lw-mono text-xs">{String(step.type ?? "")}</span></li>)}</ol>
      <p className="text-xs text-lw-faint">Edit steps in YAML for now. <button type="button" className="text-lw-link hover:underline" onClick={onEditYaml}>Switch to YAML</button></p>
    </section>

    <section aria-label="Triggers" className="flex flex-col gap-2 rounded-xl border border-lw-line bg-lw-surface p-4">
      <h2 className="text-xs font-semibold uppercase tracking-wide text-lw-text-2">Triggers</h2>
      <p className="text-lw-text-2">Run by hand{Object.keys(triggersOf(values)).map(kind => `, ${kind}`).join("")}</p>
      <p className="text-xs text-lw-faint">Edit triggers in YAML for now.</p>
    </section>
  </div>
}
