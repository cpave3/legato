import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react"
import { ArrowDown, ArrowLeft, ArrowUp, Plus, Trash2 } from "lucide-react"
import { addInput, inputsOf, itemKey, itemsOf, itemText, moveStep, problemsByItem, removeStep, renameStep, setField, setItemText, stepsOf, stepYamlPath, triggersOf, parseWorkflow, WorkflowDocumentError, type ItemRef, type WorkflowEdit, type WorkflowValues, type YamlPath } from "../../lib/workflowDocument"
import { addressOf, sameSelection, type Selection } from "../../lib/workflowAddress"
import type { SchedulePreview, WorkflowDiagnostic } from "../../lib/workflowFiles"
import { Button } from "../ui/Button"
import { PendingDrafts } from "./pendingDrafts"
import { BrokenOutline } from "./BrokenOutline"
import { RowButton, StepRow } from "./OutlineRowParts"
import { InputsBody, OutlineSection, StepsBody, TriggersBody } from "./OutlineSections"
import { OutlineMap, PhoneMap, STALE_WORDS, ViewSwitch, type OutlineView } from "./OutlineMap"
import { useHeldYaml, type HeldYaml } from "./useHeldYaml"
import { useOutlineSelection } from "./useOutlineSelection"
import { stepListOf, useOutlineEditing } from "./useOutlineEditing"
import { CommitField, Problems } from "./builderFields"
import { CARD_TASK, executorTitle, fromTheCard, nodeOf, orderOf, placeOf, rowsOf, selectionOfNode, type Row } from "./outlineRows"
import { valueText } from "./builderStyles"
import { DetailsForm, InputCard } from "./itemForms"
import { brokenReferences, noOptions, stepReferences, type BuilderOptions, type StepValue } from "./stepForms"
import { idFromTitle } from "./stepRows"
import { StepOutputs, StepSetup } from "./StepSetup"
import { StepPicker } from "./StepPicker"
import { TriggerCard } from "./TriggersEditor"
import { KIND_TITLES, triggerKinds } from "./triggerForms"
import { LazyWorkflowYamlEditor } from "./LazyWorkflowYamlEditor"
import type { WorkflowGraphModel } from "../../lib/workflow"
import { DockCard, PanelClose } from "../workflow-canvas/DockCard"
import { cardTitle, floatingPanel } from "../workflow-canvas/canvasStyles"
import { useCardFolds } from "../workflow-canvas/useCardFolds"
import { WorkflowCanvas } from "../workflow-canvas/WorkflowCanvas"
import { parseDocument } from "yaml"

export type WorkflowOutlineProps = {
  text: string
  /** onEdit is told each change, as one undo step; typing in an item's
   * YAML tab is one step until a pause, as in the YAML side. */
  onEdit: (edit: WorkflowEdit, how?: { typing?: boolean }) => void
  diagnostics?: WorkflowDiagnostic[]
  nameFixed?: boolean
  /** onEditYaml switches to YAML, at a line when given one. */
  onEditYaml: (line?: number) => void
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
  /** held and onHeld keep item YAML not written yet on the page, so it
   * outlives this outline (a switch to the whole file's YAML). */
  held?: HeldYaml
  onHeld?: React.Dispatch<React.SetStateAction<HeldYaml>>
  /** pausedHere says, by trigger kind, whether this Legato's switch has the
   * trigger paused (LEG-459); a kind it has no switch for reads the file. */
  pausedHere?: Record<string, boolean>
  /** mode navigate is the outline beside the YAML (LEG-512): rows jump the
   * editor to their text (onJump), and the row at cursor is marked. */
  mode?: "edit" | "navigate"
  cursor?: number
  onJump?: (range: { from: number; to: number }) => void
  /** broken: the text no longer parses, and this is the last that did. */
  broken?: { line?: number; message: string }
  /** graph is the run graph of the last text the check could read; stale
   * when that is not the text now (LEG-515). */
  graph?: { model: WorkflowGraphModel; stale: boolean } | null
  /** lastGood is the last text that parsed, which the page keeps across
   * modes; onGood is told each text that parses. */
  lastGood?: string | null
  onGood?: (text: string) => void
  /** savedText is the file as last saved (null for a new workflow): a
   * step it lacks takes its ID from its title. */
  savedText?: string | null
  /** layout canvas draws the Visual side on a wide screen as the graph,
   * with the outline's sections as cards on the left and the inspector on
   * the right (LEG-655); the page's notices float over it, its Problems
   * card (cards) heads the left dock. */
  layout?: "canvas"
  notices?: ReactNode
  cards?: ReactNode
}

