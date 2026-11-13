import { getTextWidth } from "@evenrealities/pretext"
import type { Agent } from "./legato"

// The glasses' canvas.
export const CANVAS_WIDTH = 576
export const CANVAS_HEIGHT = 288
// The firmware's limits on a list container (the SDK's Display docs and the
// simulator's changelog): items, and bytes of UTF-8 to an item.
export const LIST_ITEMS = 20
export const LIST_ITEM_BYTES = 63
// The width an item's text may take on the list, inside the firmware's
// highlight of the selected item.
export const ITEM_TEXT_WIDTH = CANVAS_WIDTH - 40
// The most characters textContainerUpgrade takes (the SDK's Display docs).
export const TEXT_UPDATE_LIMIT = 2000

export type Tab = "active" | "idle"

// An item of the list: it opens an agent, goes to the other tab, or only
// says something.
export type Item = { text: string; open?: string; go?: Tab }

// What the list page shows.
export type Screen = {
  tab: Tab
  tabs: { active: string; idle: string }
  // Why the list may be stale, or "".
  status: string
  // When the list was read, as HH:MM, or "".
  clock: string
  items: Item[]
}

export type GlanceOptions = {
  tab?: Tab
  updatedAt?: Date
  problem?: string
}

// The states in the order they are listed, with the TUI's words for them
// (internal/tui/agents/list.go) and a mark from the glyphs the G2 font has.
const states = [
  { activity: "waiting", mark: "●", word: "needs you" },
  { activity: "error", mark: "▲", word: "error" },
  { activity: "working", mark: "▶", word: "running" },
  { activity: "background", mark: "▷", word: "background" },
  { activity: "", mark: "○", word: "idle" },
] as const

type State = (typeof states)[number]

export function stateOf(a: Agent): State {
  return states.find((s) => s.activity === a.activity) ?? states[states.length - 1]
}

const isIdle = (a: Agent) => stateOf(a).activity === ""

const utf8 = new TextEncoder()

// Cuts text to the list's width and the firmware's bytes, ending it with an
// ellipsis when anything was cut.
export function fitItem(text: string, width = ITEM_TEXT_WIDTH, bytes = LIST_ITEM_BYTES): string {
  const fits = (s: string) => getTextWidth(s) <= width && utf8.encode(s).length <= bytes
  if (fits(text)) return text
  const chars = Array.from(text)
  while (chars.length && !fits(`${chars.join("").trimEnd()}…`)) chars.pop()
  return `${chars.join("").trimEnd()}…`
}

function label(a: Agent, withState: boolean): string {
  const s = stateOf(a)
  const title = a.title.trim()
  const head = withState ? `${s.mark} ${s.word}  ${a.task_id}` : `${s.mark} ${a.task_id}`
  return fitItem(`${head}${title ? ` ${title}` : ""}`)
}

function clock(at: Date | undefined): string {
  if (!at) return ""
  const two = (n: number) => String(n).padStart(2, "0")
  return `${two(at.getHours())}:${two(at.getMinutes())}`
}

// The agents' items, then the way to the other tab, within LIST_ITEMS.
function list(agents: Item[], nav: Item | undefined): Item[] {
  const room = LIST_ITEMS - (nav ? 1 : 0)
  const shown =
    agents.length > room ? [...agents.slice(0, room - 1), { text: `+${agents.length - room + 1} more` }] : agents
  return nav ? [...shown, nav] : shown
}

// The list page for the agents Legato listed: those still running, on two
// tabs. The active tab holds the ones not idle, those that need the operator
// first, then running and background, each group oldest first; the idle tab
// holds the rest.
export function glance(agents: Agent[], options: GlanceOptions = {}): Screen {
  const tab = options.tab ?? "active"
  const rank = (a: Agent) => states.indexOf(stateOf(a))
  const running = agents
    .filter((a) => a.status === "running")
    .sort((a, b) => rank(a) - rank(b) || a.started_at.localeCompare(b.started_at))
  const active = running.filter((a) => !isIdle(a))
  const idle = running.filter(isIdle)

  let items: Item[]
  if (tab === "active") {
    const nav = idle.length ? { text: `Idle agents (${idle.length}) →`, go: "idle" as const } : undefined
    const rows = active.map((a) => ({ text: label(a, true), open: a.task_id }))
    if (rows.length) items = list(rows, nav)
    else items = nav ? [{ text: "Nothing running." }, nav] : [{ text: "No agents running." }]
  } else {
    const rows = idle.map((a) => ({ text: label(a, false), open: a.task_id }))
    items = list(rows.length ? rows : [{ text: "No idle agents." }], {
      text: `← Active agents (${active.length})`,
      go: "active",
    })
  }

  return {
    tab,
    tabs: { active: `Active ${active.length}`, idle: `Idle ${idle.length}` },
    status: options.problem ?? "",
    clock: clock(options.updatedAt),
    items,
  }
}
