import { Bot, FileText, FolderGit2, GitPullRequest, Hourglass, ListChecks } from "lucide-react"
import { inputsOf, itemKey, parseWorkflow, stepsOf, triggersOf, type Item, type ItemRef, type WorkflowValues } from "../../lib/workflowDocument"
import { sameSelection, type Selection } from "../../lib/workflowAddress"
import { executors } from "../../lib/workflowSchema"
import type { WorkflowDiagnostic } from "../../lib/workflowFiles"
import type { WorkflowGraphModel } from "../../lib/workflow"
import { valueText } from "./builderStyles"
import type { StepValue } from "./stepForms"
import { stepSummary } from "./stepRows"
import { KIND_TITLES, triggerSentence, type SentencePart } from "./triggerForms"

// The outline's rows and how they map to the run graph, apart from the
// components that draw them.

export type Row = { selection: Selection; label: string; detail: string; problem: string | null; problems: number; paused?: boolean; on?: boolean; sentence?: SentencePart[]; summary?: string; type?: string; depth?: number }

export const CARD_TASK = "${{ event.task }}"

// The top-level keys whose items are the outline's own rows.
const CONTAINER_KEYS = ["inputs", "on", "steps"]

/** isContainerKey is a top-level key the outline shows as its own rows
 * (inputs, on, steps), not under About. */
export const isContainerKey = (key: string) => CONTAINER_KEYS.includes(key)

/** placeOf is a row's kind and position among the rows of its kind. */
export function placeOf(order: Row[], selection: Selection): string {
  const same = order.filter(row => row.selection.kind === selection.kind)
  return `${selection.kind}:${same.findIndex(row => sameSelection(selection, row.selection))}`
}

/** fromTheCard are the inputs a column trigger fills from its card. */
export function fromTheCard(values: WorkflowValues): string[] {
  const inputs = (triggersOf(values)["task.entered_column"]?.inputs ?? {}) as Record<string, unknown>
  return Object.entries(inputs).filter(([, value]) => typeof value === "string" && value.replace(/\s+/g, "") === CARD_TASK.replace(/\s+/g, "")).map(([name]) => name)
}

/** isTyping is an event's target taking text, which keeps its keys. */
export const isTyping = (target: EventTarget | null) => {
  const element = target as HTMLElement | null
  return Boolean(element && (element.tagName === "INPUT" || element.tagName === "TEXTAREA" || element.tagName === "SELECT" || element.isContentEditable))
}

export const executorTitle = (type: unknown) => executors.find(e => e.type === type)?.title ?? String(type ?? "")

/** nodeOf is a row's node in the run graph: trigger:<kind> and step:<id>. */
export function nodeOf(selection: Selection | null): string | null {
  if (selection?.kind === "step") return `step:${selection.path}`
  if (selection?.kind === "trigger") return `trigger:${selection.name}`
  return null
}

/** selectionOfNode is a node's row (a review loop's inner nodes are their
 * step's). */
export function selectionOfNode(graph: WorkflowGraphModel, id: string | null): Selection | null {
  const node = graph.nodes.find(n => n.id === id)
  const own = node?.kind === "inner" ? graph.nodes.find(n => n.id === node.parent) : node
  if (own?.kind === "step" && own.step) return { kind: "step", path: own.step.path || own.step.id }
  if (own?.kind === "trigger" && own.trigger && own.trigger.kind !== "manual") return { kind: "trigger", name: own.trigger.kind }
  return null
}

/** selectionAt is a node's row in the graph there is, if any. */
export function selectionAt(graph: { model: WorkflowGraphModel } | null, id: string | null): Selection | null {
  return graph ? selectionOfNode(graph.model, id) : null
}

/** savedIdsOf are the IDs of the steps saved (null: none yet). */
export function savedIdsOf(savedText: string | null): Set<string> | null {
  if (savedText === null) return null
  return new Set(stepsOf(parseWorkflow(savedText).values).map(step => String(step?.id ?? "")))
}

/** isStepAt is a row the step at path. */
export const isStepAt = (row: Row, path: string | null) => row.selection.kind === "step" && row.selection.path === path

/** iconOf is a step type's icon in its row. */
export function iconOf(type: string) {
  if (type === "wait") return Hourglass
  if (type.startsWith("agent.")) return Bot
  if (type.startsWith("github.")) return GitPullRequest
  if (type.startsWith("task.")) return ListChecks
  if (type.startsWith("repo.") || type.startsWith("worktree.")) return FolderGit2
  return FileText
}

