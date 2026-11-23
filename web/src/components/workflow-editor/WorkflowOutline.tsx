import { useCallback, useContext, useEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from "react"
import { AlertTriangle, ArrowDown, ArrowLeft, ArrowUp, Bot, Code2, Copy, FileText, FolderGit2, GitPullRequest, Hourglass, ListChecks, MoreVertical, Plus, Trash2 } from "lucide-react"
import { addInput, inputsOf, itemKey, itemsOf, itemText, moveStep, pastedAs, pathAt, rangeOf, problemsByItem, removeStep, renameStep, setField, setItemText, setTrigger, stepsOf, stepYamlPath, triggersOf, parseWorkflow, insertStep, WorkflowDocumentError, type Item, type ItemRef, type WorkflowEdit, type WorkflowValues, type YamlPath } from "../../lib/workflowDocument"
import { addressOf, sameSelection, type Selection } from "../../lib/workflowAddress"
import { executors } from "../../lib/workflowSchema"
import type { SchedulePreview, WorkflowDiagnostic } from "../../lib/workflowFiles"
import { Button } from "../ui/Button"
import { PendingDrafts } from "./pendingDrafts"
import { CommitField, Problems } from "./builderFields"
import { valueText } from "./builderStyles"
import { DetailsForm, InputCard } from "./itemForms"
import { brokenReferences, newStep, noOptions, stepReferences, type BuilderOptions, type StepValue } from "./stepForms"
import { connectorOf, duplicateOf, idFromTitle, stepSummary } from "./stepRows"
import { StepOutputs, StepSetup } from "./StepSetup"
import { StepPicker } from "./StepPicker"
import { TriggerCard } from "./TriggersEditor"
import { KIND_TITLES, newTrigger, triggerKinds, triggerSentence, type SentencePart } from "./triggerForms"
import { LazyWorkflowYamlEditor } from "./LazyWorkflowYamlEditor"
import { LazyWorkflowGraph } from "../workflow-graph/LazyWorkflowGraph"
import type { WorkflowGraphModel } from "../../lib/workflowGraph"
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
}

/** HeldYaml is item YAML not written yet, by item key, with the item's
 * text it was begun from. */
export type HeldYaml = Record<string, { base: string; text: string }>

// placeOf is a row's kind and position among the rows of its kind.
function placeOf(order: Row[], selection: Selection): string {
  const same = order.filter(row => row.selection.kind === selection.kind)
  return `${selection.kind}:${same.findIndex(row => sameSelection(selection, row.selection))}`
}

type Row = { selection: Selection; label: string; detail: string; problem: string | null; problems: number; paused?: boolean; on?: boolean; sentence?: SentencePart[]; summary?: string; type?: string; depth?: number }

const CARD_TASK = "${{ event.task }}"

/** fromTheCard are the inputs a column trigger fills from its card. */
function fromTheCard(values: WorkflowValues): string[] {
  const inputs = (triggersOf(values)["task.entered_column"]?.inputs ?? {}) as Record<string, unknown>
  return Object.entries(inputs).filter(([, value]) => typeof value === "string" && value.replace(/\s+/g, "") === CARD_TASK.replace(/\s+/g, "")).map(([name]) => name)
}

const isTyping = (target: EventTarget | null) => {
  const element = target as HTMLElement | null
  return Boolean(element && (element.tagName === "INPUT" || element.tagName === "TEXTAREA" || element.tagName === "SELECT" || element.isContentEditable))
}

const executorTitle = (type: unknown) => executors.find(e => e.type === type)?.title ?? String(type ?? "")

const NO_SWITCHES: Record<string, boolean> = {}

// A row's node in the run graph, and a node's row: trigger:<kind> and
// step:<id> (a review loop's inner nodes are their step's).
function nodeOf(selection: Selection | null): string | null {
  if (selection?.kind === "step") return `step:${selection.path}`
  if (selection?.kind === "trigger") return `trigger:${selection.name}`
  return null
}
function selectionOfNode(graph: WorkflowGraphModel, id: string | null): Selection | null {
  const node = graph.nodes.find(n => n.id === id)
  const own = node?.kind === "inner" ? graph.nodes.find(n => n.id === node.parent) : node
  if (own?.kind === "step" && own.step) return { kind: "step", path: own.step.id }
  if (own?.kind === "trigger" && own.trigger && own.trigger.kind !== "manual") return { kind: "trigger", name: own.trigger.kind }
  return null
}

