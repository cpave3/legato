import { socketUrl } from "./legato"
import type { SentPin } from "./scope-types"

// Sent pins, live: while a tab of a Scope site watches, the service worker
// keeps Legato's WebSocket (/ws) open and, on scope_changed for an origin,
// reads that origin's pins again and tells its tabs. The socket is handled as
// discussion-session.ts handles it: the token in the address, reconnecting
// after a drop with the same delays.

export type LiveSocket = {
  close: () => void
  onopen: (() => void) | null
  onmessage: ((event: { data: string }) => void) | null
  onclose: (() => void) | null
}

export type ScopeLiveDeps = {
  target: () => Promise<{ baseUrl: string; token: string }>
  connect: (url: string) => LiveSocket
  fetchPins: (origin: string) => Promise<SentPin[]>
  // chrome.tabs.sendMessage; a rejection means the tab has gone or left.
  notify: (tabId: number, message: { type: "scopePinsChanged"; origin: string; pins: SentPin[] }) => Promise<unknown>
  setTimeout: (callback: () => void, ms: number) => number
  clearTimeout: (id: number) => void
}

const reconnectDelays = [1_000, 2_000, 5_000, 10_000]

export function createScopeLive(deps: ScopeLiveDeps) {
  const watchers = new Map<number, string>()
  let socket: LiveSocket | undefined
  let connecting = false
  let attempts = 0
  let reconnectTimer: number | undefined
  let opened = false

  const origins = (): string[] => [...new Set(watchers.values())]

  const refresh = async (origin: string): Promise<void> => {
    const tabs = [...watchers].filter(([, watched]) => watched === origin).map(([tabId]) => tabId)
    if (tabs.length === 0) return
    const pins = await deps.fetchPins(origin)
    await Promise.all(tabs.map((tabId) => deps.notify(tabId, { type: "scopePinsChanged", origin, pins }).catch(() => {
      if (watchers.get(tabId) === origin) unwatch(tabId)
    })))
  }

  const refreshAll = (): void => {
    for (const origin of origins()) void refresh(origin).catch(() => undefined)
  }

  const connect = async (): Promise<void> => {
    if (socket || connecting || watchers.size === 0) return
    connecting = true
    let target: { baseUrl: string; token: string }
    try {
      target = await deps.target()
    } catch {
      connecting = false
      scheduleReconnect()
      return
    }
    connecting = false
    if (socket || watchers.size === 0) return
    const current = deps.connect(socketUrl(target.baseUrl, target.token))
    socket = current
    current.onopen = () => {
      if (socket !== current) return
      attempts = 0
      // After a drop, a change may have been missed.
      if (opened) refreshAll()
      opened = true
    }
    current.onmessage = (event) => {
      if (socket !== current) return
      let message: { type?: string; origin?: string }
      try { message = JSON.parse(event.data) as typeof message } catch { return }
      if (message.type === "scope_changed" && typeof message.origin === "string") void refresh(message.origin).catch(() => undefined)
    }
    current.onclose = () => {
      if (socket !== current) return
      socket = undefined
      scheduleReconnect()
    }
  }

  const scheduleReconnect = (): void => {
    if (watchers.size === 0 || reconnectTimer !== undefined) return
    const delay = reconnectDelays[Math.min(attempts++, reconnectDelays.length - 1)]
    reconnectTimer = deps.setTimeout(() => { reconnectTimer = undefined; void connect() }, delay)
  }

  const unwatch = (tabId: number): void => {
    watchers.delete(tabId)
    if (watchers.size > 0) return
    if (reconnectTimer !== undefined) deps.clearTimeout(reconnectTimer)
    reconnectTimer = undefined
    const current = socket
    socket = undefined
    opened = false
    attempts = 0
    current?.close()
  }

  return {
    watch: (tabId: number, origin: string): void => {
      watchers.set(tabId, origin)
      void connect()
    },
    unwatch,
    refresh,
    watching: (): number[] => [...watchers.keys()],
  }
}
