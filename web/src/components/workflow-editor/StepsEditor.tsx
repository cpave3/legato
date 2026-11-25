import { useState } from "react"
import { ArrowDown, ArrowUp, ChevronDown, ChevronRight, GripVertical, Maximize2, Plus, Trash2 } from "lucide-react"
import { insertStep, moveStep, removeStep, renameStep, setField, stepsOf, type WorkflowEdit, type WorkflowValues, type YamlPath } from "../../lib/workflowDocument"
import { checkExpression, references, type ExpressionLocation } from "../../lib/workflowExpressions"
import { valueGroups, type ValueGroup, type ValueSource } from "./valueGroups"
import { ExpressionText, UseValueFrom } from "./ValuePicker"
import { executors, pathText } from "../../lib/workflowSchema"
import { brokenReferences, newStep, stepForm, stepReferences, STEP_KEYS, type BuilderOptions, type FieldSpec, type StepValue } from "./stepForms"
import type { WorkflowDiagnostic } from "../../lib/workflowFiles"
import { Button } from "../ui/Button"
import { CommitField, Problems, ReferencePicker } from "./builderFields"
import { fieldClass, labelClass, problemsAt, valueText } from "./builderStyles"

type Props = {
  text: string
  values: WorkflowValues
  diagnostics: WorkflowDiagnostic[]
  options: BuilderOptions
  apply: (make: () => WorkflowEdit) => void
}