// iconOf is a step type's icon in its row.
function iconOf(type: string) {
  if (type === "wait") return Hourglass
  if (type.startsWith("agent.")) return Bot
  if (type.startsWith("github.")) return GitPullRequest
  if (type.startsWith("task.")) return ListChecks
  if (type.startsWith("repo.") || type.startsWith("worktree.")) return FolderGit2
  return FileText
}

// rowsOf are the outline's rows, section by section, from the file's
// items: a trigger, input or step each, and About for the top-level fields.
function rowsOf(items: Item[], values: WorkflowValues, problems: Map<string, WorkflowDiagnostic[]>, pausedHere: Record<string, boolean>) {
  const fromCard = fromTheCard(values)
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
      const sentence = item.problem ? undefined : triggerSentence(ref.name, trigger)
      const paused = ref.name in pausedHere ? pausedHere[ref.name] : trigger?.enabled === false
      when.push({ selection: ref, label: sentence ? sentence.map(p => p.text).join("") : KIND_TITLES[ref.name] ?? ref.name, sentence, detail: "", problem: item.problem, problems: count(ref), paused, on: !paused })
    } else if (ref.kind === "input") {
      asks.push({ selection: ref, label: ref.name, detail: fromCard.includes(ref.name) ? "from the card" : String(inputs[ref.name]?.type ?? ""), problem: item.problem, problems: count(ref) })
    } else if (ref.kind === "step") {
      const step = steps.get(ref.path) ?? {}
      const depth = ref.path.split("/").length - 1
      then.push({ selection: ref, label: valueText(step.title) || ref.path.split("/").pop() || "", detail: executorTitle(step.type), problem: item.problem, problems: count(ref), summary: item.problem ? "" : stepSummary(step as StepValue), type: String(step.type ?? ""), depth })
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
  const { text, onEdit, diagnostics = [], nameFixed, onEditYaml, options = noOptions, schedule, selection, onSelect, narrow = false, sheet = false, onCloseSheet, onUndo, onRedo, savedText = null, pausedHere = NO_SWITCHES, mode = "edit", cursor, onJump, broken, graph = null } = props
  const navigating = mode === "navigate"
  const editing = !navigating && !broken
  const { file, items } = useMemo(() => itemsOf(text), [text])
  const values = useMemo(() => parseWorkflow(text).values, [text])
  const problems = useMemo(() => problemsByItem(text, diagnostics), [text, diagnostics])
  const [error, setError] = useState("")
  // An item's YAML that does not read yet: kept by the item's key with the
  // text it was begun from, by the page (so leaving the tab, the row or
  // Visual keeps it) or else here, and counted as unsaved. It is shown only
  // while its item still has that text, so a row that moves or changes
  // never takes another's.
  const [ownHeld, setOwnHeld] = useState<HeldYaml>({})
  const heldStore = props.held ?? ownHeld
  const setHeldStore = props.onHeld ?? setOwnHeld
  const report = useContext(PendingDrafts)
  const heldAny = Object.keys(heldStore).length > 0
  useEffect(() => { report("outline-held-yaml", heldAny) }, [report, heldAny])
  useEffect(() => () => report("outline-held-yaml", false), [report])
  const [adding, setAdding] = useState<{ kind: "step"; at: number } | { kind: "trigger" } | null>(null)
  const [menu, setMenu] = useState<string | null>(null)
  const [pasted, setPasted] = useState<string | null>(null)
  // List | Map: the outline, or the run graph (read-only) in its place.
  const [view, setView] = useState<"list" | "map">("list")
  const [confirmRemove, setConfirmRemove] = useState<string | null>(null)
  const [warning, setWarning] = useState("")
  const [dragged, setDragged] = useState<string | null>(null)
  // A step just added has its first essential field focused.
  const [focusNew, setFocusNew] = useState<string | null>(null)
  const outline = useRef<HTMLElement>(null)
  const inspector = useRef<HTMLElement>(null)

  const rows = useMemo(() => rowsOf(items, values, problems, pausedHere), [items, values, problems, pausedHere])
  const savedIds = useMemo(() => savedText === null ? null : new Set(stepsOf(parseWorkflow(savedText).values).map(step => String(step?.id ?? ""))), [savedText])
  const order: Row[] = useMemo(() => [...rows.when, ...rows.asks, ...rows.then, { selection: { kind: "about" }, label: "About", detail: "", problem: rows.about.problem, problems: rows.about.problems }], [rows])

  // A rename moves a row's address: follow it to the row of its kind in
  // the same place.
  const [last, setLast] = useState<{ address: string; place: number } | null>(null)
  const atCursor = navigating && cursor !== undefined ? pathAt(text, cursor)?.item ?? null : null
  const shown: Selection | null = navigating ? (atCursor ? (atCursor.kind === "about" ? { kind: "about" } : atCursor) : null) : selection
  const found = selection ? order.findIndex(row => sameSelection(selection, row.selection)) : -1
  // Until the address catches up, the renamed row is still the one shown,
  // so its inspector (and the tab it is on) stays.
  const following = selection && found < 0 && last?.address === addressOf(selection) ? order.filter(row => row.selection.kind === selection.kind)[last.place] ?? null : null
  useEffect(() => {
    if (navigating || broken) return
    if (!selection) { setLast(null); return }
    const same = order.filter(row => row.selection.kind === selection.kind)
    const address = addressOf(selection)
    if (found >= 0) {
      const place = same.findIndex(row => sameSelection(selection, row.selection))
      setLast(previous => previous?.address === address && previous.place === place ? previous : { address, place })
      return
    }
    if (following) onSelect(following.selection)
  }, [selection, found, order, onSelect, following, navigating, broken])

  // heldYaml is the held text by the row's place, which the inspector is
  // keyed by; hold keeps or drops a row's.
  const heldYaml = useMemo(() => {
    const out: Record<string, string> = {}
    for (const row of order) {
      if (row.selection.kind === "about") continue
      const kept = heldStore[itemKey(row.selection)]
      if (kept && itemText(text, row.selection) === kept.base) out[placeOf(order, row.selection)] = kept.text
    }
    return out
  }, [order, heldStore, text])
  const hold = useCallback((place: string, held: string | null) => {
    const row = order.find(r => placeOf(order, r.selection) === place)
    if (!row || row.selection.kind === "about") return
    const key = itemKey(row.selection)
    const base = itemText(text, row.selection) ?? ""
    setHeldStore(previous => {
      if (held === null) { if (!(key in previous)) return previous; const next = { ...previous }; delete next[key]; return next }
      return { ...previous, [key]: { base: previous[key]?.base === base ? previous[key].base : base, text: held } }
    })
  }, [order, text, setHeldStore])

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
      <Button size="sm" variant="outline" onClick={() => onEditYaml()}>Edit as YAML</Button>
    </div>
  }

  // A step that is not a mapping keeps its place, and is no one's reader.
  const placed = stepsOf(values) as (StepValue | null)[]
  const steps = placed.filter((step): step is StepValue => step !== null && typeof step === "object")
  const ids = steps.map(step => step.id ?? "")
  const select = (next: Selection, open = false) => {
    setAdding(null)
    if (!navigating) { onSelect(next, { open }); return }
    // Beside the YAML, a row moves the cursor to its text.
    const item = next.kind === "about" ? items.find(i => i.item.kind === "about" && !["inputs", "on", "steps"].includes(i.item.key))?.item : next
    const range = item ? rangeOf(text, item) : null
    if (range) onJump?.(range)
  }

  // What is pasted on the forms (not into a field): a whole workflow asks
  // to replace this one; steps go in after the step chosen.
  const onPaste = (event: React.ClipboardEvent) => {
    if (!editing || isTyping(event.target)) return
    const clip = event.clipboardData.getData("text/plain")
    const what = pastedAs(clip)
    if (what.kind === "other") return
    event.preventDefault()
    if (what.kind === "workflow") { setPasted(clip); return }
    const after = selection?.kind === "step" ? placed.findIndex(step => step?.id === selection.path.split("/")[0]) + 1 : placed.length
    apply(() => {
      let working = text
      const taken = [...ids]
      // The IDs first, then the group's references to its own renamed
      // steps follow them; a reference to a step not pasted is left alone.
      const added = what.steps.map(raw => {
        const base = typeof raw.id === "string" && raw.id !== "" ? raw.id : idFromTitle(String(raw.title ?? raw.type ?? "step"), taken)
        let id = base
        for (let n = 2; taken.includes(id); n++) id = `${base}-${n}`
        taken.push(id)
        return id
      })
      const renamed = new Map(what.steps.flatMap((raw, i) => typeof raw.id === "string" && raw.id !== added[i] ? [[raw.id, added[i]] as const] : []))
      const follow = (value: unknown): unknown => {
        if (typeof value === "string") return renamed.size === 0 ? value : value.replace(/\$\{\{[^}]*\}\}/g, expression => expression.replace(/(\bsteps\.)([^.\s}]+)(?=\.outputs\b)/g, (all, head: string, id: string) => renamed.has(id) ? head + renamed.get(id) : all))
        if (Array.isArray(value)) return value.map(follow)
        if (value && typeof value === "object") return Object.fromEntries(Object.entries(value).map(([k, v]) => [k, follow(v)]))
        return value
      }
      what.steps.forEach((raw, i) => {
        working = insertStep(working, (after <= 0 ? placed.length : after) + i, { ...(follow(raw) as Record<string, unknown>), id: added[i] }).text
      })
      if (added[0]) setTimeout(() => select({ kind: "step", path: added[0] }), 0)
      return { text: working, undo: { label: `Paste ${added.length === 1 ? `step ${added[0]}` : `${added.length} steps`}`, before: text, after: working } }
    })
  }

  // A new step's ID is made from its title, and follows it until saved.
  const addStep = (type: string, at: number) => {
    const made = newStep(type, ids)
    const step = { ...made, id: idFromTitle(String(made.title ?? type), ids) }
    apply(() => insertStep(text, at, step))
    select({ kind: "step", path: step.id }, true)
    setFocusNew(step.id)
  }
  const moveTo = (id: string, to: number) => {
    const order = ids.filter(other => other !== id)
    order.splice(to, 0, id)
    const broken = brokenReferences(steps, order)
    if (broken.length > 0) { setWarning(`That move would break a reference: ${broken.join("; ")}.`); return }
    setWarning("")
    apply(() => moveStep(text, id, to))
  }
  const removeIt = (id: string) => {
    const readers = steps.filter(step => step.id !== id && stepReferences(step).some(ref => ref.id === id))
    if (readers.length > 0 && confirmRemove !== id) { setConfirmRemove(id); return }
    setConfirmRemove(null)
    apply(() => removeStep(text, id))
    if (selection?.kind === "step" && selection.path === id) onSelect(null)
  }
  const duplicate = (step: StepValue, index: number) => {
    const copy = duplicateOf(step, ids)
    apply(() => insertStep(text, index + 1, copy))
    select({ kind: "step", path: String(copy.id) }, true)
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

  const rowButton = (row: Row, bare = false, number?: number) => {
    const selected = sameSelection(shown, row.selection)
    const address = addressOf(row.selection)
    const Icon = row.type ? iconOf(row.type) : null
    const button = <button type="button" data-address={address} aria-current={selected ? "true" : undefined}
      className={`flex min-h-10 w-full min-w-0 items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm ${selected ? "bg-lw-raised text-lw-text" : "text-lw-text-2 hover:bg-lw-raised hover:text-lw-text"}`}
      onClick={() => select(row.selection, true)}>
      {number !== undefined && <span className="w-4 shrink-0 text-right text-xs text-lw-faint">{number}</span>}
      {Icon && <Icon size={14} aria-hidden className="shrink-0 text-lw-faint" />}
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="truncate">{row.sentence ? row.sentence.map((part, i) => part.strong ? <strong key={i} className="font-semibold text-lw-text">{part.text}</strong> : <span key={i}>{part.text}</span>) : row.label}</span>
        {row.summary && <span className="truncate text-xs text-lw-faint">{row.summary}</span>}
      </span>
      {row.detail && !row.summary && <span className="truncate text-xs text-lw-faint">{row.detail}</span>}
      {row.paused && <span className="rounded border border-lw-line px-1 text-[11px] text-lw-text-2">paused</span>}
      {row.on && <span className="rounded bg-lw-live-soft px-1 text-[11px] text-lw-live">on</span>}
      {row.problem && <span className="flex items-center gap-1 rounded border border-lw-line px-1 text-[11px] text-lw-text-2"><Code2 size={11} />Edited as YAML</span>}
      {row.problems > 0 && <span aria-label={`${row.problems} ${row.problems === 1 ? "problem" : "problems"}`} className="flex items-center gap-0.5 text-[11px] text-lw-danger"><AlertTriangle size={11} />{row.problems}</span>}
    </button>
    return bare ? button : <li key={address}>{button}</li>
  }

  const palette = adding?.kind === "step" ? <StepPicker narrow={narrow} onPick={type => addStep(type, adding.at)} onClose={() => setAdding(null)} /> : null

  // A top-level step's row, with the connector before it that says how
  // it starts and inserts a step there. Nested steps are plain rows.
  const stepRow = (row: Row) => {
    if (row.selection.kind !== "step" || (row.depth ?? 0) > 0) return <li key={addressOf(row.selection)} className="pl-6">{rowButton(row, true)}</li>
    const id = row.selection.path
    // A step with no ID is #<index>, its place (stepNodes).
    const index = /^#\d+$/.test(id) ? Number(id.slice(1)) : placed.findIndex(step => step?.id === id)
    const step = (placed[index] ?? {}) as StepValue
    const connector = connectorOf(placed.map(p => (p ?? {}) as Record<string, unknown>), index)
    const title = row.label
    const readers = steps.filter(other => other.id !== id && stepReferences(other).some(ref => ref.id === id)).map(other => String(other.id))
    return <li key={addressOf(row.selection)} className="group flex min-w-0 flex-col"
      draggable={editing && !narrow} onDragStart={() => setDragged(id)} onDragOver={event => event.preventDefault()}
      onDrop={event => { event.preventDefault(); if (dragged && dragged !== id) moveTo(dragged, index); setDragged(null) }}>
      <div className={`flex min-h-7 items-center gap-2 pl-5 text-[11px] ${connector.kind === "manual" ? "text-lw-needs" : "text-lw-faint"}`}>
        <span aria-hidden className={`h-5 border-l ${connector.kind === "first" ? "invisible " : ""}${connector.kind === "agent_ended" ? "border-dashed border-lw-line-strong" : connector.kind === "manual" ? "border-lw-needs" : "border-lw-line"}`} />
        <span data-start={connector.kind} className="flex-1">{connector.text}</span>
        {editing && <button type="button" aria-label={`Add a step before ${title}`} className={`rounded px-1 text-lw-text-2 hover:bg-lw-raised hover:text-lw-text ${narrow ? "" : "opacity-0 focus:opacity-100 group-hover:opacity-100"}`} onClick={() => setAdding({ kind: "step", at: index })}><Plus size={13} /></button>}
      </div>
      {adding?.kind === "step" && adding.at === index && palette}
      <div className="relative flex min-w-0 items-center">
        {rowButton(row, true, index + 1)}
        {editing && <button type="button" aria-label={`Actions for step ${id}`} aria-expanded={menu === id} className="ml-1 rounded p-1 text-lw-text-2 hover:bg-lw-raised" onClick={() => setMenu(menu === id ? null : id)}><MoreVertical size={14} /></button>}
        {menu === id && <div role="menu" aria-label={`Step ${id}`} className="absolute right-0 top-full z-20 flex w-44 flex-col rounded-md border border-lw-line bg-lw-surface p-1 text-sm shadow-xl" onKeyDown={event => { if (event.key === "Escape") setMenu(null) }}>
          <button type="button" role="menuitem" className="flex items-center gap-2 rounded px-2 py-1.5 text-left hover:bg-lw-raised" onClick={() => { setMenu(null); duplicate(step, index) }}><Copy size={13} />Duplicate</button>
          <button type="button" role="menuitem" disabled={index === 0} className="flex items-center gap-2 rounded px-2 py-1.5 text-left hover:bg-lw-raised disabled:opacity-40" onClick={() => { setMenu(null); moveTo(id, index - 1) }}><ArrowUp size={13} />Move up</button>
          <button type="button" role="menuitem" disabled={index === placed.length - 1} className="flex items-center gap-2 rounded px-2 py-1.5 text-left hover:bg-lw-raised disabled:opacity-40" onClick={() => { setMenu(null); moveTo(id, index + 1) }}><ArrowDown size={13} />Move down</button>
          <button type="button" role="menuitem" className="flex items-center gap-2 rounded px-2 py-1.5 text-left text-lw-danger hover:bg-lw-raised" onClick={() => { setMenu(null); removeIt(id) }}><Trash2 size={13} />Delete</button>
        </div>}
      </div>
      {confirmRemove === id && <div role="alertdialog" aria-label={`Remove step ${id}`} className="mx-2 my-1 flex flex-wrap items-center gap-2 rounded border border-lw-needs p-2 text-xs">
        <span>{readers.join(", ")} {readers.length === 1 ? "reads" : "read"} this step's outputs.</span>
        <Button type="button" size="sm" variant="destructive" onClick={() => removeIt(id)}>Remove anyway</Button>
        <Button type="button" size="sm" variant="ghost" onClick={() => setConfirmRemove(null)}>Keep it</Button>
      </div>}
    </li>
  }

  const section = (title: string, body: React.ReactNode) => <section aria-label={title} className="flex flex-col gap-1">
    <h2 className="px-2 text-[11px] font-semibold uppercase tracking-wide text-lw-faint">{title}</h2>
    {body}
  </section>
  const container = (key: string) => rows.about.containers.filter(item => item.item.kind === "about" && item.item.key === key).map(item => <p key={key} role="alert" className="mx-2 flex flex-wrap items-center gap-2 text-xs text-lw-needs">
    {item.problem} <Button size="sm" variant="ghost" onClick={() => onEditYaml()}>Edit as YAML</Button>
  </p>)

  const missing = triggerKinds.filter(t => !(t.kind in triggersOf(values)))
  const outlineView = <nav ref={outline} aria-label="Outline" onKeyDown={onKeyDown} className="flex min-w-0 flex-col gap-4">
    {section("When", <>
      <ul className="flex flex-col">{rows.when.map(row => rowButton(row))}</ul>
      {container("on")}
      {editing && missing.length > 0 && (adding?.kind === "trigger"
        ? <div role="dialog" aria-label="Add a trigger" className="mx-2 flex flex-col gap-1 rounded-lg border border-lw-line bg-lw-surface p-1 shadow-xl" onKeyDown={event => { if (event.key === "Escape") setAdding(null) }}>
          {missing.map(t => <button key={t.kind} type="button" className="flex min-h-9 flex-col items-start rounded-md px-2 py-1 text-left hover:bg-lw-raised" onClick={() => addTrigger(t.kind)}>
            <span className="text-sm">{KIND_TITLES[t.kind] ?? t.kind}</span>
            <span className="text-xs text-lw-text-2">{t.description}</span>
          </button>)}
          <Button type="button" size="sm" variant="ghost" className="self-start" onClick={() => setAdding(null)}>Cancel</Button>
        </div>
        : <Button type="button" size="sm" variant="ghost" className="self-start" onClick={() => setAdding({ kind: "trigger" })}><Plus size={13} />Add trigger</Button>)}
      <p className="px-2 text-xs text-lw-faint">You can always run it by hand.</p>
    </>)}
    {section("Asks for", <>
      {rows.asks.length === 0 && <p className="px-2 text-sm text-lw-text-2">Nothing: it takes no inputs</p>}
      <ul className="flex flex-col">{rows.asks.map(row => rowButton(row))}</ul>
      {container("inputs")}
      {editing && <Button type="button" size="sm" variant="ghost" className="self-start" onClick={addAnInput}><Plus size={13} />Add input</Button>}
    </>)}
    {section("Then", <>
      {rows.then.length === 0 && <p className="px-2 text-sm text-lw-text-2">No steps yet: a workflow needs at least one.</p>}
      {warning && <p role="alert" className="px-2 text-xs text-lw-needs">{warning}</p>}
      <ol aria-label="Steps" className="flex flex-col">{rows.then.map(stepRow)}</ol>
      {container("steps")}
      {editing && (adding?.kind === "step" && adding.at >= placed.length ? palette : <Button type="button" size="sm" variant="ghost" className="self-start" onClick={() => setAdding({ kind: "step", at: placed.length })}><Plus size={13} />Add step</Button>)}
    </>)}
    {section("About", <ul className="flex flex-col">{rowButton(order[order.length - 1])}</ul>)}
  </nav>

  const listOrMap = <div role="radiogroup" aria-label="Outline view" className="inline-flex self-start rounded-md border border-lw-line p-0.5">
    {(["list", "map"] as const).map(option => <button key={option} type="button" role="radio" aria-checked={view === option} disabled={option === "map" && !graph}
      title={option === "map" && !graph ? "The map shows once Legato has read the file" : undefined}
      className={`min-h-7 rounded px-2.5 text-xs disabled:opacity-40 ${view === option ? "bg-lw-raised text-lw-text" : "text-lw-text-2"}`} onClick={() => setView(option)}>{option === "list" ? "List" : "Map"}</button>)}
  </div>
  const mapView = graph && <section aria-label="Map" className="flex flex-col gap-1">
    {graph.stale && <p role="status" className="text-xs text-lw-needs">The map is of the last version Legato could read.</p>}
    <div className={narrow ? "h-[60vh] overflow-hidden rounded-lg border border-lw-line bg-lw-ground" : "h-[32rem] overflow-hidden rounded-lg border border-lw-line bg-lw-ground"}>
      <LazyWorkflowGraph graph={graph.model} selectedId={nodeOf(shown)} direction="TB" onSelect={id => { const next = selectionOfNode(graph.model, id); if (next) select(next, true) }} />
    </div>
  </section>
  const navigation = <div className="flex min-w-0 flex-col gap-2">
    {listOrMap}
    {view === "map" && mapView ? mapView : outlineView}
  </div>

  const selectedRow = found >= 0 ? order[found] : following
  const inspectorView = <section ref={inspector} aria-label="Inspector" className="flex min-w-0 flex-col gap-3">
    {narrow && <Button type="button" size="sm" variant="ghost" className="self-start" onClick={onCloseSheet}><ArrowLeft size={13} />Outline</Button>}
    {selectedRow
      ? <Inspector key={placeOf(order, selectedRow.selection)} place={placeOf(order, selectedRow.selection)} held={heldYaml[placeOf(order, selectedRow.selection)]} onHold={hold} row={selectedRow} savedIds={savedIds} focusEssential={selectedRow.selection.kind === "step" && selectedRow.selection.path === focusNew} onFocused={() => setFocusNew(null)} text={text} values={values} diagnostics={diagnostics} problems={problems} options={options} apply={apply} nameFixed={nameFixed} schedule={schedule} onSelect={onSelect} onUndo={onUndo} onRedo={onRedo} />
      : <p className="text-sm text-lw-text-2">Choose a row of the outline to edit it.</p>}
  </section>

  const brokenNote = broken && <p role="alert" className="flex flex-wrap items-center gap-2 rounded-lg border border-lw-danger-line bg-lw-error-soft px-3 py-2 text-xs text-lw-danger">
    <span className="min-w-0 flex-1">{broken.line ? `Line ${broken.line}: ` : ""}can't read the YAML. The outline is as it last could be; the forms are off until it reads again.</span>
    <Button size="sm" variant="outline" onClick={() => onEditYaml(broken.line)}>Show</Button>
  </p>
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
      <div inert={Boolean(broken) || undefined} className={broken ? "opacity-50" : undefined}>{broken ? outlineView : navigation}</div>
    </div>
  }
  if (broken) {
    return <div className="flex flex-col gap-3 text-sm">
      {brokenNote}
      <div inert className="rounded-xl border border-lw-line bg-lw-surface p-2 opacity-50">{outlineView}</div>
    </div>
  }

  return <div className="flex flex-col gap-3 text-sm" onPaste={onPaste}>
    {error && <p role="alert" className="text-sm text-lw-danger">{error}</p>}
    {pasted !== null && <div role="alertdialog" aria-label="Replace the workflow" className="flex flex-wrap items-center gap-2 rounded-lg border border-lw-needs bg-lw-surface p-3 text-sm">
      <span className="min-w-0 flex-1">You pasted a whole workflow. Replace this one with it? Undo brings this one back.</span>
      <Button size="sm" onClick={() => { const next = pasted; setPasted(null); onEdit({ text: next, undo: { label: "Paste a workflow", before: text, after: next } }) }}>Replace</Button>
      <Button size="sm" variant="ghost" onClick={() => setPasted(null)}>Cancel</Button>
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
