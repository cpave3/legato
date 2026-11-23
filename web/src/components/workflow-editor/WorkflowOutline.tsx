import { useCallback, useContext, useEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from "react"
import { AlertTriangle, ArrowDown, ArrowLeft, ArrowUp, Code2, Plus, Trash2 } from "lucide-react"
import { addInput, inputsOf, itemKey, itemsOf, itemText, moveStep, problemsByItem, removeStep, setField, setItemText, setTrigger, stepsOf, stepYamlPath, triggersOf, parseWorkflow, insertStep, WorkflowDocumentError, type Item, type ItemRef, type WorkflowEdit, type WorkflowValues, type YamlPath } from "../../lib/workflowDocument"
import { addressOf, sameSelection, type Selection } from "../../lib/workflowAddress"
import { executors } from "../../lib/workflowSchema"
import type { SchedulePreview, WorkflowDiagnostic } from "../../lib/workflowFiles"
import { Button } from "../ui/Button"
import { PendingDrafts } from "./pendingDrafts"
import { CommitField, Problems } from "./builderFields"
import { valueText } from "./builderStyles"
import { DetailsForm, InputCard } from "./itemForms"
import { brokenReferences, newStep, noOptions, stepReferences, type BuilderOptions, type StepValue } from "./stepForms"
import { StepCard } from "./StepsEditor"
import { TriggerCard } from "./TriggersEditor"
import { KIND_TITLES, newTrigger, triggerKinds } from "./triggerForms"
import { LazyWorkflowYamlEditor } from "./LazyWorkflowYamlEditor"
import { parseDocument } from "yaml"

export type WorkflowOutlineProps = {
  text: string
  /** onEdit is told each change, as one undo step; typing in an item's
   * YAML tab is one step until a pause, as in the YAML side. */
  onEdit: (edit: WorkflowEdit, how?: { typing?: boolean }) => void
  diagnostics?: WorkflowDiagnostic[]
  nameFixed?: boolean
  onEditYaml: () => void
  onPending?: (pending: boolean) => void
  options?: BuilderOptions
  schedule?: SchedulePreview | null
  /** selection is the row the inspector edits: the page's address. */
  selection: Selection | null
  /** onSelect is told a row chosen; open says it was opened (Enter, a
   * tap), which on a phone shows the inspector. */
  onSelect: (selection: Selection | null, how?: { open?: boolean }) => void
  /** narrow: a phone, where the outline is the screen and the inspector a
   * sheet over it while sheet is true. */
  narrow?: boolean
  sheet?: boolean
  onCloseSheet?: () => void
  onUndo?: () => void
  onRedo?: () => void
}

// placeOf is a row's kind and position among the rows of its kind.
function placeOf(order: Row[], selection: Selection): string {
  const same = order.filter(row => row.selection.kind === selection.kind)
  return `${selection.kind}:${same.findIndex(row => sameSelection(selection, row.selection))}`
}

type Row = { selection: Selection; label: string; detail: string; problem: string | null; problems: number; paused?: boolean }

const isTyping = (target: EventTarget | null) => {
  const element = target as HTMLElement | null
  return Boolean(element && (element.tagName === "INPUT" || element.tagName === "TEXTAREA" || element.tagName === "SELECT" || element.isContentEditable))
}

const executorTitle = (type: unknown) => executors.find(e => e.type === type)?.title ?? String(type ?? "")

// rowsOf are the outline's rows, section by section, from the file's
// items: a trigger, input or step each, and About for the top-level fields.
function rowsOf(items: Item[], values: WorkflowValues, problems: Map<string, WorkflowDiagnostic[]>) {
  const count = (item: ItemRef) => (problems.get(itemKey(item)) ?? []).filter(d => d.severity === "error").length
  const triggers = triggersOf(values)
  const inputs = inputsOf(values)
  const steps = new Map<string, Record<string, unknown>>()
  const walk = (list: unknown, parent: string) => {
    if (!Array.isArray(list)) return
    list.forEach((step, i) => {
      const id = step && typeof step === "object" && typeof (step as { id?: unknown }).id === "string" && (step as { id: string }).id !== "" ? (step as { id: string }).id : `#${i}`
      const path = parent ? `${parent}/${id}` : id
      steps.set(path, (step ?? {}) as Record<string, unknown>)
      walk((step as { steps?: unknown } | null)?.steps, path)
    })
  }
  walk(values.steps, "")
  const when: Row[] = []
  const asks: Row[] = []
  const then: Row[] = []
  const about: { problems: number; problem: string | null; containers: Item[] } = { problems: 0, problem: null, containers: [] }
  for (const item of items) {
    const ref = item.item
    if (ref.kind === "trigger") {
      const trigger = triggers[ref.name]
      when.push({ selection: ref, label: KIND_TITLES[ref.name] ?? ref.name, detail: "", problem: item.problem, problems: count(ref), paused: trigger?.enabled === false })
    } else if (ref.kind === "input") {
      asks.push({ selection: ref, label: ref.name, detail: String(inputs[ref.name]?.type ?? ""), problem: item.problem, problems: count(ref) })
    } else if (ref.kind === "step") {
      const step = steps.get(ref.path) ?? {}
      const depth = ref.path.split("/").length - 1
      then.push({ selection: ref, label: `${"  ".repeat(depth)}${valueText(step.title) || ref.path.split("/").pop()}`, detail: executorTitle(step.type), problem: item.problem, problems: count(ref) })
    } else if (["inputs", "on", "steps"].includes(ref.key)) {
      if (item.problem) about.containers.push(item)
    } else {
      about.problems += count(ref)
      about.problem ??= item.problem
    }
  }
  return { when, asks, then, about }
}

// WorkflowOutline is the Visual side of the editor (LEG-505): the outline
// of the workflow (When, Asks for, Then, About) and an inspector of the
// row chosen, over the same text the YAML side edits.
function Outline(props: WorkflowOutlineProps) {
  const { text, onEdit, diagnostics = [], nameFixed, onEditYaml, options = noOptions, schedule, selection, onSelect, narrow = false, sheet = false, onCloseSheet, onUndo, onRedo } = props
  const { file, items } = useMemo(() => itemsOf(text), [text])
  const values = useMemo(() => parseWorkflow(text).values, [text])
  const problems = useMemo(() => problemsByItem(text, diagnostics), [text, diagnostics])
  const [error, setError] = useState("")
  // An item's YAML that does not read yet, by the row's place (so a rename
  // keeps it): kept when its tab or row is left, and counted as unsaved.
  const [heldYaml, setHeldYaml] = useState<Record<string, string>>({})
  const report = useContext(PendingDrafts)
  const heldAny = Object.keys(heldYaml).length > 0
  useEffect(() => { report("outline-held-yaml", heldAny) }, [report, heldAny])
  useEffect(() => () => report("outline-held-yaml", false), [report])
  const hold = useCallback((place: string, held: string | null) => setHeldYaml(previous => {
    if (held === null) { if (!(place in previous)) return previous; const next = { ...previous }; delete next[place]; return next }
    return previous[place] === held ? previous : { ...previous, [place]: held }
  }), [])
  const [adding, setAdding] = useState<"step" | "trigger" | null>(null)
  const outline = useRef<HTMLElement>(null)
  const inspector = useRef<HTMLElement>(null)

  const rows = useMemo(() => rowsOf(items, values, problems), [items, values, problems])
  const order: Row[] = useMemo(() => [...rows.when, ...rows.asks, ...rows.then, { selection: { kind: "about" }, label: "About", detail: "", problem: rows.about.problem, problems: rows.about.problems }], [rows])

  // A rename moves a row's address: follow it to the row of its kind in
  // the same place.
  const [last, setLast] = useState<{ address: string; place: number } | null>(null)
  const found = selection ? order.findIndex(row => sameSelection(selection, row.selection)) : -1
  // Until the address catches up, the renamed row is still the one shown,
  // so its inspector (and the tab it is on) stays.
  const following = selection && found < 0 && last?.address === addressOf(selection) ? order.filter(row => row.selection.kind === selection.kind)[last.place] ?? null : null
  useEffect(() => {
    if (!selection) { setLast(null); return }
    const same = order.filter(row => row.selection.kind === selection.kind)
    const address = addressOf(selection)
    if (found >= 0) {
      const place = same.findIndex(row => sameSelection(selection, row.selection))
      setLast(previous => previous?.address === address && previous.place === place ? previous : { address, place })
      return
    }
    if (following) onSelect(following.selection)
  }, [selection, found, order, onSelect, following])

  const apply = useCallback((make: () => WorkflowEdit, how?: { typing?: boolean }) => {
    try {
      setError("")
      const result = make()
      if (result.text !== text) onEdit(result, how)
    } catch (reason) {
      setError(reason instanceof WorkflowDocumentError ? reason.message : String(reason))
    }
  }, [text, onEdit])

  if (file) {
    return <div role="alert" className="flex flex-col items-start gap-2 rounded-xl border border-lw-danger-line bg-lw-error-soft p-4 text-sm text-lw-danger">
      <p>{file.startsWith("the YAML does not parse") ? `The YAML doesn't parse, so it can't be shown here: ${file.replace(/^the YAML does not parse: /, "")}` : `This file can't be shown as forms: ${file}.`}</p>
      <Button size="sm" variant="outline" onClick={onEditYaml}>Edit as YAML</Button>
    </div>
  }

  // A step that is not a mapping keeps its place, and is no one's reader.
  const placed = stepsOf(values) as (StepValue | null)[]
  const steps = placed.filter((step): step is StepValue => step !== null && typeof step === "object")
  const ids = steps.map(step => step.id ?? "")
  const select = (next: Selection, open = false) => { setAdding(null); onSelect(next, { open }) }

  const addStep = (type: string) => {
    const step = newStep(type, ids)
    apply(() => insertStep(text, placed.length, step))
    select({ kind: "step", path: step.id! }, true)
  }
  const addTrigger = (kind: string) => {
    apply(() => setTrigger(text, kind, newTrigger(kind, options)))
    select({ kind: "trigger", name: kind }, true)
  }
  const addAnInput = () => {
    const taken = Object.keys(inputsOf(values))
    let name = "input"
    for (let n = 2; taken.includes(name); n++) name = `input-${n}`
    apply(() => addInput(text, name, { type: "string" }))
    select({ kind: "input", name }, true)
  }

  // j and k move through the rows, Enter opens the one chosen.
  const onKeyDown = (event: ReactKeyboardEvent) => {
    if (isTyping(event.target) || event.metaKey || event.ctrlKey || event.altKey) return
    const at = order.findIndex(row => sameSelection(selection, row.selection))
    if (event.key === "j" || event.key === "k") {
      event.preventDefault()
      const next = order[Math.max(0, Math.min(order.length - 1, at < 0 ? 0 : at + (event.key === "j" ? 1 : -1)))]
      if (!next) return
      onSelect(next.selection)
      requestAnimationFrame(() => outline.current?.querySelector<HTMLElement>(`[data-address="${CSS.escape(addressOf(next.selection))}"]`)?.focus())
    }
    // Enter on a row is the row button's own click, which opens it.
  }

  const rowButton = (row: Row) => {
    const selected = sameSelection(selection, row.selection)
    const address = addressOf(row.selection)
    return <li key={address}>
      <button type="button" data-address={address} aria-current={selected ? "true" : undefined}
        className={`flex min-h-10 w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm ${selected ? "bg-lw-raised text-lw-text" : "text-lw-text-2 hover:bg-lw-raised hover:text-lw-text"}`}
        onClick={() => select(row.selection, true)}>
        <span className="min-w-0 flex-1 truncate whitespace-pre">{row.label}</span>
        {row.detail && <span className="truncate text-xs text-lw-faint">{row.detail}</span>}
        {row.paused && <span className="rounded border border-lw-line px-1 text-[11px] text-lw-text-2">paused</span>}
        {row.problem && <span className="flex items-center gap-1 rounded border border-lw-line px-1 text-[11px] text-lw-text-2"><Code2 size={11} />Edited as YAML</span>}
        {row.problems > 0 && <span aria-label={`${row.problems} ${row.problems === 1 ? "problem" : "problems"}`} className="flex items-center gap-0.5 text-[11px] text-lw-danger"><AlertTriangle size={11} />{row.problems}</span>}
      </button>
    </li>
  }

  const section = (title: string, body: React.ReactNode) => <section aria-label={title} className="flex flex-col gap-1">
    <h2 className="px-2 text-[11px] font-semibold uppercase tracking-wide text-lw-faint">{title}</h2>
    {body}
  </section>
  const container = (key: string) => rows.about.containers.filter(item => item.item.kind === "about" && item.item.key === key).map(item => <p key={key} role="alert" className="mx-2 flex flex-wrap items-center gap-2 text-xs text-lw-needs">
    {item.problem} <Button size="sm" variant="ghost" onClick={onEditYaml}>Edit as YAML</Button>
  </p>)

  const missing = triggerKinds.filter(t => !(t.kind in triggersOf(values)))
  const outlineView = <nav ref={outline} aria-label="Outline" onKeyDown={onKeyDown} className="flex min-w-0 flex-col gap-4">
    {section("When", <>
      <p className="px-2 text-sm text-lw-text-2">By hand, from the Workflows tab, a card or the API</p>
      <ul className="flex flex-col">{rows.when.map(rowButton)}</ul>
      {container("on")}
      {missing.length > 0 && (adding === "trigger"
        ? <div role="group" aria-label="Trigger kinds" className="flex flex-col gap-1 px-2">
          {missing.map(t => <button key={t.kind} type="button" className="flex flex-col items-start rounded-lg border border-lw-line p-2 text-left hover:bg-lw-raised" onClick={() => addTrigger(t.kind)}>
            <span className="text-sm font-medium">{KIND_TITLES[t.kind] ?? t.kind}</span>
            <span className="text-xs text-lw-text-2">{t.description}</span>
          </button>)}
          <Button type="button" size="sm" variant="ghost" className="self-start" onClick={() => setAdding(null)}>Cancel</Button>
        </div>
        : <Button type="button" size="sm" variant="ghost" className="self-start" onClick={() => setAdding("trigger")}><Plus size={13} />Add trigger</Button>)}
    </>)}
    {section("Asks for", <>
      {rows.asks.length === 0 && <p className="px-2 text-sm text-lw-text-2">Nothing: it takes no inputs</p>}
      <ul className="flex flex-col">{rows.asks.map(rowButton)}</ul>
      {container("inputs")}
      <Button type="button" size="sm" variant="ghost" className="self-start" onClick={addAnInput}><Plus size={13} />Add input</Button>
    </>)}
    {section("Then", <>
      {rows.then.length === 0 && <p className="px-2 text-sm text-lw-text-2">No steps yet: a workflow needs at least one.</p>}
      <ol className="flex flex-col">{rows.then.map(rowButton)}</ol>
      {container("steps")}
      {adding === "step"
        ? <div role="group" aria-label="Step types" className="grid gap-1 px-2">
          {executors.filter(e => !e.internal).map(e => <button key={e.type} type="button" className="flex flex-col items-start gap-0.5 rounded-lg border border-lw-line p-2 text-left hover:bg-lw-raised" onClick={() => addStep(e.type)}>
            <span className="text-sm font-medium">{e.title}</span>
            <span className="text-xs text-lw-text-2">{e.description}</span>
          </button>)}
          <Button type="button" size="sm" variant="ghost" className="self-start" onClick={() => setAdding(null)}>Cancel</Button>
        </div>
        : <Button type="button" size="sm" variant="ghost" className="self-start" onClick={() => setAdding("step")}><Plus size={13} />Add step</Button>}
    </>)}
    {section("About", <ul className="flex flex-col">{rowButton(order[order.length - 1])}</ul>)}
  </nav>

  const selectedRow = found >= 0 ? order[found] : following
  const inspectorView = <section ref={inspector} aria-label="Inspector" className="flex min-w-0 flex-col gap-3">
    {narrow && <Button type="button" size="sm" variant="ghost" className="self-start" onClick={onCloseSheet}><ArrowLeft size={13} />Outline</Button>}
    {selectedRow
      ? <Inspector key={placeOf(order, selectedRow.selection)} place={placeOf(order, selectedRow.selection)} held={heldYaml[placeOf(order, selectedRow.selection)]} onHold={hold} row={selectedRow} text={text} values={values} diagnostics={diagnostics} problems={problems} options={options} apply={apply} nameFixed={nameFixed} schedule={schedule} onSelect={onSelect} onUndo={onUndo} onRedo={onRedo} />
      : <p className="text-sm text-lw-text-2">Choose a row of the outline to edit it.</p>}
  </section>

  return <div className="flex flex-col gap-3 text-sm">
    {error && <p role="alert" className="text-sm text-lw-danger">{error}</p>}
    {narrow
      ? (sheet && selectedRow
        ? <div role="dialog" aria-label={selectedRow.label.trim()} className="fixed inset-0 z-40 overflow-y-auto bg-lw-ground px-4 pb-20 pt-3">{inspectorView}</div>
        : outlineView)
      : <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,24rem)_minmax(0,1fr)] md:grid-cols-[minmax(0,20rem)_minmax(0,1fr)]">
        <div className="rounded-xl border border-lw-line bg-lw-surface p-2">{outlineView}</div>
        <div className="rounded-xl border border-lw-line bg-lw-surface p-4">{inspectorView}</div>
      </div>}
  </div>
}

