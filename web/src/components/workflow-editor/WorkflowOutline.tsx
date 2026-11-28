import { useEffect, useMemo, useRef, useState, type ReactNode } from "react"
import { itemsOf, problemsByItem, stepsOf, triggersOf, parseWorkflow, type WorkflowEdit } from "../../lib/workflowDocument"
import { addressOf, sameSelection, type Selection } from "../../lib/workflowAddress"
import type { SchedulePreview, WorkflowDiagnostic } from "../../lib/workflowFiles"
import { PendingDrafts, usePendingReport } from "./pendingDrafts"
import { BrokenOutline } from "./BrokenOutline"
import { RowButton, StepRow } from "./OutlineRowParts"
import { InputsBody, OutlineSection, StepsBody, TriggersBody } from "./OutlineSections"
import { OutlineNavigation, type OutlineView } from "./OutlineMap"
import { BrokenNote, CanvasOutline, InspectorPane, JumpMenu, SplitOutline } from "./OutlineLayouts"
import { useCanvasDock } from "./useCanvasDock"
import { Inspector } from "./OutlineInspector"
import { useHeldYaml, type HeldYaml } from "./useHeldYaml"
import { useOutlineSelection } from "./useOutlineSelection"
import { stepListOf, useOutlineEditing } from "./useOutlineEditing"
import { isStepAt, savedIdsOf, selectionAt, nodeOf, orderOf, placeOf, rowsOf, type Row } from "./outlineRows"
import { noOptions, type BuilderOptions, type StepValue } from "./stepForms"
import { StepPicker } from "./StepPicker"
import { triggerKinds } from "./triggerForms"
import type { WorkflowGraphModel } from "../../lib/workflow"

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
  const dock = useCanvasDock(canvasOn, selection)

  const rows = useMemo(() => rowsOf(items, values, problems, pausedHere), [items, values, problems, pausedHere])
  const savedIds = useMemo(() => savedIdsOf(savedText), [savedText])
  const order: Row[] = useMemo(() => orderOf(rows), [rows])

  const { shown, selectedRow, choose, outline, onKeyDown } = useOutlineSelection({ text, items, order, selection, onSelect, navigating, broken: Boolean(broken), cursor, onJump })

  const { heldYaml, hold } = useHeldYaml({ text, order, held: props.held, onHeld: props.onHeld })

  const list = useMemo(() => stepListOf(stepsOf(values) as (StepValue | null)[]), [values])
  const { placed } = list
  const editor = useOutlineEditing({ editing, text, values, list, selection, onEdit, onSelect, choose, options })
  const { error, apply, adding, setAdding, select, paste, addStep, warning, focusNew, focused, addTrigger, addAnInput } = editor

  if (file) return <BrokenOutline file={file} canvas={canvasOn} notices={props.notices} cards={props.cards} onEditYaml={onEditYaml} />

  const chooseRow = (row: Row) => { select(row.selection, true); if (canvasOn) dock.centre(nodeOf(row.selection)) }
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

  // A node chosen in the graph opens its row; none while the YAML doesn't
  // read.
  const graphSelect = (id: string | null) => {
    const next = broken ? null : selectionAt(graph, id)
    if (next) select(next, true)
  }
  // While the YAML doesn't read: the last map (it says it is stale), or the
  // last outline, dimmed and off.
  const navigation = <OutlineNavigation view={view} onView={setView} graph={graph} selectedId={nodeOf(shown)} onSelect={graphSelect} narrow={narrow} phone={narrow && !navigating} dim={Boolean(broken)}>{outlineView}</OutlineNavigation>

  const place = selectedRow && placeOf(order, selectedRow.selection)
  const inspectorView = <InspectorPane sectionRef={inspector} back={narrow} onBack={onCloseSheet}>
    {selectedRow && place && <Inspector key={place} place={place} held={heldYaml[place]} onHold={hold} row={selectedRow} savedIds={savedIds} focusEssential={isStepAt(selectedRow, focusNew)} onFocused={focused} text={text} values={values} diagnostics={diagnostics} problems={problems} options={options} apply={apply} nameFixed={nameFixed} schedule={schedule} onSelect={onSelect} onUndo={onUndo} onRedo={onRedo} />}
  </InspectorPane>

  const brokenNote = <BrokenNote broken={broken} onShow={onEditYaml} />
  if (canvasOn) {
    return <CanvasOutline dock={dock} bodies={{ triggers: whenBody, inputs: asksBody, steps: thenBody, about: aboutBody }} counts={{ triggers: rows.when.length, inputs: rows.asks.length, steps: rows.then.length }}
      outline={outline} onKeyDown={onKeyDown} pageNotices={props.notices} pageCards={props.cards} broken={broken} brokenNote={brokenNote}
      graph={graph} selectedId={nodeOf(shown)} onGraphSelect={graphSelect} error={error} paste={paste} onPaste={paste.onPaste}
      selectedRow={selectedRow} selection={selection} inspector={inspectorView} onSelect={onSelect} />
  }
  // On a phone, the outline beside the YAML is a menu to jump with.
  if (navigating && narrow) return <JumpMenu order={order} shown={shown} broken={Boolean(broken)} note={brokenNote} onChoose={next => select(next)} />
  if (navigating) {
    return <div className="flex flex-col gap-2 text-sm">
      {brokenNote}
      {navigation}
    </div>
  }
  if (broken) {
    return <div className="flex flex-col gap-3 text-sm">
      {brokenNote}
      <div className="rounded-xl border border-lw-line bg-lw-surface p-2">{navigation}</div>
    </div>
  }
  return <SplitOutline narrow={narrow} sheet={sheet} selectedRow={selectedRow} navigation={navigation} inspector={inspectorView} error={error} paste={paste} onPaste={paste.onPaste} />
}

export default function WorkflowOutline(props: WorkflowOutlineProps) {
  const report = usePendingReport(props.onPending)
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
