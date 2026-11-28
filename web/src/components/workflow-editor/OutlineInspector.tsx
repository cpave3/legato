import { useEffect, useRef, useState, type ReactNode } from "react"
import { ArrowDown, ArrowUp, Plus, Trash2 } from "lucide-react"
import { addInput, inputsOf, itemKey, itemText, moveStep, removeStep, renameStep, setField, setItemText, stepsOf, stepYamlPath, triggersOf, WorkflowDocumentError, type ItemRef, type WorkflowValues, type YamlPath } from "../../lib/workflowDocument"
import { addressOf, type Selection } from "../../lib/workflowAddress"
import type { SchedulePreview, WorkflowDiagnostic } from "../../lib/workflowFiles"
import { Button } from "../ui/Button"
import { CommitField, Problems } from "./builderFields"
import { valueText } from "./builderStyles"
import { DetailsForm, InputCard } from "./itemForms"
import { ReadersConfirm } from "./OutlineRowParts"
import { CARD_TASK, executorTitle, fromTheCard, isContainerKey, isStepAt, placeOf, type Row } from "./outlineRows"
import type { BuilderOptions, StepValue } from "./stepForms"
import { idFromTitle } from "./stepRows"
import { StepOutputs, StepSetup } from "./StepSetup"
import { TriggerCard } from "./TriggersEditor"
import { KIND_TITLES } from "./triggerForms"
import { LazyWorkflowYamlEditor } from "./LazyWorkflowYamlEditor"
import { freeName, moveProblem, readersOf, stepListOf, type Apply, type StepList } from "./useOutlineEditing"
import { parseDocument } from "yaml"

export type InspectorProps = {
  /** place names the row by its kind and position, which a rename keeps. */
  place: string
  held?: string
  onHold: (place: string, held: string | null) => void
  row: Row
  focusEssential?: boolean
  onFocused?: () => void
  /** savedIds are the IDs of the steps saved (null: none yet). */
  savedIds: Set<string> | null
  text: string
  values: WorkflowValues
  diagnostics: WorkflowDiagnostic[]
  problems: Map<string, WorkflowDiagnostic[]>
  options: BuilderOptions
  apply: Apply
  nameFixed?: boolean
  schedule?: SchedulePreview | null
  onSelect: (selection: Selection | null) => void
  onUndo?: () => void
  onRedo?: () => void
}

type Tab = "setup" | "outputs" | "yaml"

const TAB_TITLES: Record<Tab, string> = { setup: "Setup", outputs: "Outputs", yaml: "YAML" }

/** tabsOf are a row's inspector tabs: Setup, Outputs for a step, and YAML;
 * a row the forms can't show has YAML alone, and About Setup alone. */
function tabsOf(row: Row): Tab[] {
  if (row.problem) return ["yaml"]
  if (row.selection.kind === "step") return ["setup", "outputs", "yaml"]
  return row.selection.kind === "about" ? ["setup"] : ["setup", "yaml"]
}

/** ownProblems are the diagnostics on a row: About's are every top-level
 * field's but the containers'. */
function ownProblems(selection: Selection, problems: Map<string, WorkflowDiagnostic[]>): WorkflowDiagnostic[] {
  if (selection.kind !== "about") return problems.get(itemKey(selection)) ?? []
  return [...problems.entries()].filter(([key]) => key.startsWith("about:") && !isContainerKey(key.slice("about:".length))).flatMap(([, list]) => list)
}

// A row's parts as each kind draws them; the frame draws them the same.
type Parts = { kind: string; header: ReactNode; actions?: ReactNode; setup?: ReactNode; outputs?: ReactNode }

// useFocusEssential focuses a just-added step's first essential field once
// it is drawn.
function useFocusEssential(focusEssential: boolean | undefined, onFocused: (() => void) | undefined) {
  const body = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!focusEssential) return
    const control = body.current?.querySelector<HTMLElement>('[data-essential="true"] :is(input, textarea, select)')
    if (control) { control.focus(); onFocused?.() }
  })
  return body
}

function InspectorTabs({ tabs, shown, onTab }: { tabs: Tab[]; shown: Tab; onTab: (tab: Tab) => void }) {
  return <div role="tablist" aria-label="Inspector" className="inline-flex self-start rounded-md border border-lw-line p-0.5">
    {tabs.map(t => <button key={t} type="button" role="tab" aria-selected={shown === t} className={`min-h-7 rounded px-2.5 text-xs ${shown === t ? "bg-lw-raised text-lw-text" : "text-lw-text-2"}`} onClick={() => onTab(t)}>
      {TAB_TITLES[t]}
    </button>)}
  </div>
}

