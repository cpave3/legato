export const STREAM_DECK_MARKER = "legato_source"
export const STREAM_DECK_VALUE = "streamdeck"

export type ActiveTabSnapshot = {
  tabId: number
  windowId: number
  url: string
  activatedAt: number
}

type ReuseInput = {
  destination: string
  newTabId: number
  newWindowId: number
  eventAt: number
  configuredOrigin: string
  prior?: ActiveTabSnapshot
}

type ReuseDecision =
  | { kind: "reuse"; priorTabId: number; cleanUrl: string }
  | { kind: "keep"; cleanUrl?: string }

export function stripStreamDeckMarker(destination: string): { marked: boolean; url: string } {
  let url: URL
  try { url = new URL(destination) } catch { return { marked: false, url: destination } }
  const marked = url.searchParams.get(STREAM_DECK_MARKER) === STREAM_DECK_VALUE
  if (marked) url.searchParams.delete(STREAM_DECK_MARKER)
  return { marked, url: url.toString() }
}

function origin(value: string): string | null {
  try {
    const url = new URL(value)
    return url.protocol === "http:" || url.protocol === "https:" ? url.origin : null
  } catch {
    return null
  }
}

export function decideTabReuse(input: ReuseInput): ReuseDecision {
  const stripped = stripStreamDeckMarker(input.destination)
  if (!stripped.marked) return { kind: "keep" }
  const keep: ReuseDecision = { kind: "keep", cleanUrl: stripped.url }
  const prior = input.prior
  if (!prior || prior.tabId === input.newTabId || prior.windowId !== input.newWindowId || prior.activatedAt >= input.eventAt) return keep
  if (origin(prior.url) !== input.configuredOrigin || origin(stripped.url) !== input.configuredOrigin) return keep
  return { kind: "reuse", priorTabId: prior.tabId, cleanUrl: stripped.url }
}

type TabsEffects = {
  update: (tabId: number, properties: chrome.tabs.UpdateProperties) => Promise<unknown>
  remove: (tabId: number) => Promise<unknown>
}

export async function handleMarkedTab(input: ReuseInput, tabs: TabsEffects): Promise<void> {
  const decision = decideTabReuse(input)
  if (decision.kind === "keep") {
    if (decision.cleanUrl && decision.cleanUrl !== input.destination) {
      await tabs.update(input.newTabId, { url: decision.cleanUrl }).catch(() => undefined)
    }
    return
  }
  try {
    await tabs.update(decision.priorTabId, { url: decision.cleanUrl, active: true })
  } catch {
    await tabs.update(input.newTabId, { url: decision.cleanUrl }).catch(() => undefined)
    return
  }
  await tabs.remove(input.newTabId).catch(() => undefined)
}
