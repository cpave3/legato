// A conversation with one native agent, followed over Legato's WebSocket as
// the web UI's native panel follows it: the session's snapshot once, then its
// live events, reduced by the web UI's own reducer. Messages and answers go
// back over the same socket.
import {
  createNativeSyncState,
  reconcileNativeSnapshot,
  reduceNativeEvent,
  type NativeEvent,
  type NativeSnapshot,
  type NativeSyncState,
} from "../../web/src/lib/nativeAgentSync"
import { unreachableMessage } from "./legato"

export type DiscussionSocket = {
  send: (data: string) => void
  close: () => void
  onopen: (() => void) | null
  onmessage: ((event: { data: string }) => void) | null
  onclose: (() => void) | null
}

export type DiscussionState = NativeSyncState & {
  connected: boolean
  loading: boolean
}

type Dependencies = {
  fetch: typeof fetch
  connect: (url: string) => DiscussionSocket
  setTimeout: (callback: () => void, ms: number) => number
  clearTimeout: (id: number) => void
}

type Target = { baseUrl: string; token: string; taskId: string }

const reconnectDelays = [1_000, 2_000, 5_000, 10_000]

export const browserDependencies: Dependencies = {
  fetch: (...args) => fetch(...args),
  connect: (url) => new WebSocket(url) as unknown as DiscussionSocket,
  setTimeout: (callback, ms) => window.setTimeout(callback, ms),
  clearTimeout: (id) => window.clearTimeout(id),
}

export function socketUrl(baseUrl: string, token: string): string {
  const url = new URL("/ws", baseUrl)
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:"
  if (token) url.searchParams.set("token", token)
  return url.toString()
}

export function createDiscussionSession(target: Target, deps: Dependencies, onChange: (state: DiscussionState) => void) {
  let sync = createNativeSyncState()
  let connected = false
  let loading = true
  let socket: DiscussionSocket | undefined
  let stopped = false
  let attempts = 0
  let reconnectTimer: number | undefined
  let resyncTimer: number | undefined
  // Events that arrive while a snapshot is on its way are applied after it.
  let pending: NativeEvent[] | null = null
  let generation = 0
  let hydrated = false

  const publish = (): void => onChange({ ...sync, connected, loading })
  const send = (message: Record<string, unknown>): void => {
    if (connected) socket?.send(JSON.stringify(message))
  }

  const resync = async (initial = false): Promise<void> => {
    const mine = ++generation
    const queue: NativeEvent[] = pending ?? []
    pending = queue
    try {
      const headers: Record<string, string> = target.token ? { Authorization: `Bearer ${target.token}` } : {}
      const response = await deps.fetch(
        `${target.baseUrl}/api/agents/${encodeURIComponent(target.taskId)}/chimera/snapshot${initial ? "?initial=true" : ""}`,
        { headers },
      ).catch(() => { throw new Error(unreachableMessage(target.baseUrl)) })
      if (!response.ok) throw new Error((await response.text()).trim() || `Legato answered ${response.status}`)
      const snapshot = await response.json() as NativeSnapshot
      if (mine !== generation) return
      sync = reconcileNativeSnapshot(snapshot, sync, queue)
    } catch (error) {
      if (mine !== generation) return
      for (const event of queue) sync = reduceNativeEvent(sync, event)
      sync = { ...sync, error: error instanceof Error ? error.message : String(error) }
    } finally {
      if (mine === generation) {
        pending = null
        loading = false
        publish()
      }
    }
  }

  const handle = (raw: string): void => {
    let message: { type?: string; agent_id?: string; content?: string }
    try { message = JSON.parse(raw) as typeof message } catch { return }
    if (message.agent_id !== target.taskId) return
    if (message.type === "chimera_sync_required") {
      void resync()
      return
    }
    if (message.type !== "chimera_event") return
    let event: NativeEvent
    try { event = JSON.parse(message.content || "{}") as NativeEvent } catch { return }
    if (pending) pending.push(event)
    else sync = reduceNativeEvent(sync, event)
    // A finished run is read again whole, as the web UI does.
    if (event.type === "run_finished") {
      if (resyncTimer !== undefined) deps.clearTimeout(resyncTimer)
      resyncTimer = deps.setTimeout(() => { resyncTimer = undefined; void resync() }, 100)
    }
    publish()
  }

  const connect = (): void => {
    const current = deps.connect(socketUrl(target.baseUrl, target.token))
    socket = current
    current.onopen = () => {
      if (socket !== current) return
      connected = true
      attempts = 0
      send({ type: "subscribe_agent", agent_id: target.taskId, cursor: sync.cursor })
      if (!hydrated) {
        hydrated = true
        void resync(true)
      }
      publish()
    }
    current.onmessage = (event) => { if (socket === current) handle(event.data) }
    current.onclose = () => {
      if (socket !== current) return
      connected = false
      socket = undefined
      publish()
      if (stopped) return
      const delay = reconnectDelays[Math.min(attempts++, reconnectDelays.length - 1)]
      reconnectTimer = deps.setTimeout(() => { reconnectTimer = undefined; if (!stopped) connect() }, delay)
    }
  }

  return {
    start: (): void => {
      publish()
      connect()
    },
    stop: (): void => {
      stopped = true
      if (reconnectTimer !== undefined) deps.clearTimeout(reconnectTimer)
      if (resyncTimer !== undefined) deps.clearTimeout(resyncTimer)
      send({ type: "unsubscribe_agent", agent_id: target.taskId })
      socket?.close()
    },
    send: (content: string): void => {
      send({ type: "agent_message", agent_id: target.taskId, content, queue: sync.busy })
    },
    resolvePermission: (requestId: string, decision: "allow" | "deny"): void => {
      send({ type: "agent_permission", agent_id: target.taskId, request_id: requestId, decision })
    },
    refresh: (): Promise<void> => resync(),
  }
}
