import type { WorkflowDayCount, WorkflowDefinition, WorkflowRun, WorkflowStats, WorkflowTriggerState } from "./workflow"
import { parseTime } from "./workflowView"

// What starts a workflow, as the Workflows page filters by it (LEG-657).
export type StartKind = "manual" | "schedule" | "chat" | "button" | "column" | "pr"
export const startKinds: { kind: StartKind; label: string }[] = [
  { kind: "manual", label: "Manual" },
  { kind: "schedule", label: "Schedule" },
  { kind: "chat", label: "Chat" },
  { kind: "button", label: "Button" },
  { kind: "column", label: "Column" },
  { kind: "pr", label: "Pull request" },
]

export function startKindOf(trigger: string): StartKind | null {
  if (trigger === "manual") return null
  if (trigger === "schedule") return "schedule"
  if (trigger === "chat.button") return "button"
  if (trigger.startsWith("chat.")) return "chat"
  if (trigger === "task.entered_column") return "column"
  if (trigger.startsWith("pr.")) return "pr"
  return null
}

// SwitchState is how a workflow's triggers stand in this Legato: all on,
// all paused, some of each, or none to switch (it is only run by hand).
export type SwitchState = { state: "on" | "paused" | "mixed" | "none"; on: number; total: number }

export interface WorkflowRow {
  definition: WorkflowDefinition
  tags: string[]
  kinds: Set<StartKind>
  // The Starts column: what starts it, then a detail such as its next time.
  start: { kind: StartKind | "called"; text: string; detail: string }
  switches: SwitchState
  states: WorkflowTriggerState[]
  // Inner: run only by other workflows (it has no trigger of its own).
  inner: boolean
  last?: WorkflowRun
  days: WorkflowDayCount[]
}

type GraphTrigger = { kind: string; summary: string; confirm?: boolean }

function triggersOf(definition: WorkflowDefinition): GraphTrigger[] {
  return (definition.graph?.nodes ?? [])
    .map(node => (node as { trigger?: GraphTrigger }).trigger)
    .filter((trigger): trigger is GraphTrigger => Boolean(trigger) && trigger!.kind !== "manual")
}

// nextText is a schedule's next due time, as short as is clear: its time
// within a day, else its date and time, in the schedule's own zone.
export function nextText(due: string, now: number): string {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}:\d{2})/.exec(due)
  if (!match) return ""
  const soon = parseTime(due) - now < 24 * 60 * 60 * 1000
  if (soon) return `next ${match[4]}`
  const month = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"][Number(match[2]) - 1]
  return `next ${Number(match[3])} ${month} ${match[4]}`
}

export function switchState(states: WorkflowTriggerState[]): SwitchState {
  const on = states.filter(state => state.enabled).length
  if (states.length === 0) return { state: "none", on, total: 0 }
  return { state: on === states.length ? "on" : on === 0 ? "paused" : "mixed", on, total: states.length }
}

export function switchLabel(switches: SwitchState): string {
  if (switches.state === "mixed") return `${switches.on} of ${switches.total} on`
  return switches.state === "on" ? "on" : "paused"
}

export function workflowRows(definitions: WorkflowDefinition[], triggerStates: Record<string, WorkflowTriggerState[]> | null | undefined, stats: WorkflowStats | null, now: number): WorkflowRow[] {
  const titles = new Map(definitions.map(definition => [definition.name, definition.title]))
  const days = stats?.days.length ?? 7
  return definitions.map(definition => {
    const triggers = triggersOf(definition)
    const states = triggerStates?.[definition.name] ?? []
    const callers = definition.called_by ?? []
    const inner = callers.length > 0 && triggers.length === 0
    const kinds = new Set(triggers.map(trigger => startKindOf(trigger.kind)).filter((kind): kind is StartKind => kind !== null))
    if (kinds.size === 0) kinds.add("manual")
    let start: WorkflowRow["start"]
    if (inner) {
      start = { kind: "called", text: `Run by ${callers.map(name => titles.get(name) ?? name).join(", ")}`, detail: "" }
    } else if (triggers.length === 0) {
      start = { kind: "manual", text: "Run by hand", detail: "" }
    } else {
      const first = triggers[0]
      const next = states.find(state => state.trigger === first.kind)?.schedule?.next?.[0]
      const details = [next ? nextText(next, now) : first.confirm ? "asks first" : "", triggers.length > 1 ? `+${triggers.length - 1} more` : ""].filter(Boolean)
      start = { kind: startKindOf(first.kind) ?? "manual", text: first.summary, detail: details.join(" · ") }
    }
    const stat = stats?.workflows[definition.name]
    return {
      definition, tags: definition.tags ?? [], kinds, start, states, inner,
      switches: switchState(states),
      last: stat?.last,
      days: stat?.days ?? Array.from({ length: days }, () => ({ runs: 0, failed: 0 })),
    }
  })
}