type InspectorProps = {
  /** place names the row by its kind and position, which a rename keeps. */
  place: string
  held?: string
  onHold: (place: string, held: string | null) => void
  row: Row
  text: string
  values: WorkflowValues
  diagnostics: WorkflowDiagnostic[]
  problems: Map<string, WorkflowDiagnostic[]>
  options: BuilderOptions
  apply: (make: () => WorkflowEdit, how?: { typing?: boolean }) => void
  nameFixed?: boolean
  schedule?: SchedulePreview | null
  onSelect: (selection: Selection | null) => void
  onUndo?: () => void
  onRedo?: () => void
}

type Tab = "setup" | "outputs" | "yaml"

// Inspector edits one row: its header, then its tabs (Setup, Outputs for a
// step, and YAML). A row the forms can't show has YAML alone.
function Inspector({ place, held, onHold, row, text, values, diagnostics, problems, options, apply, nameFixed, schedule, onSelect, onUndo, onRedo }: InspectorProps) {
  const selection = row.selection
  const tabs: Tab[] = row.problem ? ["yaml"] : selection.kind === "step" ? ["setup", "outputs", "yaml"] : selection.kind === "about" ? ["setup"] : ["setup", "yaml"]
  const [tab, setTab] = useState<Tab>(tabs[0])
  const shown = tabs.includes(tab) ? tab : tabs[0]
  // A step that is not a mapping keeps its place, and is no one's reader.
  const placed = stepsOf(values) as (StepValue | null)[]
  const steps = placed.filter((step): step is StepValue => step !== null && typeof step === "object")
  const ids = steps.map(step => step.id ?? "")
  const own = selection.kind === "about"
    ? [...problems.entries()].filter(([key]) => key.startsWith("about:") && !["about:inputs", "about:on", "about:steps"].includes(key)).flatMap(([, list]) => list)
    : problems.get(itemKey(selection)) ?? []

  let kind = "About this workflow"
  let header: React.ReactNode = <h2 className="text-base font-semibold">{valueText(values.title) || "Untitled"}</h2>
  let actions: React.ReactNode = null
  let setup: React.ReactNode = null
  let outputs: React.ReactNode = null

  if (selection.kind === "about") {
    setup = <div className="flex flex-col gap-3"><DetailsForm text={text} values={values} diagnostics={diagnostics} apply={apply} nameFixed={nameFixed} /></div>
  } else if (selection.kind === "input") {
    kind = "Input"
    header = <h2 className="font-lw-mono text-base font-semibold">{selection.name}</h2>
    setup = <div className="flex flex-col gap-2"><InputCard text={text} values={values} diagnostics={diagnostics} apply={apply} name={selection.name} input={inputsOf(values)[selection.name]} /></div>
  } else if (selection.kind === "trigger") {
    kind = "Trigger"
    header = <h2 className="text-base font-semibold">{KIND_TITLES[selection.name] ?? selection.name}</h2>
    setup = <TriggerCard kind={selection.name} trigger={triggersOf(values)[selection.name] ?? {}} text={text} values={values} diagnostics={diagnostics} options={options} apply={apply} schedule={selection.name === "schedule" ? schedule : null} />
  } else {
    const yaml = stepYamlPath(text, selection.path)
    const top = yaml && yaml.length === 2 ? Number(yaml[1]) : -1
    const step = (top >= 0 ? placed[top] : null) ?? null
    const id = selection.path.split("/").pop() ?? ""
    kind = step ? executorTitle(step.type) : "Step"
    header = top >= 0 && step && !row.problem
      ? <CommitField aria-label="Step title" value={valueText(step.title)} onCommit={value => apply(() => setField(text, ["steps", top, "title"] as YamlPath, value === "" ? undefined : value))} />
      : <h2 className="text-base font-semibold">{row.label.trim()}</h2>
    if (top >= 0 && step) {
      const move = (to: number) => {
        const order = ids.filter(other => other !== id)
        order.splice(to, 0, id)
        const broken = brokenReferences(steps, order)
        if (broken.length > 0) { apply(() => { throw new WorkflowDocumentError(`That move would break a reference: ${broken.join("; ")}.`) }); return }
        apply(() => moveStep(text, id, to))
      }
      const readers = steps.filter(other => other.id !== id && stepReferences(other).some(ref => ref.id === id)).map(other => other.id)
      actions = <div className="flex gap-1">
        <Button type="button" variant="ghost" size="icon-sm" aria-label={`Move step ${id} up`} disabled={top === 0} onClick={() => move(top - 1)}><ArrowUp size={13} /></Button>
        <Button type="button" variant="ghost" size="icon-sm" aria-label={`Move step ${id} down`} disabled={top === placed.length - 1} onClick={() => move(top + 1)}><ArrowDown size={13} /></Button>
        <RemoveStep id={id} readers={readers.map(String)} onRemove={() => { apply(() => removeStep(text, id)); onSelect(null) }} />
      </div>
      if (!row.problem) {
        setup = <StepCard step={step} index={top} text={text} values={values} diagnostics={diagnostics} options={options} apply={apply} taken={ids} parts="setup" titleInHeader />
        outputs = <StepCard step={step} index={top} text={text} values={values} diagnostics={diagnostics} options={options} apply={apply} taken={ids} parts="outputs" />
      }
    } else if (!row.problem) {
      // A nested step: its forms come with the containers (LEG-518).
      setup = <p className="text-sm text-lw-text-2">A step inside another is edited as YAML for now.</p>
    }
  }

  const ref: ItemRef | null = selection.kind === "about" ? null : selection
  const yamlTab = ref && <ItemYaml text={text} item={ref} apply={apply} onUndo={onUndo} onRedo={onRedo} held={held} onHold={next => onHold(place, next)} />

  return <div className="flex flex-col gap-3">
    <header className="flex flex-col gap-1">
      <div className="flex items-center justify-between gap-2">
        <span className="text-[11px] font-semibold uppercase tracking-wide text-lw-faint">{kind}</span>
        {actions}
      </div>
      {header}
      {row.problem && <p className="text-xs text-lw-text-2">This can't be shown as a form: {row.problem}. Edit it as YAML here; the rest of the workflow stays as forms.</p>}
      {row.problem && <Problems diagnostics={own} />}
    </header>
    {tabs.length > 1 && <div role="tablist" aria-label="Inspector" className="inline-flex self-start rounded-md border border-lw-line p-0.5">
      {tabs.map(t => <button key={t} type="button" role="tab" aria-selected={shown === t} className={`min-h-7 rounded px-2.5 text-xs ${shown === t ? "bg-lw-raised text-lw-text" : "text-lw-text-2"}`} onClick={() => setTab(t)}>
        {t === "setup" ? "Setup" : t === "outputs" ? "Outputs" : "YAML"}
      </button>)}
    </div>}
    <div role={tabs.length > 1 ? "tabpanel" : undefined} className="flex flex-col gap-3">
      {shown === "setup" && setup}
      {shown === "outputs" && outputs}
      {shown === "yaml" && yamlTab}
    </div>
  </div>
}

function RemoveStep({ id, readers, onRemove }: { id: string; readers: string[]; onRemove: () => void }) {
  const [asking, setAsking] = useState(false)
  if (asking) {
    return <div role="alertdialog" aria-label={`Remove step ${id}`} className="flex flex-wrap items-center gap-2 rounded border border-lw-needs p-2 text-xs">
      <span>{readers.join(", ")} {readers.length === 1 ? "reads" : "read"} this step's outputs.</span>
      <Button type="button" size="sm" variant="destructive" onClick={onRemove}>Remove anyway</Button>
      <Button type="button" size="sm" variant="ghost" onClick={() => setAsking(false)}>Keep it</Button>
    </div>
  }
  return <Button type="button" variant="ghost" size="icon-sm" aria-label={`Remove step ${id}`} onClick={() => readers.length > 0 ? setAsking(true) : onRemove()}><Trash2 size={13} /></Button>
}

// ItemYaml is one item's YAML: what it edits writes over that item alone,
// once it parses, so a half-typed line never breaks the rest of the file.
function ItemYaml({ text, item, apply, onUndo, onRedo, held, onHold }: { text: string; item: ItemRef; apply: InspectorProps["apply"]; onUndo?: () => void; onRedo?: () => void; held?: string; onHold: (held: string | null) => void }) {
  const written = itemText(text, item)
  if (written === null) return null
  const problemOf = (next: string) => next.trim() === "" ? "it is empty" : parseDocument(next).errors[0]?.message.split("\n")[0]
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

export default function WorkflowOutline(props: WorkflowOutlineProps) {
  const { onPending } = props
  const pending = useRef(new Set<string>())
  const report = useCallback((id: string, isPending: boolean) => {
    const had = pending.current.size > 0
    if (isPending) pending.current.add(id)
    else pending.current.delete(id)
    if (had !== pending.current.size > 0) onPending?.(pending.current.size > 0)
  }, [onPending])
  return <PendingDrafts.Provider value={report}><Outline {...props} /></PendingDrafts.Provider>
}