// TabBody is the tab shown's body.
function TabBody({ shown, parts, yaml }: { shown: Tab; parts: Parts; yaml: ReactNode }) {
  return <>
    {shown === "setup" && (parts.setup ?? null)}
    {shown === "outputs" && (parts.outputs ?? null)}
    {shown === "yaml" && yaml}
  </>
}

// InspectorHeader is a row's kind, actions and header, and why the forms
// can't show it, with its problems.
function InspectorHeader({ parts, row, problems }: { parts: Parts; row: Row; problems: InspectorProps["problems"] }) {
  return <header className="flex flex-col gap-1">
    <div className="flex items-center justify-between gap-2">
      <span className="text-[11px] font-semibold uppercase tracking-wide text-lw-faint">{parts.kind}</span>
      {parts.actions ?? null}
    </div>
    {parts.header}
    {row.problem && <p className="text-xs text-lw-text-2">This can't be shown as a form: {row.problem}. Edit it as YAML here; the rest of the workflow stays as forms.</p>}
    {row.problem && <Problems diagnostics={ownProblems(row.selection, problems)} />}
  </header>
}

// InspectorFrame draws a row's parts: its header, then its tabs.
function InspectorFrame({ parts, ...props }: InspectorProps & { parts: Parts }) {
  const { row, text, apply, onUndo, onRedo, held, onHold, place, problems } = props
  const selection = row.selection
  const tabs = tabsOf(row)
  const [tab, setTab] = useState<Tab>(tabs[0])
  const body = useFocusEssential(props.focusEssential, props.onFocused)
  const shown = tabs.includes(tab) ? tab : tabs[0]
  const yamlTab = selection.kind !== "about" && <ItemYaml text={text} item={selection} apply={apply} onUndo={onUndo} onRedo={onRedo} held={held} onHold={next => onHold(place, next)} />
  return <div ref={body} className="flex flex-col gap-3">
    <InspectorHeader parts={parts} row={row} problems={problems} />
    {tabs.length > 1 && <InspectorTabs tabs={tabs} shown={shown} onTab={setTab} />}
    <div role={tabs.length > 1 ? "tabpanel" : undefined} className="flex flex-col gap-3">
      <TabBody shown={shown} parts={parts} yaml={yamlTab} />
    </div>
  </div>
}

function AboutInspector(props: InspectorProps) {
  const { text, values, diagnostics, apply, nameFixed } = props
  return <InspectorFrame {...props} parts={{
    kind: "About this workflow",
    header: <h2 className="text-base font-semibold">{valueText(values.title) || "Untitled"}</h2>,
    setup: <div className="flex flex-col gap-3"><DetailsForm text={text} values={values} diagnostics={diagnostics} apply={apply} nameFixed={nameFixed} /></div>,
  }} />
}

function InputInspector({ name, ...props }: InspectorProps & { name: string }) {
  const { text, values, diagnostics, apply } = props
  return <InspectorFrame {...props} parts={{
    kind: "Input",
    header: <h2 className="font-lw-mono text-base font-semibold">{name}</h2>,
    setup: <div className="flex flex-col gap-2"><InputCard text={text} values={values} diagnostics={diagnostics} apply={apply} name={name} input={inputsOf(values)[name]} /></div>,
  }} />
}

function TriggerInspector({ name, ...props }: InspectorProps & { name: string }) {
  const { text, values, diagnostics, options, apply, schedule } = props
  const card = <TriggerCard kind={name} trigger={triggersOf(values)[name] ?? {}} text={text} values={values} diagnostics={diagnostics} options={options} apply={apply} schedule={name === "schedule" ? schedule : null} />
  const offer = name === "task.entered_column" && fromTheCard(values).length === 0
  return <InspectorFrame {...props} parts={{
    kind: "Trigger",
    header: <h2 className="text-base font-semibold">{KIND_TITLES[name] ?? name}</h2>,
    setup: offer ? <div className="flex flex-col gap-3"><CardTaskOffer text={text} values={values} apply={apply} />{card}</div> : card,
  }} />
}

/** topStepOf is a step's place among the top-level steps (-1 for a nested
 * step), the step there, and its ID. */
function topStepOf(text: string, placed: StepList["placed"], path: string) {
  const yaml = stepYamlPath(text, path)
  const top = yaml && yaml.length === 2 ? Number(yaml[1]) : -1
  const step = (top >= 0 ? placed[top] : null) ?? null
  return { top, step, id: path.split("/").pop() ?? "" }
}

type TitleProps = { text: string; step: StepValue; top: number; id: string; ids: string[]; savedIds: Set<string> | null; apply: Apply }

// StepTitle edits a top-level step's title in the header. A step not
// saved yet takes its ID from its title, while the ID is still the one its
// title made.
function StepTitle({ text, step, top, id, ids, savedIds, apply }: TitleProps) {
  const others = ids.filter(other => other !== id)
  const follows = !savedIds?.has(id) && id === idFromTitle(valueText(step.title), others)
  const commitTitle = (value: string) => apply(() => {
    const titled = setField(text, ["steps", top, "title"] as YamlPath, value === "" ? undefined : value)
    const next = idFromTitle(value, others)
    if (!follows || value.trim() === "" || next === id) return titled
    const renamed = renameStep(titled.text, id, next).text
    return { text: renamed, undo: { label: `Set the title of step ${id}`, before: text, after: renamed } }
  })
  return <CommitField aria-label="Step title" value={valueText(step.title)} onCommit={commitTitle} />
}

// StepActions move a top-level step up or down, unless that would break a
// reference, and remove it.
function StepActions({ text, top, id, list, apply, onSelect }: { text: string; top: number; id: string; list: StepList; apply: Apply; onSelect: InspectorProps["onSelect"] }) {
  const move = (to: number) => {
    const problem = moveProblem(list, id, to)
    if (problem) { apply(() => { throw new WorkflowDocumentError(problem) }); return }
    apply(() => moveStep(text, id, to))
  }
  return <div className="flex gap-1">
    <Button type="button" variant="ghost" size="icon-sm" aria-label={`Move step ${id} up`} disabled={top === 0} onClick={() => move(top - 1)}><ArrowUp size={13} /></Button>
    <Button type="button" variant="ghost" size="icon-sm" aria-label={`Move step ${id} down`} disabled={top === list.placed.length - 1} onClick={() => move(top + 1)}><ArrowDown size={13} /></Button>
    <RemoveStep id={id} readers={readersOf(list.steps, id).map(other => String(other.id))} onRemove={() => { apply(() => removeStep(text, id)); onSelect(null) }} />
  </div>
}

// topStepParts are a top-level step's parts: its title to edit, its
// actions, and its Setup and Outputs unless the forms can't show it.
function topStepParts(props: InspectorProps, list: StepList, step: StepValue, top: number, id: string): Parts {
  const { row, text, values, diagnostics, options, apply, savedIds, onSelect } = props
  const forms = !row.problem
  return {
    kind: executorTitle(step.type),
    header: forms ? <StepTitle text={text} step={step} top={top} id={id} ids={list.ids} savedIds={savedIds} apply={apply} /> : <h2 className="text-base font-semibold">{row.label.trim()}</h2>,
    actions: <StepActions text={text} top={top} id={id} list={list} apply={apply} onSelect={onSelect} />,
    setup: forms && <StepSetup step={step} index={top} text={text} values={values} diagnostics={diagnostics} options={options} apply={apply} />,
    outputs: forms && <StepOutputs step={step} index={top} text={text} diagnostics={diagnostics} apply={apply} />,
  }
}

// nestedStepParts are a nested step's parts (or one not found): its forms
// come with the containers (LEG-518).
function nestedStepParts(row: Row, step: StepValue | null): Parts {
  return {
    kind: step ? executorTitle(step.type) : "Step",
    header: <h2 className="text-base font-semibold">{row.label.trim()}</h2>,
    setup: row.problem ? null : <p className="text-sm text-lw-text-2">A step inside another is edited as YAML for now.</p>,
  }
}

function StepInspector({ path, ...props }: InspectorProps & { path: string }) {
  // A step that is not a mapping keeps its place, and is no one's reader.
  const list = stepListOf(stepsOf(props.values) as (StepValue | null)[])
  const { top, step, id } = topStepOf(props.text, list.placed, path)
  const parts = top >= 0 && step ? topStepParts(props, list, step, top, id) : nestedStepParts(props.row, step)
  return <InspectorFrame {...props} parts={parts} />
}

/** Inspector edits one row: its header, then its tabs (Setup, Outputs for
 * a step, and YAML). A row the forms can't show has YAML alone. */
export function Inspector(props: InspectorProps) {
  const selection = props.row.selection
  if (selection.kind === "about") return <AboutInspector {...props} />
  if (selection.kind === "input") return <InputInspector {...props} name={selection.name} />
  if (selection.kind === "trigger") return <TriggerInspector {...props} name={selection.name} />
  return <StepInspector {...props} path={selection.path} />
}

type RowInspectorProps = Omit<InspectorProps, "place" | "held" | "row" | "focusEssential"> & { row: Row | null; order: Row[]; heldYaml: Record<string, string>; focusNew: string | null }

/** RowInspector is the inspector of the row chosen, keyed by its place (a
 * rename keeps it), with that place's held YAML; or it says to choose a
 * row. */
export function RowInspector({ row, order, heldYaml, focusNew, ...props }: RowInspectorProps) {
  if (!row) return <p className="text-sm text-lw-text-2">Choose a row of the outline to edit it.</p>
  const place = placeOf(order, row.selection)
  return <Inspector key={place} place={place} held={heldYaml[place]} row={row} focusEssential={isStepAt(row, focusNew)} {...props} />
}

// CardTaskOffer offers a column trigger's workflow a task input filled
// from the card that entered the column.
function CardTaskOffer({ text, values, apply }: { text: string; values: WorkflowValues; apply: Apply }) {
  const name = freeName("task", Object.keys(inputsOf(values)))
  const add = () => apply(() => {
    const added = addInput(text, name, { type: "task", required: true })
    const mapped = setField(added.text, ["on", "task.entered_column", "inputs", name], CARD_TASK)
    return { text: mapped.text, undo: { label: `Add input ${name} from the card`, before: text, after: mapped.text } }
  })
  return <div role="note" className="flex flex-wrap items-center gap-2 rounded-lg border border-lw-line bg-lw-ground p-2 text-xs text-lw-text-2">
    <span className="min-w-0 flex-1">Let the steps know which card it was: a task input, filled from the card.</span>
    <Button type="button" size="sm" variant="outline" onClick={add}><Plus size={12} />Add a task input from the card</Button>
  </div>
}

function RemoveStep({ id, readers, onRemove }: { id: string; readers: string[]; onRemove: () => void }) {
  const [asking, setAsking] = useState(false)
  if (asking) return <ReadersConfirm id={id} readers={readers} className="flex flex-wrap items-center gap-2 rounded border border-lw-needs p-2 text-xs" onRemove={onRemove} onKeep={() => setAsking(false)} />
  return <Button type="button" variant="ghost" size="icon-sm" aria-label={`Remove step ${id}`} onClick={() => readers.length > 0 ? setAsking(true) : onRemove()}><Trash2 size={13} /></Button>
}

const problemOf = (next: string) => next.trim() === "" ? "it is empty" : parseDocument(next).errors[0]?.message.split("\n")[0]

// ItemYaml is one item's YAML: what it edits writes over that item alone,
// once it parses, so a half-typed line never breaks the rest of the file.
function ItemYaml({ text, item, apply, onUndo, onRedo, held, onHold }: { text: string; item: ItemRef; apply: Apply; onUndo?: () => void; onRedo?: () => void; held?: string; onHold: (held: string | null) => void }) {
  const written = itemText(text, item)
  if (written === null) return null
  const value = held ?? written
  const problem = held !== undefined ? problemOf(held) : undefined
  const change = (next: string) => {
    const wrong = problemOf(next)
    onHold(wrong ? next : null)
    if (!wrong) apply(() => setItemText(text, item, next), { typing: true })
  }
  return <div className="flex flex-col gap-1">
    <div className="h-72 overflow-hidden rounded-lg border border-lw-line">
      <LazyWorkflowYamlEditor fragment label={`YAML of ${addressOf(item)}`} value={value} onChange={change} onUndo={onUndo} onRedo={onRedo} />
    </div>
    {problem && <p role="status" className="text-xs text-lw-needs">Not written to the file yet: {problem}</p>}
  </div>
}