export interface Filters {
  query: string
  starts: StartKind | "all"
  triggers: "any" | "on" | "paused"
}
export const noFilters: Filters = { query: "", starts: "all", triggers: "any" }

export function matches(row: WorkflowRow, filters: Filters): boolean {
  const query = filters.query.trim().toLowerCase()
  const { title, name } = row.definition
  if (query && !(title.toLowerCase().includes(query) || name.toLowerCase().includes(query) || row.tags.some(tag => tag.includes(query)))) return false
  if (filters.starts !== "all" && !row.kinds.has(filters.starts)) return false
  if (filters.triggers === "on" && row.switches.on === 0) return false
  if (filters.triggers === "paused" && row.switches.on === row.switches.total) return false
  return true
}

export type SortBy = "title" | "last"

export function sortRows(rows: WorkflowRow[], by: SortBy): WorkflowRow[] {
  const lastTime = (row: WorkflowRow) => row.last ? parseTime(row.last.created_at) : -Infinity
  return [...rows].sort((a, b) => by === "last" ? lastTime(b) - lastTime(a) || a.definition.title.localeCompare(b.definition.title) : a.definition.title.localeCompare(b.definition.title))
}

export interface WorkflowGroup {
  // key is the tag, "" for Untagged, or "all" when not grouped.
  key: string
  tag: string
  rows: WorkflowRow[]
  inner: WorkflowRow[]
}

// groupRows gives a group per tag, in name order, then Untagged: a
// workflow with several tags is in each of their groups.
export function groupRows(rows: WorkflowRow[], by: SortBy): WorkflowGroup[] {
  const tags = [...new Set(rows.flatMap(row => row.tags))].sort()
  const groups = [...tags.map(tag => ({ key: tag, tag, list: rows.filter(row => row.tags.includes(tag)) })), { key: "", tag: "", list: rows.filter(row => row.tags.length === 0) }]
  return groups.filter(group => group.list.length > 0).map(group => ({
    key: group.key, tag: group.tag,
    rows: sortRows(group.list.filter(row => !row.inner), by),
    inner: sortRows(group.list.filter(row => row.inner), by),
  }))
}

export function allTags(definitions: WorkflowDefinition[]): string[] {
  return [...new Set(definitions.flatMap(definition => definition.tags ?? []))].sort()
}

// knownTags are the tags workflows have, with how many have each.
export function knownTags(definitions: WorkflowDefinition[]): Map<string, number> {
  return new Map(allTags(definitions).map(tag => [tag, definitions.filter(definition => definition.tags?.includes(tag)).length]))
}

// tagProblem says why a tag can't be added, as a workflow file's tags: are
// checked.
export function tagProblem(tag: string): string {
  if (!/^[a-z0-9][a-z0-9-]{0,31}$/.test(tag)) return "Use lower-case letters, digits and -, starting with a letter or digit, at most 32 characters"
  return ""
}

// errorText is an API error's message: its JSON's error, else its text.
export function errorText(reason: unknown): string {
  const text = reason instanceof Error ? reason.message : String(reason)
  try {
    const parsed = JSON.parse(text) as { error?: unknown }
    if (typeof parsed.error === "string") return parsed.error
  } catch { /* plain text */ }
  return text
}