// The editor's inspector is wider than a run's side panel: its forms need
// the room (LEG-655).
const INSPECTOR_WIDTH = 480

// cardOfKind is the card over the graph that holds a row of each kind.
const cardOfKind: Record<Selection["kind"], string> = { trigger: "triggers", input: "inputs", step: "steps", about: "about" }

export type { HeldYaml }

const NO_SWITCHES: Record<string, boolean> = {}

// WorkflowOutline is the Visual side of the editor (LEG-505): the outline
// of the workflow (Triggers, Inputs, Steps, About; When, Asks for and Then
// before LEG-655) and an inspector of the
// row chosen, over the same text the YAML side edits.
function Outline(props: WorkflowOutlineProps) {
  const { text, onEdit, diagnostics = [], nameFixed, onEditYaml, options = noOptions, schedule, selection, onSelect, narrow = false, sheet = false, onCloseSheet, onUndo, onRedo, savedText = null, pausedHere = NO_SWITCHES, mode = "edit", cursor, onJump, broken, graph = null } = props
  const navigating = mode === "navigate"
  const editing = !navigating && !broken
  const { file, items } = useMemo(() => itemsOf(text), [text])
  const values = useMemo(() => parseWorkflow(text).values, [text])
  const problems = useMemo(() => problemsByItem(text, diagnostics), [text, diagnostics])
  const [view, setView] = useState<OutlineView>("list")
  const inspector = useRef<HTMLElement>(null)
  // On the canvas: which cards are folded, and the node to centre.
  const canvasOn = props.layout === "canvas" && !navigating && !narrow
  const folds = useCardFolds("legato:workflow-editor:folds", {})
  const { open: openCard } = folds
  const [centerOn, setCenterOn] = useState<{ id: string; n: number } | null>(null)
  const centre = (id: string | null) => { if (id) setCenterOn(current => ({ id, n: (current?.n ?? 0) + 1 })) }
  // The card that holds the row chosen opens, so the row is never hidden.
  const selectedCard = selection ? cardOfKind[selection.kind] : null
  const selectedFolded = selectedCard ? folds.folded(selectedCard) : false
  useEffect(() => { if (canvasOn && selectedCard && selectedFolded) openCard(selectedCard) }, [canvasOn, selectedCard, selectedFolded, openCard])
  // List | Graph on a phone: the outline, or the graph (read-only) in its
  // place.

  const rows = useMemo(() => rowsOf(items, values, problems, pausedHere), [items, values, problems, pausedHere])
  const savedIds = useMemo(() => savedText === null ? null : new Set(stepsOf(parseWorkflow(savedText).values).map(step => String(step?.id ?? ""))), [savedText])
  const order: Row[] = useMemo(() => orderOf(rows), [rows])

  const { shown, selectedRow, choose, outline, onKeyDown } = useOutlineSelection({ text, items, order, selection, onSelect, navigating, broken: Boolean(broken), cursor, onJump })

  const { heldYaml, hold } = useHeldYaml({ text, order, held: props.held, onHeld: props.onHeld })

  const list = useMemo(() => stepListOf(stepsOf(values) as (StepValue | null)[]), [values])
  const { placed } = list
  const editor = useOutlineEditing({ editing, text, values, list, selection, onEdit, onSelect, choose, options })
  const { error, apply, adding, setAdding, select, paste, addStep, warning, focusNew, focused, addTrigger, addAnInput } = editor

  if (file) return <BrokenOutline file={file} canvas={canvasOn} notices={props.notices} cards={props.cards} onEditYaml={onEditYaml} />

  const chooseRow = (row: Row) => { select(row.selection, true); if (canvasOn) centre(nodeOf(row.selection)) }
  const rowButton = (row: Row, number?: number) => <RowButton row={row} number={number} selected={sameSelection(shown, row.selection)} onChoose={() => chooseRow(row)} />
  const rowItem = (row: Row) => <li key={addressOf(row.selection)}>{rowButton(row)}</li>

  const palette = adding?.kind === "step" ? <StepPicker narrow={narrow} onPick={type => addStep(type, adding.at)} onClose={() => setAdding(null)} /> : null

  const sectionProps = { containers: rows.about.containers, editing, rowItem, onEditYaml: () => onEditYaml() }
  const stepRow = (row: Row) => <StepRow key={addressOf(row.selection)} row={row} list={list} editing={editing} narrow={narrow} rowButton={rowButton} palette={palette} editor={editor} />
  const whenBody = <TriggersBody {...sectionProps} rows={rows.when} missing={triggerKinds.filter(t => !(t.kind in triggersOf(values)))} adding={adding} setAdding={setAdding} onAdd={addTrigger} />
  const asksBody = <InputsBody {...sectionProps} rows={rows.asks} onAdd={addAnInput} />
  const thenBody = <StepsBody {...sectionProps} rows={rows.then} stepRow={stepRow} count={placed.length} warning={warning} adding={adding} setAdding={setAdding} palette={palette} />
  const aboutBody = <ul className="flex flex-col">{rowItem(order[order.length - 1])}</ul>
  const outlineView = <nav ref={outline} aria-label="Outline" onKeyDown={onKeyDown} className="flex min-w-0 flex-col gap-4">
    <OutlineSection title="Triggers">{whenBody}</OutlineSection>
    <OutlineSection title="Inputs">{asksBody}</OutlineSection>
    <OutlineSection title="Steps">{thenBody}</OutlineSection>
    <OutlineSection title="About">{aboutBody}</OutlineSection>
  </nav>

  const listOrMap = <ViewSwitch view={view} onView={setView} hasGraph={Boolean(graph)} />
  const graphSelect = (id: string | null) => { if (broken || !graph) return; const next = selectionOfNode(graph.model, id); if (next) select(next, true) }
  const mapProps = graph && { graph, selectedId: nodeOf(shown), onSelect: graphSelect }
  // On a phone, Graph is the canvas in the outline's place, top to bottom.
  const mapView = mapProps && (narrow && !navigating ? <PhoneMap {...mapProps} /> : <OutlineMap {...mapProps} narrow={narrow} />)
  const navigation = <div className="flex min-w-0 flex-col gap-2">
    {listOrMap}
    {view === "map" && mapView ? mapView : outlineView}
  </div>

  const inspectorView = <section ref={inspector} aria-label="Inspector" className="flex min-w-0 flex-col gap-3">
    {narrow && <Button type="button" size="sm" variant="ghost" className="self-start" onClick={onCloseSheet}><ArrowLeft size={13} />Outline</Button>}
    {selectedRow
      ? <Inspector key={placeOf(order, selectedRow.selection)} place={placeOf(order, selectedRow.selection)} held={heldYaml[placeOf(order, selectedRow.selection)]} onHold={hold} row={selectedRow} savedIds={savedIds} focusEssential={selectedRow.selection.kind === "step" && selectedRow.selection.path === focusNew} onFocused={focused} text={text} values={values} diagnostics={diagnostics} problems={problems} options={options} apply={apply} nameFixed={nameFixed} schedule={schedule} onSelect={onSelect} onUndo={onUndo} onRedo={onRedo} />
      : <p className="text-sm text-lw-text-2">Choose a row of the outline to edit it.</p>}
  </section>

  const brokenNote = broken && <p role="alert" className="flex flex-wrap items-center gap-2 rounded-lg border border-lw-danger-line bg-lw-error-soft px-3 py-2 text-xs text-lw-danger">
    <span className="min-w-0 flex-1">{broken.line ? `Line ${broken.line}: ` : ""}can't read the YAML. The outline is as it last could be; the forms are off until it reads again.</span>
    <Button size="sm" variant="outline" onClick={() => onEditYaml(broken.line)}>Show</Button>
  </p>
  // While the YAML doesn't read: the last map (it says it is stale), or the
  // last outline, dimmed and off.
  const brokenView = <div className="flex min-w-0 flex-col gap-2">
    {listOrMap}
    {view === "map" && mapView ? mapView : <div inert className="opacity-50">{outlineView}</div>}
  </div>
  if (canvasOn) {
    const cardOf = (key: string, label: string, size: number | null, body: ReactNode) => <DockCard label={label} folded={folds.folded(key)} onFold={() => folds.toggle(key)}
      heading={<h2 className={cardTitle}>{label}{size !== null && <span className="font-medium text-lw-faint">{size}</span>}</h2>}>
      <div className="flex min-w-0 flex-col gap-1 px-1.5 pb-2">{body}</div>
    </DockCard>
    // j and k move across every card's rows: the keys are the dock's.
    const nav = <nav ref={outline} aria-label="Outline" onKeyDown={onKeyDown} className="flex min-w-0 flex-col gap-2">
      {cardOf("triggers", "Triggers", rows.when.length, whenBody)}
      {cardOf("inputs", "Inputs", rows.asks.length, asksBody)}
      {cardOf("steps", "Steps", rows.then.length, thenBody)}
      {cardOf("about", "About", null, aboutBody)}
    </nav>
    const box = "rounded-lg border border-lw-line bg-lw-surface px-3 py-2 text-[13px] shadow-[var(--shadow-lw-panel)]"
    const notices = <>
      {props.notices}
      {brokenNote}
      {graph?.stale && <p role="status" className={`${box} text-lw-needs`}>{STALE_WORDS}</p>}
      {error && <p role="alert" className={`${box} text-lw-danger`}>{error}</p>}
      {paste.pasted !== null && <div role="alertdialog" aria-label="Replace the workflow" className="flex flex-wrap items-center gap-2 rounded-lg border border-lw-needs bg-lw-surface p-3 text-sm shadow-[var(--shadow-lw-panel)]">
        <span className="min-w-0 flex-1">You pasted a whole workflow. Replace this one with it? Undo brings this one back.</span>
        <Button size="sm" onClick={paste.replace}>Replace</Button>
        <Button size="sm" variant="ghost" onClick={paste.cancel}>Cancel</Button>
      </div>}
    </>
    const hasNotices = Boolean(props.notices || broken || graph?.stale || error || paste.pasted !== null)
    const panel = selectedRow && !broken && <aside aria-label={selectedRow.label.trim()} className={`flex min-h-0 flex-col rounded-xl border border-lw-line bg-lw-surface p-4 text-sm ${floatingPanel}`}>
      <div className="flex items-start gap-2">
        <div className="min-w-0 flex-1">{inspectorView}</div>
        <PanelClose onClose={() => onSelect(null)} />
      </div>
    </aside>
    return <div className="flex min-h-0 flex-1 flex-col text-sm" onPaste={paste.onPaste}>
      <WorkflowCanvas label="Workflow graph" narrow={false} panelWidth={INSPECTOR_WIDTH} dockYields
        graph={graph ? { graph: graph.model, selectedId: nodeOf(shown), onSelect: graphSelect, centerOn, direction: "LR" } : null}
        empty="The graph shows once Legato has read the file."
        cards={<>{props.cards}{broken ? <div inert className="opacity-50">{nav}</div> : nav}</>}
        notices={hasNotices && notices} panel={panel || null} panelTitle={selectedRow?.label.trim()}
        selected={selection !== null} onClosePanel={() => onSelect(null)} hiddenPill="Outline" />
    </div>
  }
  if (navigating) {
    if (narrow) {
      // On a phone, the outline beside the YAML is a menu to jump with.
      return <div className="flex flex-col gap-2 text-sm">
        {brokenNote}
        <label className="flex items-center gap-2 text-xs text-lw-text-2">Jump to
          <select aria-label="Jump to" disabled={Boolean(broken)} className="min-h-9 min-w-0 flex-1 rounded-md border border-lw-line-strong bg-lw-ground px-2 text-sm text-lw-text" value={shown ? addressOf(shown) : ""}
            onChange={event => { const row = order.find(r => addressOf(r.selection) === event.target.value); if (row) select(row.selection) }}>
            <option value="">The outline…</option>
            {order.map(row => <option key={addressOf(row.selection)} value={addressOf(row.selection)}>{row.label}</option>)}
          </select>
        </label>
      </div>
    }
    return <div className="flex flex-col gap-2 text-sm">
      {brokenNote}
      {broken ? brokenView : navigation}
    </div>
  }
  if (broken) {
    return <div className="flex flex-col gap-3 text-sm">
      {brokenNote}
      <div className="rounded-xl border border-lw-line bg-lw-surface p-2">{brokenView}</div>
    </div>
  }

  return <div className="flex flex-col gap-3 text-sm" onPaste={paste.onPaste}>
    {error && <p role="alert" className="text-sm text-lw-danger">{error}</p>}
    {paste.pasted !== null && <div role="alertdialog" aria-label="Replace the workflow" className="flex flex-wrap items-center gap-2 rounded-lg border border-lw-needs bg-lw-surface p-3 text-sm">
      <span className="min-w-0 flex-1">You pasted a whole workflow. Replace this one with it? Undo brings this one back.</span>
      <Button size="sm" onClick={paste.replace}>Replace</Button>
      <Button size="sm" variant="ghost" onClick={paste.cancel}>Cancel</Button>
    </div>}
    {narrow
      ? (sheet && selectedRow
        ? <div role="dialog" aria-label={selectedRow.label.trim()} className="fixed inset-0 z-40 overflow-y-auto bg-lw-ground px-4 pb-20 pt-3">{inspectorView}</div>
        : navigation)
      : <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,24rem)_minmax(0,1fr)] md:grid-cols-[minmax(0,20rem)_minmax(0,1fr)]">
        <div className="rounded-xl border border-lw-line bg-lw-surface p-2">{navigation}</div>
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
  focusEssential?: boolean
  onFocused?: () => void
  /** savedIds are the IDs of the steps saved (null: none yet). */
  savedIds: Set<string> | null
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
function Inspector({ place, held, onHold, row, focusEssential, onFocused, savedIds, text, values, diagnostics, problems, options, apply, nameFixed, schedule, onSelect, onUndo, onRedo }: InspectorProps) {
  const selection = row.selection
  const tabs: Tab[] = row.problem ? ["yaml"] : selection.kind === "step" ? ["setup", "outputs", "yaml"] : selection.kind === "about" ? ["setup"] : ["setup", "yaml"]
  const [tab, setTab] = useState<Tab>(tabs[0])
  const body = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!focusEssential) return
    const control = body.current?.querySelector<HTMLElement>('[data-essential="true"] :is(input, textarea, select)')
    if (control) { control.focus(); onFocused?.() }
  })
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
    const card = <TriggerCard kind={selection.name} trigger={triggersOf(values)[selection.name] ?? {}} text={text} values={values} diagnostics={diagnostics} options={options} apply={apply} schedule={selection.name === "schedule" ? schedule : null} />
    setup = selection.name === "task.entered_column" && fromTheCard(values).length === 0
      ? <div className="flex flex-col gap-3"><CardTaskOffer text={text} values={values} apply={apply} />{card}</div>
      : card
  } else {
    const yaml = stepYamlPath(text, selection.path)
    const top = yaml && yaml.length === 2 ? Number(yaml[1]) : -1
    const step = (top >= 0 ? placed[top] : null) ?? null
    const id = selection.path.split("/").pop() ?? ""
    kind = step ? executorTitle(step.type) : "Step"
    // A step not saved yet takes its ID from its title, while the ID is
    // still the one its title made.
    const others = ids.filter(other => other !== id)
    const follows = step !== null && !savedIds?.has(id) && id === idFromTitle(valueText(step.title), others)
    const commitTitle = (value: string) => apply(() => {
      const titled = setField(text, ["steps", top, "title"] as YamlPath, value === "" ? undefined : value)
      const next = idFromTitle(value, others)
      if (!follows || value.trim() === "" || next === id) return titled
      const renamed = renameStep(titled.text, id, next).text
      return { text: renamed, undo: { label: `Set the title of step ${id}`, before: text, after: renamed } }
    })
    header = top >= 0 && step && !row.problem
      ? <CommitField aria-label="Step title" value={valueText(step.title)} onCommit={commitTitle} />
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
        setup = <StepSetup step={step} index={top} text={text} values={values} diagnostics={diagnostics} options={options} apply={apply} />
        outputs = <StepOutputs step={step} index={top} text={text} diagnostics={diagnostics} apply={apply} />
      }
    } else if (!row.problem) {
      // A nested step: its forms come with the containers (LEG-518).
      setup = <p className="text-sm text-lw-text-2">A step inside another is edited as YAML for now.</p>
    }
  }

  const ref: ItemRef | null = selection.kind === "about" ? null : selection
  const yamlTab = ref && <ItemYaml text={text} item={ref} apply={apply} onUndo={onUndo} onRedo={onRedo} held={held} onHold={next => onHold(place, next)} />

  return <div ref={body} className="flex flex-col gap-3">
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

// CardTaskOffer offers a column trigger's workflow a task input filled
// from the card that entered the column.
function CardTaskOffer({ text, values, apply }: { text: string; values: WorkflowValues; apply: InspectorProps["apply"] }) {
  const taken = Object.keys(inputsOf(values))
  let name = "task"
  for (let n = 2; taken.includes(name); n++) name = `task-${n}`
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
  // A file that stops parsing keeps the outline it last had, dimmed.
  const parsed = useMemo(() => itemsOf(props.text), [props.text])
  const [ownGood, setOwnGood] = useState<string | null>(parsed.file === null ? props.text : null)
  const { onGood } = props
  useEffect(() => { if (parsed.file === null) { setOwnGood(props.text); onGood?.(props.text) } }, [parsed.file, props.text, onGood])
  const lastGood = props.lastGood ?? ownGood
  const broken = parsed.file !== null && lastGood !== null && parsed.file.startsWith("the YAML does not parse")
    ? { line: parsed.fileLine, message: parsed.file } : undefined
  return <PendingDrafts.Provider value={report}>
    {broken ? <Outline {...props} text={lastGood!} broken={broken} /> : <Outline {...props} />}
  </PendingDrafts.Provider>
}
