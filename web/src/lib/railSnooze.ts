import type { AttentionItem } from "./attention"
import { needsYouKind } from "./needsYou"

// The phone's needs-you sheet opens by itself when something waits. Once
// the operator closes it, it stays away for the snooze (web.rail.snooze),
// unless an item that holds work arrives first. The snooze is kept in this
// browser for each server, as when it ends and what the rail held at the
// close.
export type RailSnooze = { until: number; seen: string[] }

// What the sheet would open for: an item's ID, and whether it holds work.
export type SheetCandidate = { id: string; holdsWork: boolean }

// The snooze when the rail view does not say (an older server).
export const DEFAULT_SNOOZE_MS = 30 * 60 * 1000

const keyOf = (baseUrl: string) => `legato:rail-snooze:${baseUrl}`

// The snooze of this page, for a browser whose storage refuses it, and the
// servers whose last snooze storage refused to keep: for those, memory is
// newer than what storage holds.
const memory = new Map<string, RailSnooze>()
const unsaved = new Set<string>()

function parse(raw: string | null): RailSnooze | null | "bad" {
  if (raw === null) return null
  try {
    const value = JSON.parse(raw) as Partial<RailSnooze>
    if (typeof value.until !== "number" || !Array.isArray(value.seen)) return "bad"
    return { until: value.until, seen: value.seen.filter((id): id is string => typeof id === "string") }
  } catch {
    return "bad"
  }
}

// readSnooze is the snooze in force for a server at now, or null. Storage
// that cannot be read gives this page's own snooze, if any, so it fails to
// "not snoozed".
export function readSnooze(baseUrl: string, now = Date.now()): RailSnooze | null {
  let stored: RailSnooze | null | "bad"
  try {
    stored = unsaved.has(baseUrl) ? memory.get(baseUrl) ?? null : parse(window.localStorage.getItem(keyOf(baseUrl)))
  } catch {
    stored = memory.get(baseUrl) ?? null
  }
  if (stored === "bad" || (stored && stored.until <= now)) {
    clearSnooze(baseUrl)
    return null
  }
  return stored
}

export function writeSnooze(baseUrl: string, snooze: RailSnooze) {
  memory.set(baseUrl, snooze)
  try {
    window.localStorage.setItem(keyOf(baseUrl), JSON.stringify(snooze))
    unsaved.delete(baseUrl)
  } catch {
    // Kept for this page only.
    unsaved.add(baseUrl)
  }
}

export function clearSnooze(baseUrl: string) {
  memory.delete(baseUrl)
  unsaved.delete(baseUrl)
  try {
    window.localStorage.removeItem(keyOf(baseUrl))
  } catch {
    // Nothing kept.
  }
}

// holdsWork says whether an item stops work until the operator decides:
// an approval, a question, a plan or a gate. Only a new one of these ends
// a snooze early.
export function holdsWork(item: Pick<AttentionItem, "signal_kind">): boolean {
  const kind = needsYouKind(item)
  return kind === "approval" || kind === "question" || kind === "plan" || kind === "gate"
}

// snoozeOver says whether the sheet may open by itself: no snooze, its time
// has passed, or an item that holds work is not one the rail held at the
// close. A changed item keeps its ID, so it does not end the snooze.
export function snoozeOver(snooze: RailSnooze | null, candidates: SheetCandidate[], now: number): boolean {
  if (!snooze || snooze.until <= now) return true
  const seen = new Set(snooze.seen)
  return candidates.some((candidate) => candidate.holdsWork && !seen.has(candidate.id))
}

// resetSnoozeMemory is for tests: a fresh page.
export function resetSnoozeMemory() {
  memory.clear()
  unsaved.clear()
}