const isStepId = (step: unknown): step is { id: string } =>
  Boolean(step) && typeof step === "object" && typeof (step as { id?: unknown }).id === "string" && (step as { id: string }).id !== ""

// stepsByPath are the file's steps by their path, nested steps under their
// parent's: a step with no ID is #<index>, its place.
function stepsByPath(list: unknown, parent = "", out = new Map<string, Record<string, unknown>>()) {
  if (!Array.isArray(list)) return out
  list.forEach((step, i) => {
    const id = isStepId(step) ? step.id : `#${i}`
    const path = parent ? `${parent}/${id}` : id
    out.set(path, (step ?? {}) as Record<string, unknown>)
    stepsByPath((step as { steps?: unknown } | null)?.steps, path, out)
  })
  return out
}

type RowContext = { values: WorkflowValues; fromCard: string[]; steps: Map<string, Record<string, unknown>>; pausedHere: Record<string, boolean>; count: (item: ItemRef) => number }

function triggerRow(item: Item, ref: Extract<ItemRef, { kind: "trigger" }>, context: RowContext): Row {
  const trigger = triggersOf(context.values)[ref.name]
  const sentence = item.problem ? undefined : triggerSentence(ref.name, trigger)
  const paused = ref.name in context.pausedHere ? context.pausedHere[ref.name] : trigger?.enabled === false
  const label = sentence ? sentence.map(p => p.text).join("") : KIND_TITLES[ref.name] ?? ref.name
  return { selection: ref, label, sentence, detail: "", problem: item.problem, problems: context.count(ref), paused, on: !paused }
}

function inputRow(item: Item, ref: Extract<ItemRef, { kind: "input" }>, context: RowContext): Row {
  const detail = context.fromCard.includes(ref.name) ? "from the card" : String(inputsOf(context.values)[ref.name]?.type ?? "")
  return { selection: ref, label: ref.name, detail, problem: item.problem, problems: context.count(ref) }
}

function stepRowOf(item: Item, ref: Extract<ItemRef, { kind: "step" }>, context: RowContext): Row {
  const step = context.steps.get(ref.path) ?? {}
  const depth = ref.path.split("/").length - 1
  const label = valueText(step.title) || ref.path.split("/").pop() || ""
  return { selection: ref, label, detail: executorTitle(step.type), problem: item.problem, problems: context.count(ref), summary: item.problem ? "" : stepSummary(step as StepValue), type: String(step.type ?? ""), depth }
}

export type OutlineRows = { when: Row[]; asks: Row[]; then: Row[]; about: { problems: number; problem: string | null; containers: Item[] } }

// addAbout counts a top-level item under About, or keeps a container the
// forms can't show for its section's own alert.
function addAbout(about: OutlineRows["about"], item: Item, ref: Extract<ItemRef, { kind: "about" }>, context: RowContext) {
  if (isContainerKey(ref.key)) {
    if (item.problem) about.containers.push(item)
    return
  }
  about.problems += context.count(ref)
  about.problem ??= item.problem
}

/** rowsOf are the outline's rows, section by section, from the file's
 * items: a trigger, input or step each, and About for the top-level
 * fields. */
export function rowsOf(items: Item[], values: WorkflowValues, problems: Map<string, WorkflowDiagnostic[]>, pausedHere: Record<string, boolean>): OutlineRows {
  const context: RowContext = {
    values, pausedHere,
    fromCard: fromTheCard(values),
    steps: stepsByPath(values.steps),
    count: item => (problems.get(itemKey(item)) ?? []).filter(d => d.severity === "error").length,
  }
  const rows: OutlineRows = { when: [], asks: [], then: [], about: { problems: 0, problem: null, containers: [] } }
  for (const item of items) {
    const ref = item.item
    if (ref.kind === "trigger") rows.when.push(triggerRow(item, ref, context))
    else if (ref.kind === "input") rows.asks.push(inputRow(item, ref, context))
    else if (ref.kind === "step") rows.then.push(stepRowOf(item, ref, context))
    else addAbout(rows.about, item, ref, context)
  }
  return rows
}

/** orderOf is every row in the outline's order, About last. */
export function orderOf(rows: OutlineRows): Row[] {
  return [...rows.when, ...rows.asks, ...rows.then, { selection: { kind: "about" }, label: "About", detail: "", problem: rows.about.problem, problems: rows.about.problems }]
}
