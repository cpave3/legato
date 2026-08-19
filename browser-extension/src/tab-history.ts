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
const creationKey = (tabId: number): string => `tabCreated:${tabId}`

export async function loadWindowHistory(windowId: number): Promise<WindowTabHistory> {
  const stored = await chrome.storage.session.get(key(windowId))
  return (stored[key(windowId)] as WindowTabHistory | undefined) ?? {}
}

export async function saveWindowHistory(windowId: number, history: WindowTabHistory): Promise<void> {
  await chrome.storage.session.set({ [key(windowId)]: history })
}

const activationWrites = new Map<number, Promise<void>>()

export function queueActivation(snapshot: ActiveTabSnapshot): Promise<void> {
  const priorWrite = activationWrites.get(snapshot.windowId) ?? Promise.resolve()
  const write = priorWrite.catch(() => undefined).then(async () => {
    const history = await loadWindowHistory(snapshot.windowId)
    await saveWindowHistory(snapshot.windowId, recordActivation(history, snapshot))
  })
  activationWrites.set(snapshot.windowId, write)
  void write.finally(() => {
    if (activationWrites.get(snapshot.windowId) === write) activationWrites.delete(snapshot.windowId)
  }).catch(() => undefined)
  return write
}

const creationWrites = new Map<number, Promise<void>>()

export function recordTabCreated(tabId: number, createdAt: number): Promise<void> {
  const write = chrome.storage.session.set({ [creationKey(tabId)]: createdAt })
  creationWrites.set(tabId, write)
  void write.finally(() => {
    if (creationWrites.get(tabId) === write) creationWrites.delete(tabId)
  }).catch(() => undefined)
  return write
}

export async function consumeTabCreated(tabId: number, fallback: number): Promise<number> {
  await creationWrites.get(tabId)?.catch(() => undefined)
  const stored = await chrome.storage.session.get(creationKey(tabId))
  await chrome.storage.session.remove(creationKey(tabId))
  return typeof stored[creationKey(tabId)] === "number" ? stored[creationKey(tabId)] as number : fallback
}
