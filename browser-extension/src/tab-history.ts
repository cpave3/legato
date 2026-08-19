import type { ActiveTabSnapshot } from "./tab-reuse"

export type WindowTabHistory = {
  current?: ActiveTabSnapshot
  previous?: ActiveTabSnapshot
}

export function recordActivation(history: WindowTabHistory, snapshot: ActiveTabSnapshot): WindowTabHistory {
  if (history.current?.tabId === snapshot.tabId) return { ...history, current: snapshot }
  return { current: snapshot, previous: history.current }
}

export function predecessorFor(history: WindowTabHistory, newTabId: number): ActiveTabSnapshot | undefined {
  if (history.current?.tabId !== newTabId) return history.current
  if (history.previous?.tabId !== newTabId) return history.previous
  return undefined
}

const key = (windowId: number): string => `tabHistory:${windowId}`

export async function loadWindowHistory(windowId: number): Promise<WindowTabHistory> {
  const stored = await chrome.storage.session.get(key(windowId))
  return (stored[key(windowId)] as WindowTabHistory | undefined) ?? {}
}

export async function saveWindowHistory(windowId: number, history: WindowTabHistory): Promise<void> {
  await chrome.storage.session.set({ [key(windowId)]: history })
}
