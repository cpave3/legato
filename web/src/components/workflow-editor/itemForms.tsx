import { useState } from "react"
import { Plus, Trash2 } from "lucide-react"
import { addInput, removeInput, renameInput, setField, type WorkflowEdit, type WorkflowValues, type YamlPath } from "../../lib/workflowDocument"
import { references } from "../../lib/workflowExpressions"
import { pathText } from "../../lib/workflowSchema"
import type { WorkflowDiagnostic } from "../../lib/workflowFiles"
import { Button } from "../ui/Button"
import { CommitField, Problems, ReferencePicker } from "./builderFields"
import { fieldClass, labelClass, problemsAt } from "./builderStyles"

// The forms of a workflow's details and of an input, which the builder
// draws all at once and the outline editor's inspector one at a time.

const field = fieldClass
const label = labelClass
const INPUT_TYPES = ["string", "integer", "boolean", "json", "task", "repository"] as const

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

type FormProps = { text: string; values: WorkflowValues; diagnostics: WorkflowDiagnostic[]; apply: (make: () => WorkflowEdit) => void }

function helpers({ text, diagnostics, apply }: FormProps) {
  const set = (path: YamlPath, value: unknown) => apply(() => setField(text, path, value === "" ? undefined : value))
  const at = (path: YamlPath) => problemsAt(diagnostics, pathText(path))
  return { set, at }
}

/** DetailsForm is the workflow's own fields: name, title, description,
 * run title and what opens after a run starts. */
export function DetailsForm(props: FormProps & { nameFixed?: boolean }) {
  const { values, nameFixed } = props
  const { set, at } = helpers(props)
  const described = (path: YamlPath) => at(path).length > 0 ? { "aria-invalid": true, "aria-describedby": `problem-${pathText(path)}` } : {}
  const runTitleRefs = references(values, { kind: "run_title" }).map(r => r.expression).filter(e => !e.endsWith("."))
  const text1 = (path: YamlPath, name: string, opts: { multiline?: boolean; readOnly?: boolean } = {}) => <label className={label}>{name}
    <CommitField aria-label={name} value={defaultText((values as Record<string, unknown>)[path[0] as string])} multiline={opts.multiline} readOnly={opts.readOnly} onCommit={value => set(path, value)} {...described(path)} />
    <Problems diagnostics={at(path)} id={`problem-${pathText(path)}`} />
  </label>
  return <>
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
  </>
}

/** InputCard is one input's form: its name, type, switches, default,
 * choices and description. */
export function InputCard(props: FormProps & { name: string; input: Record<string, unknown> | null | undefined }) {
  const { text, name, input, apply } = props
  const { set, at } = helpers(props)
  const type = String(input?.type ?? "string")
  const path = ["inputs", name]
  return <>
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
      <Choices name={name} type={type} choices={Array.isArray(input?.choices) ? input.choices : []} onChange={choices => set([...path, "choices"], choices.length === 0 ? undefined : choices)} />
    </div>
    <label className={label}>Description
      <CommitField aria-label={`Description of input ${name}`} value={defaultText(input?.description)} onCommit={value => set([...path, "description"], value)} />
    </label>
    <Problems diagnostics={at(path)} />
  </>
}

/** NewInputForm adds an input of a name, as a string to start with. */
export function NewInputForm({ text, apply, onAdded }: { text: string; apply: (make: () => WorkflowEdit) => void; onAdded?: (name: string) => void }) {
  const [newInput, setNewInput] = useState("")
  return <form className="flex items-end gap-2" onSubmit={event => { event.preventDefault(); const name = newInput.trim(); if (name) { apply(() => addInput(text, name, { type: "string" })); setNewInput(""); onAdded?.(name) } }}>
    <label className={`${label} flex-1`}>New input
      <input aria-label="New input name" className={field} value={newInput} placeholder="repo" onChange={event => setNewInput(event.target.value)} />
    </label>
    <Button type="submit" size="sm" variant="outline" disabled={!newInput.trim()}><Plus size={13} />Add input</Button>
  </form>
}

// Choices is an input's choices, one field each, so a choice with a comma
// in it stays one choice.
export function Choices({ name, type, choices, onChange }: { name: string; type: string; choices: unknown[]; onChange: (choices: unknown[]) => void }) {
  const [adding, setAdding] = useState("")
  return <fieldset className="flex flex-col gap-1 text-xs text-lw-text-2">
    <legend>Choices</legend>
    {choices.map((choice, i) => <div key={i} className="flex items-center gap-1">
      <CommitField aria-label={`Choice ${i + 1} of input ${name}`} value={defaultText(choice)} onCommit={value => onChange(choices.map((c, j) => j === i ? parseDefault(type, value) : c))} />
      <Button type="button" variant="ghost" size="sm" aria-label={`Remove choice ${i + 1} of input ${name}`} onClick={() => onChange(choices.filter((_, j) => j !== i))}><Trash2 size={13} /></Button>
    </div>)}
    <div className="flex items-center gap-1">
      <input aria-label={`New choice of input ${name}`} className={field} value={adding} onChange={event => setAdding(event.target.value)}
        onKeyDown={event => { if (event.key === "Enter" && adding !== "") { event.preventDefault(); onChange([...choices, parseDefault(type, adding)]); setAdding("") } }} />
      <Button type="button" variant="ghost" size="sm" aria-label={`Add a choice to input ${name}`} disabled={adding === ""} onClick={() => { onChange([...choices, parseDefault(type, adding)]); setAdding("") }}><Plus size={13} />Add choice</Button>
    </div>
  </fieldset>
}

