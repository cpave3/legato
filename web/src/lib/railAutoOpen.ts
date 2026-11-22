import { useSyncExternalStore } from "react"

// When the phone's needs-you sheet opens by itself, as this browser chose
// for each server (Settings › Needs-you rail › On a phone). It is a choice
// of the device, not of Legato: another phone keeps its own.
export type AutoOpen = {
  // Approvals, questions, plans and gates about the agent whose page is
  // open open the sheet too. Other items about that agent never do.
  agentInView: boolean
  // Items that do not hold work (a waiting agent, finished work) open it;
  // off, only approvals, questions, plans and gates do.
  everything: boolean
}

export const DEFAULT_AUTO_OPEN: AutoOpen = { agentInView: true, everything: true }

const keyOf = (baseUrl: string) => `legato:rail-auto-open:${baseUrl}`

// This page's choices, for a browser whose storage refuses them: for these
// servers memory is newer than storage.
const memory = new Map<string, AutoOpen>()
const listeners = new Set<() => void>()
// The last value read for each server, so useSyncExternalStore sees the
// same object while nothing changed.
const cache = new Map<string, { raw: string | null; value: AutoOpen }>()

function parse(raw: string | null): AutoOpen {
  if (raw === null) return DEFAULT_AUTO_OPEN
  try {
    const value = JSON.parse(raw) as Partial<Record<keyof AutoOpen, unknown>>
    return {
      agentInView: typeof value.agentInView === "boolean" ? value.agentInView : DEFAULT_AUTO_OPEN.agentInView,
      everything: typeof value.everything === "boolean" ? value.everything : DEFAULT_AUTO_OPEN.everything,
    }
  } catch {
    return DEFAULT_AUTO_OPEN
  }
}

// readAutoOpen is this browser's choice for a server. Storage that cannot
// be read gives this page's own choice, else the defaults.
export function readAutoOpen(baseUrl: string): AutoOpen {
  const kept = memory.get(baseUrl)
  if (kept) return kept
  let raw: string | null
  try {
    raw = window.localStorage.getItem(keyOf(baseUrl))
  } catch {
    return DEFAULT_AUTO_OPEN
  }
  const last = cache.get(baseUrl)
  if (last && last.raw === raw) return last.value
  const value = parse(raw)
  cache.set(baseUrl, { raw, value })
  return value
}

export function writeAutoOpen(baseUrl: string, value: AutoOpen) {
  try {
    window.localStorage.setItem(keyOf(baseUrl), JSON.stringify(value))
    memory.delete(baseUrl)
  } catch {
    // Kept for this page only.
    memory.set(baseUrl, value)
  }
  listeners.forEach((listener) => listener())
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  window.addEventListener("storage", listener)
  return () => {
    listeners.delete(listener)
    window.removeEventListener("storage", listener)
  }
}

// useAutoOpen is readAutoOpen kept current, also when another tab changes it.
export function useAutoOpen(baseUrl: string): AutoOpen {
  return useSyncExternalStore(subscribe, () => readAutoOpen(baseUrl))
}

// resetAutoOpenMemory is for tests: a fresh page.
export function resetAutoOpenMemory() {
  memory.clear()
  cache.clear()
}