// StepsEditor is the Steps section of the builder: the steps in order,
// each a card with its form, added from a palette of the step types,
// moved by drag or by its buttons, and removed.
export function StepsEditor({ text, values, diagnostics, options, apply }: Props) {
  const steps = stepsOf(values) as StepValue[]
  const [open, setOpen] = useState<Record<string, boolean>>({})
  const [adding, setAdding] = useState(false)
  const [warning, setWarning] = useState("")
  const [confirmRemove, setConfirmRemove] = useState<string | null>(null)
  const [dragged, setDragged] = useState<string | null>(null)
  const ids = steps.map(step => step.id ?? "")

  const move = (id: string, to: number) => {
    const order = ids.filter(other => other !== id)
    order.splice(to, 0, id)
    const broken = brokenReferences(steps, order)
    if (broken.length > 0) { setWarning(`That move would break a reference: ${broken.join("; ")}.`); return }
    setWarning("")
    apply(() => moveStep(text, id, to))
  }
  const remove = (id: string) => {
    const readers = steps.filter(step => step.id !== id && stepReferences(step).some(ref => ref.id === id)).map(step => step.id)
    if (readers.length > 0 && confirmRemove !== id) { setConfirmRemove(id); return }
    setConfirmRemove(null)
    apply(() => removeStep(text, id))
  }
  const add = (type: string) => {
    const step = newStep(type, ids)
    apply(() => insertStep(text, steps.length, step))
    setOpen({ ...open, [step.id!]: true })
    setAdding(false)
  }

  return <section aria-label="Steps" className="flex flex-col gap-3 rounded-xl border border-lw-line bg-lw-surface p-4">
    <h2 className="text-xs font-semibold uppercase tracking-wide text-lw-text-2">Steps</h2>
    {warning && <p role="alert" className="text-sm text-lw-needs">{warning}</p>}
    {steps.length === 0 && <p className="text-lw-text-2">No steps yet: a workflow needs at least one.</p>}
    <ol aria-label="Steps" className="flex flex-col gap-2">
      {steps.map((step, index) => {
        const id = step.id ?? `#${index + 1}`
        const expanded = open[id] ?? steps.length <= 3
        const readers = steps.filter(other => other.id !== step.id && stepReferences(other).some(ref => ref.id === step.id)).map(other => other.id)
        return <li key={`${id}-${index}`} aria-label={`Step ${index + 1}: ${step.title ?? id}`} className="rounded-lg border border-lw-line bg-lw-ground"
          draggable onDragStart={() => setDragged(step.id ?? null)} onDragOver={event => event.preventDefault()}
          onDrop={event => { event.preventDefault(); if (dragged && dragged !== step.id) move(dragged, index); setDragged(null) }}>
          <div className="flex items-center gap-1 px-2 py-1.5">
            <GripVertical size={14} aria-hidden className="cursor-grab text-lw-faint" />
            <button type="button" aria-expanded={expanded} aria-label={`${expanded ? "Fold" : "Open"} step ${id}`} className="flex min-w-0 flex-1 items-center gap-1.5 text-left" onClick={() => setOpen({ ...open, [id]: !expanded })}>
              {expanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
              <span className="truncate font-medium">{index + 1}. {String(step.title ?? id)}</span>
              <span className="truncate font-lw-mono text-xs text-lw-faint">{String(step.type ?? "")}</span>
            </button>
            <Button type="button" variant="ghost" size="icon-sm" aria-label={`Move step ${id} up`} disabled={index === 0} onClick={() => move(id, index - 1)}><ArrowUp size={13} /></Button>
            <Button type="button" variant="ghost" size="icon-sm" aria-label={`Move step ${id} down`} disabled={index === steps.length - 1} onClick={() => move(id, index + 1)}><ArrowDown size={13} /></Button>
            <Button type="button" variant="ghost" size="icon-sm" aria-label={`Remove step ${id}`} onClick={() => remove(id)}><Trash2 size={13} /></Button>
          </div>
          {confirmRemove === id && <div role="alertdialog" aria-label={`Remove step ${id}`} className="mx-2 mb-2 flex flex-wrap items-center gap-2 rounded border border-lw-needs p-2 text-xs">
            <span>{readers.join(", ")} {readers.length === 1 ? "reads" : "read"} this step's outputs.</span>
            <Button type="button" size="sm" variant="destructive" onClick={() => remove(id)}>Remove anyway</Button>
            <Button type="button" size="sm" variant="ghost" onClick={() => setConfirmRemove(null)}>Keep it</Button>
          </div>}
          {expanded && <StepCard step={step} index={index} text={text} values={values} diagnostics={diagnostics} options={options} apply={apply} taken={ids} />}
        </li>
      })}
    </ol>
    {adding
      ? <div role="group" aria-label="Step types" className="grid gap-2 sm:grid-cols-2">
        {executors.map(e => <button key={e.type} type="button" className="flex flex-col items-start gap-0.5 rounded-lg border border-lw-line p-2 text-left hover:bg-lw-raised focus-visible:outline focus-visible:outline-2 focus-visible:outline-lw-accent" onClick={() => add(e.type)}>
          <span className="text-sm font-medium">{e.title}</span>
          <span className="font-lw-mono text-[11px] text-lw-faint">{e.type}</span>
          <span className="text-xs text-lw-text-2">{e.description}</span>
        </button>)}
        <Button type="button" size="sm" variant="ghost" onClick={() => setAdding(false)}>Cancel</Button>
      </div>
      : <Button type="button" size="sm" variant="outline" className="self-start" onClick={() => setAdding(true)}><Plus size={13} />Add step</Button>}
  </section>
}

/** StepCard is one step's form. parts draws its setup (ID, title, how it
 * starts and its fields), its outputs, or both; titleInHeader leaves the
 * title to the inspector's header. */
export function StepCard({ step, index, text, values, diagnostics, options, apply, taken, parts = "all", titleInHeader = false }: Omit<Props, "options"> & { options: BuilderOptions; step: StepValue; index: number; taken: string[]; parts?: "all" | "setup" | "outputs"; titleInHeader?: boolean }) {
  const path: YamlPath = ["steps", index]
  const set = (sub: YamlPath, value: unknown) => apply(() => setField(text, [...path, ...sub], value === "" ? undefined : value))
  const setWith = (name: string, value: unknown) => apply(() => setField(text, [...path, "with", name], value))
  const at = (sub: YamlPath) => problemsAt(diagnostics, pathText([...path, ...sub]))
  const form = step.type ? stepForm(step.type) : null
  const withValues = step.with ?? {}
  const refs = references(values, { kind: "with", step: index }).map(r => r.expression).filter(e => !e.endsWith("."))
  const taskRefs = ["${{ workflow.task }}", ...Object.entries(values.inputs ?? {}).filter(([, input]) => input?.type === "task").map(([name]) => `\${{ workflow.inputs.${name} }}`)]
  const grouped = new Set((form?.groups ?? []).filter(g => g.kind === "exactly_one").flatMap(g => g.fields))
  const others = [...Object.keys(withValues).filter(key => !form?.fields.some(f => f.name === key)).map(key => `with.${key}`), ...Object.keys(step).filter(key => !STEP_KEYS.includes(key))]
  const id = step.id ?? ""
  const outputs = <OutputsEditor step={step} id={id} path={path} text={text} apply={apply} problems={at(["outputs"])} taken={taken} />
  if (parts === "outputs") return <div className="flex flex-col gap-3">{outputs}</div>

  return <div className={parts === "all" ? "flex flex-col gap-3 border-t border-lw-line p-3" : "flex flex-col gap-3"}>
    <div className={titleInHeader ? "grid gap-2 sm:grid-cols-2" : "grid gap-2 sm:grid-cols-3"}>
      <StepIdField step={step} index={index} text={text} values={values} apply={apply} diagnostics={diagnostics} />
      {!titleInHeader && <label className={labelClass}>Title
        <CommitField aria-label={`Title of step ${id}`} value={valueText(step.title)} onCommit={value => set(["title"], value)} />
      </label>}
      <label className={labelClass}>Starts
        <select aria-label={`How step ${id} starts`} className={fieldClass} value={step.trigger ?? "automatic"} onChange={event => set(["trigger"], event.target.value === "automatic" ? undefined : event.target.value)}>
          <option value="automatic">By itself</option>
          <option value="manual">When you start it</option>
          <option value="agent_ended">When the agent ends</option>
        </select>
      </label>
    </div>
    <p className="text-xs text-lw-faint">{executors.find(e => e.type === step.type)?.description ?? `${step.type}: a step type this Legato doesn't know.`}</p>
    <Problems diagnostics={at(["type"])} />
    {form && <div className="flex flex-col gap-2">
      {(form.groups.filter(g => g.kind === "exactly_one")).map(group => {
        const chosen = group.fields.find(f => f in withValues) ?? group.fields[0]
        const spec = form.fields.find(f => f.name === chosen)!
        return <div key={group.fields.join()} className="flex flex-col gap-1.5">
          <div role="radiogroup" aria-label={`Step ${id}: ${group.fields.join(" or ")}`} className="inline-flex self-start rounded-md border border-lw-line p-0.5">
            {group.fields.map(name => <button key={name} type="button" role="radio" aria-checked={chosen === name} className={`min-h-7 rounded px-2.5 text-xs capitalize ${chosen === name ? "bg-lw-raised text-lw-text" : "text-lw-text-2"}`}
              onClick={() => { if (chosen !== name) apply(() => setField(setField(text, [...path, "with", chosen], undefined).text, [...path, "with", name], "")) }}>{name}</button>)}
          </div>
          <WithField spec={spec} stepId={id} value={withValues[chosen]} onChange={value => setWith(chosen, value)} refs={refs} taskRefs={taskRefs} options={options} agent={String(withValues.agent ?? "")} problems={at(["with", chosen])} />
        </div>
      })}
      {form.fields.filter(f => !grouped.has(f.name)).map(spec => <WithField key={spec.name} spec={spec} stepId={id} value={withValues[spec.name]} onChange={value => setWith(spec.name, value)} refs={refs} taskRefs={taskRefs} options={options} agent={String(withValues.agent ?? "")} problems={at(["with", spec.name])} />)}
    </div>}
    {others.length > 0 && <p className="text-xs text-lw-text-2">Other fields (edit in YAML): <span className="font-lw-mono">{others.join(", ")}</span></p>}
    {parts === "all" && outputs}
  </div>
}

/** StepIdField is a step's ID. Renaming the ID of a step others read
 * offers to change their references too. */
export function StepIdField({ step, index, text, values, apply, diagnostics, hint }: { step: StepValue; index: number; text: string; values: WorkflowValues; apply: Props["apply"]; diagnostics: WorkflowDiagnostic[]; hint?: string }) {
  const id = step.id ?? ""
  const path: YamlPath = ["steps", index]
  const [renaming, setRenaming] = useState<string | null>(null)
  const readers = (values.steps ?? []).filter(other => other && other.id !== id && stepReferences(other as StepValue).some(ref => ref.id === id)).map(other => String(other!.id))
  const rename = (to: string) => { if (to === id || to === "") return; if (readers.length > 0) setRenaming(to); else apply(() => renameStep(text, id, to)) }
  return <label className={labelClass}>ID
    <CommitField aria-label={`ID of step ${id}`} value={id} onCommit={value => rename(value.trim())} />
    {hint && <span className="text-[11px] text-lw-faint">{hint}</span>}
    <Problems diagnostics={problemsAt(diagnostics, pathText([...path, "id"]))} />
    {renaming !== null && <span role="alertdialog" aria-label={`Rename step ${id}`} className="flex flex-col gap-1.5 rounded border border-lw-needs p-2 text-xs text-lw-text">
      <span>{readers.join(", ")} {readers.length === 1 ? "reads" : "read"} this step's outputs. Change {readers.length === 1 ? "its reference" : "their references"} to {renaming} too?</span>
      <span className="flex flex-wrap gap-1">
        <Button type="button" size="sm" onClick={() => { apply(() => renameStep(text, id, renaming)); setRenaming(null) }}>Change them too</Button>
        <Button type="button" size="sm" variant="outline" onClick={() => { apply(() => setField(text, [...path, "id"], renaming)); setRenaming(null) }}>Only the ID</Button>
        <Button type="button" size="sm" variant="ghost" onClick={() => setRenaming(null)}>Cancel</Button>
      </span>
    </span>}
  </label>
}

type WithFieldProps = { spec: FieldSpec; stepId: string; value: unknown; onChange: (value: unknown) => void; refs: string[]; taskRefs: string[]; options: BuilderOptions; agent: string; problems: WorkflowDiagnostic[]; rich?: boolean
  /** reads is where the field is, for the inspector's value picker. */
  reads?: { values: ValueSource; location: ExpressionLocation } }

// WithField draws one with field by its kind. Every kind is drawn; a value
// that is an expression is shown as text, so it can always be edited.
export function WithField({ spec, stepId, value, onChange, refs, taskRefs, options, agent, problems, rich = false, reads }: WithFieldProps) {
  const name = `${spec.name} of step ${stepId}`
  // A number field holding text that is neither a whole number nor an
  // expression: the engine may only find out when the step runs.
  const notANumber = spec.kind === "integer" && typeof value === "string" && value.trim() !== "" && !value.includes("${{") && !/^-?\d+$/.test(value.trim())
  const text = valueText(value)
  const isExpression = typeof value === "string" && value.includes("${{")
  const listId = `options-${stepId}-${spec.name}`
  const set = (raw: string) => onChange(raw === "" && !spec.required ? undefined : raw)
  // Buttons take references row by row, in each value.
  const picker = spec.expressions && !rich && spec.kind !== "chat_buttons" && <ReferencePicker name={name} options={refs} onPick={expression => onChange(`${text}\${{ ${expression} }}`)} />
  let control: React.ReactNode
  const suggestions = spec.kind === "macro" ? options.macros : spec.kind === "agent" ? options.agents : spec.kind === "tier" ? (options.tiers[agent] ?? Object.values(options.tiers).flat()) : spec.kind === "column" ? options.columns : spec.kind === "repository" ? options.repositories ?? [] : spec.kind === "chat_bot" ? options.bots ?? [] : []
  const groups = reads && spec.expressions ? valueGroups(reads.values, reads.location, spec.kind) : null
  const check = (expression: string) => reads ? checkExpression(reads.values, reads.location, expression) : null
  // In the inspector, a text field takes values at its cursor (LEG-510).
  const textKinds = ["string", "text", "path", "repository", "column", "macro", "tier", "agent", "chat_bot", "task_or_id"]
  const expressionText = rich && groups && textKinds.includes(spec.kind) && !(spec.enum && !isExpression)
  const richControl = rich && !isExpression ? richControlOf({ spec, name, value, text, onChange, set, options, groups, check }) : null
  switch (richControl ? "rich" : expressionText ? "expression" : spec.kind) {
    case "rich":
      control = richControl
      break
    case "expression":
      control = <ExpressionText name={name} label={spec.label} value={text} multiline={spec.kind === "text"} groups={groups!} check={check} invalid={notANumber || undefined} onCommit={set} list={suggestions.length > 0 ? listId : undefined} />
      break
    case "text":
      control = <CommitField aria-label={name} multiline value={text} onCommit={set} />
      break
    case "boolean":
      control = isExpression
        ? <CommitField aria-label={name} value={text} onCommit={set} />
        : <input type="checkbox" aria-label={name} checked={value === true} onChange={event => onChange(event.target.checked ? true : spec.required ? false : undefined)} />
      break
    case "integer":
      // A whole number is written as one; anything else (an expression) as text.
      control = <CommitField aria-label={name} inputMode="numeric" value={text} aria-invalid={notANumber || undefined} onCommit={raw => /^-?\d+$/.test(raw.trim()) ? onChange(Number(raw.trim())) : set(raw)} />
      break
    case "json":
      control = <CommitField aria-label={name} multiline value={value === undefined ? "" : typeof value === "string" ? value : JSON.stringify(value, null, 2)} onCommit={raw => { if (raw.trim() === "") { onChange(undefined); return } try { onChange(JSON.parse(raw)) } catch { onChange(raw) } }} />
      break
    case "chat_buttons":
      control = isExpression
        ? <CommitField aria-label={name} value={text} onCommit={set} />
        : <ChatButtonsField name={name} stepId={stepId} value={value} refs={refs} onChange={onChange} />
      break
    case "task":
      control = <select aria-label={name} className={fieldClass} value={text} onChange={event => onChange(event.target.value)}>
        {!taskRefs.includes(text) && <option value={text}>{text || "Choose a task"}</option>}
        {taskRefs.map(ref => <option key={ref} value={ref}>{ref === "${{ workflow.task }}" ? "The workflow's task" : `Input ${ref.slice(20, -3)}`}</option>)}
      </select>
      break
    default:
      if (spec.enum && !isExpression) {
        control = <select aria-label={name} className={fieldClass} value={text} onChange={event => set(event.target.value)}>
          <option value="">{spec.required ? "Choose" : "The default"}</option>
          {spec.enum.map(option => <option key={option} value={option}>{option}</option>)}
        </select>
      } else {
        control = <CommitField aria-label={name} value={text} onCommit={set} list={suggestions.length > 0 ? listId : undefined} />
      }
  }
  // A div, not a label: a label would pass a click on the picker's menu on
  // to its first button and open it again. The controls name themselves.
  return <div className={labelClass} data-essential={spec.essential || undefined}>
    <span className="flex items-center justify-between gap-2"><span>{spec.label}{spec.required || spec.requiredAtRun ? " (required)" : ""}{spec.label !== spec.name && <span className="ml-1.5 font-lw-mono text-[11px] text-lw-faint">{spec.name}</span>}</span>{picker}</span>
    {control}
    {suggestions.length > 0 && <datalist id={listId}>{suggestions.map(s => <option key={s} value={s} />)}</datalist>}
    <span className="text-[11px] text-lw-faint">{spec.description}</span>
    {notANumber && <p className="text-xs text-lw-danger">{spec.name} must be a whole number or a {"${{ }}"} expression</p>}
    <Problems diagnostics={problems} />
  </div>
}

type ChatButtonValue = { label?: unknown; value?: unknown; style?: unknown }

// MAX_CHAT_BUTTONS is what a message takes on every platform.
const MAX_CHAT_BUTTONS = 5

// ChatButtonsField is chat.message.send's buttons as rows of a label, a
// value and a style, up to five; with none left, the field goes.
function ChatButtonsField({ name, stepId, value, refs, onChange }: { name: string; stepId: string; value: unknown; refs: string[]; onChange: (value: unknown) => void }) {
  const rows: ChatButtonValue[] = Array.isArray(value) ? value.map(row => (row && typeof row === "object" ? row : {}) as ChatButtonValue) : []
  const write = (next: ChatButtonValue[]) => onChange(next.length === 0 ? undefined : next)
  const update = (i: number, key: "label" | "value" | "style", raw: string) => write(rows.map((row, j) => {
    if (j !== i) return row
    const next: ChatButtonValue = { ...row, [key]: raw }
    if (key === "style" && raw === "") delete next.style
    return next
  }))
  return <div role="group" aria-label={name} className="flex flex-col gap-2">
    {rows.map((row, i) => {
      const n = i + 1
      // A card per button: the inspector is narrow, so label and value
      // share a line and the style and Remove go under them.
      return <div key={i} role="group" aria-label={`Button ${n} of step ${stepId}`} className="flex flex-col gap-2 rounded-lg border border-lw-line p-2">
        <div className="grid gap-2 sm:grid-cols-2">
          <CommitField aria-label={`Button ${n} label of step ${stepId}`} placeholder="Label" value={valueText(row.label)} onCommit={raw => update(i, "label", raw)} />
          <span className="flex min-w-0 items-center gap-1">
            <CommitField aria-label={`Button ${n} value of step ${stepId}`} placeholder="Value, such as fix:…" value={valueText(row.value)} onCommit={raw => update(i, "value", raw)} />
            <ReferencePicker name={`button ${n} value of step ${stepId}`} options={refs} onPick={expression => update(i, "value", `${valueText(row.value)}\${{ ${expression} }}`)} />
          </span>
        </div>
        <div className="flex items-center justify-between gap-2">
          <select aria-label={`Button ${n} style of step ${stepId}`} className={`${fieldClass} w-auto`} value={valueText(row.style)} onChange={event => update(i, "style", event.target.value)}>
            <option value="">Default style</option><option value="primary">Primary</option><option value="danger">Danger</option>
          </select>
          <Button type="button" variant="ghost" size="icon-sm" aria-label={`Remove button ${n} of step ${stepId}`} onClick={() => write(rows.filter((_, j) => j !== i))}><Trash2 size={13} /></Button>
        </div>
      </div>
    })}
    {rows.length < MAX_CHAT_BUTTONS && <Button type="button" size="sm" variant="outline" className="self-start" aria-label={`Add a button to step ${stepId}`} onClick={() => write([...rows, { label: "", value: "" }])}><Plus size={13} />Add a button</Button>}
  </div>
}

// richControlOf is the inspector's control for a field, where one suits it
// better than a text box: a sentence switch for a boolean, the agent tools
// as a choice, and long text with a larger editor.
function richControlOf({ spec, name, value, text, onChange, set, options, groups, check }: { spec: FieldSpec; name: string; value: unknown; text: string; onChange: (value: unknown) => void; set: (raw: string) => void; options: BuilderOptions; groups: ValueGroup[] | null; check: (expression: string) => string | null }): React.ReactNode {
  const useValue = groups && <UseValueFrom label={spec.label} groups={groups} onPick={onChange} />
  if (spec.kind === "integer" && groups) {
    return <div className="flex flex-wrap items-center gap-2">
      <CommitField aria-label={name} inputMode="numeric" value={text} onCommit={raw => /^-?\d+$/.test(raw.trim()) ? onChange(Number(raw.trim())) : set(raw)} />
      {useValue}
    </div>
  }
  if (spec.enum && groups) {
    return <div className="flex flex-wrap items-center gap-2">
      <select aria-label={name} className={fieldClass} value={text} onChange={event => set(event.target.value)}>
        <option value="">{spec.required ? "Choose" : "The default"}</option>
        {spec.enum.map(option => <option key={option} value={option}>{option}</option>)}
      </select>
      {useValue}
    </div>
  }
  // A switch or a choice of agent tools can take a value from elsewhere
  // too, as a number or a choice can.
  const withValue = (control: React.ReactNode) => groups ? <div className="flex flex-wrap items-center gap-2">{control}{useValue}</div> : control
  if (spec.kind === "boolean") {
    const on = value === true
    return withValue(<button type="button" role="switch" aria-checked={on} aria-label={name} className="flex min-h-9 items-center gap-2 self-start text-sm text-lw-text" onClick={() => onChange(on ? (spec.required ? false : undefined) : true)}>
      <span aria-hidden className={`relative h-5 w-9 rounded-full transition-colors ${on ? "bg-lw-accent" : "bg-lw-line-strong"}`}><span className={`absolute top-0.5 h-4 w-4 rounded-full bg-lw-surface transition-all ${on ? "left-[1.125rem]" : "left-0.5"}`} /></span>
      {spec.label}
    </button>)
  }
  if (spec.kind === "agent" && options.agents.length > 0 && (text === "" || options.agents.includes(text))) {
    return withValue(<div role="radiogroup" aria-label={name} className="inline-flex flex-wrap self-start rounded-md border border-lw-line p-0.5">
      {["", ...options.agents].map(agent => <button key={agent || "default"} type="button" role="radio" aria-checked={text === agent} className={`min-h-8 rounded px-2.5 text-xs ${text === agent ? "bg-lw-raised text-lw-text" : "text-lw-text-2"}`} onClick={() => set(agent)}>{agent || "Default"}</button>)}
    </div>)
  }
  if (spec.kind === "text") return <LongText name={name} label={spec.label} value={text} onCommit={set} groups={groups} check={check} />
  return null
}

// LongText is a multi-line field with a larger editor to write it in.
function LongText({ name, label, value, onCommit, groups, check }: { name: string; label: string; value: string; onCommit: (value: string) => void; groups: ValueGroup[] | null; check: (expression: string) => string | null }) {
  const [focus, setFocus] = useState<string | null>(null)
  return <div className="flex flex-col gap-1">
    {groups ? <ExpressionText name={name} label={label} value={value} multiline groups={groups} check={check} onCommit={onCommit} /> : <CommitField aria-label={name} multiline value={value} onCommit={onCommit} />}
    <Button type="button" size="sm" variant="ghost" className="self-end" aria-label={`Write ${label} in a larger editor`} onClick={() => setFocus(value)}><Maximize2 size={12} />Larger</Button>
    {focus !== null && <div role="dialog" aria-label={label} className="fixed inset-0 z-50 flex flex-col gap-2 bg-lw-ground p-4 sm:p-8">
      <h3 className="text-sm font-semibold">{label}</h3>
      <textarea aria-label={`${label}, larger`} autoFocus className="min-h-0 flex-1 rounded-md border border-lw-line-strong bg-lw-surface p-3 font-lw-mono text-sm text-lw-text" value={focus} onChange={event => setFocus(event.target.value)} />
      <div className="flex justify-end gap-2">
        <Button type="button" size="sm" variant="ghost" onClick={() => setFocus(null)}>Cancel</Button>
        <Button type="button" size="sm" onClick={() => { if (focus !== value) onCommit(focus); setFocus(null) }}>Done</Button>
      </div>
    </div>}
  </div>
}

const OUTPUT_TYPES = ["string", "integer", "boolean", "json", "file"]

export function OutputsEditor({ step, id, path, text, apply, problems }: { step: StepValue; id: string; path: YamlPath; text: string; apply: Props["apply"]; problems: WorkflowDiagnostic[]; taken: string[] }) {
  const outputs = step.outputs ?? {}
  const [name, setName] = useState("")
  const set = (sub: YamlPath, value: unknown) => apply(() => setField(text, [...path, "outputs", ...sub], value === "" ? undefined : value))
  return <fieldset className="flex flex-col gap-2 rounded border border-lw-line p-2">
    <legend className="px-1 text-xs text-lw-text-2">Outputs</legend>
    {Object.entries(outputs).map(([key, output]) => <div key={key} role="group" aria-label={`Output ${key} of step ${id}`} className="grid gap-2 sm:grid-cols-[8rem_7rem_1fr_auto]">
      <span className="self-center font-lw-mono text-xs">{key}</span>
      <select aria-label={`Type of output ${key}`} className={fieldClass} value={String(output?.type ?? "string")} onChange={event => set([key, "type"], event.target.value)}>
        {OUTPUT_TYPES.map(type => <option key={type} value={type}>{type}</option>)}
      </select>
      <div className="flex flex-col gap-1">
        <CommitField aria-label={`Description of output ${key}`} placeholder="What it is" value={valueText(output?.description)} onCommit={value => set([key, "description"], value)} />
        {output?.type === "file" && <CommitField aria-label={`Path of output ${key}`} placeholder="report.md" value={valueText(output?.path)} onCommit={value => set([key, "path"], value)} />}
      </div>
      <Button type="button" variant="ghost" size="icon-sm" aria-label={`Remove output ${key}`} onClick={() => apply(() => setField(text, [...path, "outputs", ...(Object.keys(outputs).length === 1 ? [] : [key])], undefined))}><Trash2 size={13} /></Button>
    </div>)}
    <Problems diagnostics={problems} />
    <div className="flex items-end gap-2">
      <input aria-label={`New output of step ${id}`} placeholder="summary" className={fieldClass} value={name} onChange={event => setName(event.target.value)} />
      <Button type="button" size="sm" variant="outline" disabled={!name.trim() || name.trim() in outputs} onClick={() => { set([name.trim()], { type: "string" }); setName("") }}><Plus size={13} />Add output</Button>
    </div>
  </fieldset>
}
