import { getTextWidth } from "@evenrealities/pretext"
import { ITEM_TEXT_WIDTH } from "./layout"
import type { Agent } from "./legato"

export { ITEM_TEXT_WIDTH }
// The firmware's limits on a list container (the SDK's Display docs and the
// simulator's changelog): items, and bytes of UTF-8 to an item.
export const LIST_ITEMS = 20
export const LIST_ITEM_BYTES = 63
// The most characters textContainerUpgrade takes (the SDK's Display docs).
export const TEXT_UPDATE_LIMIT = 2000

export type Tab = "active" | "idle"

// An item of the list: it opens an agent or the Reeve's chat, goes to the
// other tab, or only says something.
export type Item = { text: string; open?: string; go?: Tab; reeve?: true; approvals?: true }

// What the list page shows.
export type Screen = {
  tab: Tab
  tabs: { active: string; idle: string }
  // Why the list may be stale, or "".
  status: string
  // When the list was read, as HH:MM, after how old it is once that is a
  // minute or more ("3m · HH:MM"); or "".
  clock: string
  items: Item[]
}

export type GlanceOptions = {
  tab?: Tab
  updatedAt?: Date
  // The time now, for how old the list is.
  now?: Date
  problem?: string
  // How many approvals wait for the operator.
  approvals?: number
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

// How long ago, once it is a minute or more: in minutes, then an hour or more.
function age(seconds: number): string {
  if (seconds < 60) return ""
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  return "1h+"
}

function clock(at: Date | undefined, now: Date): string {
  if (!at) return ""
  const two = (n: number) => String(n).padStart(2, "0")
  const old = age(Math.max(0, (now.getTime() - at.getTime()) / 1000))
  const time = `${two(at.getHours())}:${two(at.getMinutes())}`
  return old ? `${old} · ${time}` : time
}

// The agents' items, then the way to the other tab, within `limit` items.
function list(agents: Item[], nav: Item | undefined, limit = LIST_ITEMS): Item[] {
  const room = limit - (nav ? 1 : 0)
  const shown =
    agents.length > room ? [...agents.slice(0, room - 1), { text: `+${agents.length - room + 1} more` }] : agents
  return nav ? [...shown, nav] : shown
}

// The Reeve's state in its own words: it is ready rather than idle, and
// asleep when not running (a message wakes it).
const reeveWords: Record<string, string> = { "": "ready", working: "working" }

// The Reeve's item, pinned first on the active tab. `reeve` is its running
// agent, if any.
export function reeveItem(reeve: Agent | undefined): Item {
  const word = reeve ? (reeveWords[stateOf(reeve).activity] ?? stateOf(reeve).word) : "asleep"
  return { text: fitItem(`◆ Reeve  ${word}`), reeve: true }
}

// The list page for the agents Legato listed: those still running, on two
// tabs. The active tab holds the Reeve first, then the agents not idle, those
// that need the operator first, then running and background, each group
// oldest first; the idle tab holds the rest. The Reeve is on neither count.
export function glance(agents: Agent[], options: GlanceOptions = {}): Screen {
  const tab = options.tab ?? "active"
  const rank = (a: Agent) => states.indexOf(stateOf(a))
  const reeve = agents.find((a) => a.reeve && a.status === "running")
  const running = agents
    .filter((a) => a.status === "running" && !a.reeve)
    .sort((a, b) => rank(a) - rank(b) || a.started_at.localeCompare(b.started_at))
  const active = running.filter((a) => !isIdle(a))
  const idle = running.filter(isIdle)

  let items: Item[]
  if (tab === "active") {
    const nav = idle.length ? { text: `Idle agents (${idle.length}) →`, go: "idle" as const } : undefined
    const rows = active.map((a) => ({ text: label(a, true), open: a.task_id }))
    // The Reeve first, then the way to the approvals while any wait.
    const pinned: Item[] = [reeveItem(reeve)]
    if (options.approvals) pinned.push({ text: `● To approve (${options.approvals}) →`, approvals: true })
    if (rows.length) items = [...pinned, ...list(rows, nav, LIST_ITEMS - pinned.length)]
    else items = [...pinned, ...(nav ? [{ text: "Nothing running." }, nav] : [{ text: "No agents running." }])]
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
    clock: clock(options.updatedAt, options.now ?? new Date()),
    items,
  }
}
