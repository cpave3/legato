import { Component, useCallback, useMemo, useRef, useState, type ReactNode } from "react"
import { Plus, Trash2 } from "lucide-react"
import { addInput, inputsOf, parseWorkflow, shapeProblem, removeInput, renameInput, setField, triggersOf, WorkflowDocumentError, type WorkflowEdit, type YamlPath } from "../../lib/workflowDocument"
import { references } from "../../lib/workflowExpressions"
import { pathText } from "../../lib/workflowSchema"
import type { WorkflowDiagnostic } from "../../lib/workflowFiles"
import { Button } from "../ui/Button"
import { PendingDrafts } from "./pendingDrafts"
import { CommitField, Problems, ReferencePicker } from "./builderFields"
import { fieldClass, labelClass, problemsAt } from "./builderStyles"
import { StepsEditor } from "./StepsEditor"
import { noOptions, type BuilderOptions } from "./stepForms"

export type WorkflowBuilderProps = {
  text: string
  /** onEdit is told each change the builder makes, as one undo step. */
  onEdit: (edit: WorkflowEdit) => void
  diagnostics?: WorkflowDiagnostic[]
  /** nameFixed: an existing workflow's name can't change (LEG-460). */
  nameFixed?: boolean
  onEditYaml: () => void
  /** onPending is told whether a field holds text not yet written. */
  onPending?: (pending: boolean) => void
  /** options are what the pickers offer. */
  options?: BuilderOptions
}

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

// WorkflowBuilder is the Visual side of the editor: the workflow's details
// and inputs as forms, over the same text the YAML side edits
// (lib/workflowDocument). Steps and triggers are edited in YAML until
// their sections land.
function Builder({ text, onEdit, diagnostics = [], nameFixed, onEditYaml, options = noOptions }: WorkflowBuilderProps) {
  const parsed = useMemo(() => parseWorkflow(text), [text])
  const [error, setError] = useState("")
  const [newInput, setNewInput] = useState("")
  if (parsed.errors.length > 0) {
    return <div role="alert" className="flex flex-col items-start gap-2 rounded-xl border border-lw-danger-line bg-lw-error-soft p-4 text-sm text-lw-danger">
      <p>The YAML doesn't parse, so it can't be shown here: {parsed.errors[0]}</p>
      <Button size="sm" variant="outline" onClick={onEditYaml}>Edit as YAML</Button>
    </div>
  }
  const shape = shapeProblem(parsed.values)
  if (shape) {
    return <div role="alert" className="flex flex-col items-start gap-2 rounded-xl border border-lw-line bg-lw-surface p-4 text-sm">
      <p>This file can't be shown as forms yet: {shape}.</p>
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
              <Choices name={name} type={type} choices={Array.isArray(input?.choices) ? input.choices : []} onChange={choices => set([...path, "choices"], choices.length === 0 ? undefined : choices)} />
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

    <StepsEditor text={text} values={values} diagnostics={diagnostics} options={options} apply={apply} />

    <section aria-label="Triggers" className="flex flex-col gap-2 rounded-xl border border-lw-line bg-lw-surface p-4">
      <h2 className="text-xs font-semibold uppercase tracking-wide text-lw-text-2">Triggers</h2>
      <p className="text-lw-text-2">Run by hand{Object.keys(triggersOf(values)).map(kind => `, ${kind}`).join("")}</p>
      <p className="text-xs text-lw-faint">Edit triggers in YAML for now.</p>
    </section>
  </div>
}

// Choices is an input's choices, one field each, so a choice with a comma
// in it stays one choice.
function Choices({ name, type, choices, onChange }: { name: string; type: string; choices: unknown[]; onChange: (choices: unknown[]) => void }) {
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

type BoundaryProps = { text: string; onEditYaml: () => void; children: ReactNode }

// Boundary keeps a form that fails to draw from taking the editor down: it
// says so and offers YAML, and tries again when the text changes.
class Boundary extends Component<BoundaryProps, { failed: string | null; text: string }> {
  state = { failed: null as string | null, text: this.props.text }
  static getDerivedStateFromError(error: unknown) { return { failed: error instanceof Error ? error.message : String(error) } }
  static getDerivedStateFromProps(props: BoundaryProps, state: { failed: string | null; text: string }) {
    return props.text !== state.text ? { failed: null, text: props.text } : null
  }
  render() {
    if (!this.state.failed) return this.props.children
    return <div role="alert" className="flex flex-col items-start gap-2 rounded-xl border border-lw-danger-line bg-lw-error-soft p-4 text-sm text-lw-danger">
      <p>The forms can't show this file: {this.state.failed}</p>
      <Button size="sm" variant="outline" onClick={this.props.onEditYaml}>Edit as YAML</Button>
    </div>
  }
}

export default function WorkflowBuilder(props: WorkflowBuilderProps) {
  const { onPending } = props
  const pending = useRef(new Set<string>())
  const report = useCallback((id: string, isPending: boolean) => {
    const had = pending.current.size > 0
    if (isPending) pending.current.add(id)
    else pending.current.delete(id)
    if (had !== pending.current.size > 0) onPending?.(pending.current.size > 0)
  }, [onPending])
  return <PendingDrafts.Provider value={report}>
    <Boundary text={props.text} onEditYaml={props.onEditYaml}><Builder {...props} /></Boundary>
  </PendingDrafts.Provider>
}
