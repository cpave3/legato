import { useEffect, useRef, useState, useCallback, createContext, useContext } from "react"
import { getToken } from "../lib/auth"
import { clearFrontendUpdateAttempt, coordinateFrontendUpdate, updateRegisteredServiceWorkers } from "../lib/frontendUpdate"

export interface WSMessage {
  type: "agent_output" | "agent_list" | "agents_changed" | "prompt_state" | "error" | "chimera_event" | "chimera_sync_required"
    | "subscribe_agent" | "unsubscribe_agent" | "send_keys" | "resize" | "detect_prompt" | "refresh_pane"
    | "agent_message" | "agent_interrupt" | "agent_permission" | "agent_question"
    | "plan_proposed" | "plan_verdict" | "plan_changed" | "swarm_changed" | "cards_changed" | "review_changed" | "workflow_changed" | "attention_changed" | "approvals_changed"
    | "agent_ui_changed" | "agent_context" | "pairing_changed"
  agent_id?: string
  // How full a native agent's context window is (agent_context).
  context_used?: number
  context_window?: number
  cursor?: string
  queue?: boolean
  content?: string
  full?: boolean
  keys?: string
  cols?: number
  rows?: number
  agents?: AgentInfo[]
  prompt?: PromptState | null
  error?: string
  // Plan proposal / swarm change fields
  plan_path?: string
  reply_socket?: string
  parent_task_id?: string
  status?: string
  notes?: string
  subtask_id?: string
  new_status?: string
  mode?: string
  // Review change fields
  task_id?: string
  tour_id?: string
  step_id?: string
  kind?: string
  plan_id?: string
  revision_id?: string
  workflow_run_id?: string
  workflow_step_id?: string
  item_id?: string
  // agent_ui_changed: where the item is drawn and which revision it is at.
  placement?: "panel" | "inline"
  revision?: number
  producer?: string
  source_key?: string
  request_id?: string
  decision?: "allow" | "deny"
  feedback?: string
  // agent_permission: which of the request's options an allow takes.
  option?: string
  // agent_question: what the person answered, per question ID, or that they
  // closed the question unanswered.
  answers?: Record<string, string[]>
  dismissed?: boolean
}

export interface AgentInfo {
  id: number
  task_id: string
  execution_id?: string
  attribution_task_id?: string
  execution_kind?: string
  workflow_run_id?: string
  task_title: string
  tmux_session: string
  command: string
  agent_kind: string
  status: string
  activity: string
  role?: string
  parent_task_id?: string
  subtask_id?: string
  started_at: string
  ended_at?: string
  working_seconds: number
  waiting_seconds: number
  error_seconds: number
  state_timeline?: string[]
  notify_enabled?: boolean
  has_chimera_endpoint?: boolean
  // The agent runs in Legato's sandbox.
  sandboxed?: boolean
  // The sandbox is on and this agent opted out of it.
  unsandboxed?: boolean
  // The last known fill of a native agent's context window; changes arrive
  // as agent_context messages (useAgentContexts).
  context_used?: number
  context_window?: number
}

export interface PromptState {
  // menu: any other list of options the tool is showing in its terminal.
  type: "tool_approval" | "plan_approval" | "question" | "menu" | "free_text" | "working"
  context?: string
  actions?: { label: string; keys: string }[]
}

type MessageHandler = (msg: WSMessage) => void

interface WebSocketContextValue {
  connected: boolean
  connectionGeneration: number
  send: (msg: WSMessage) => void
  subscribe: (handler: MessageHandler) => () => void
  updateRequired: boolean
  retryUpdate: () => void
}

const WebSocketContext = createContext<WebSocketContextValue>(null as unknown as WebSocketContextValue)

export function useWebSocket() {
  return useContext(WebSocketContext)
}

export function useWebSocketProvider(wsUrl: string) {
  const [connected, setConnected] = useState(false)
  const [connectionGeneration, setConnectionGeneration] = useState(0)
  const [updateRequired, setUpdateRequired] = useState(false)
  const wsRef = useRef<WebSocket | null>(null)
  const handlersRef = useRef<Set<MessageHandler>>(new Set())
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const attemptRef = useRef(0)
  const wsUrlRef = useRef(wsUrl)
  wsUrlRef.current = wsUrl

  const connect = useCallback(() => {
    let url = wsUrlRef.current
    // Derive the matching HTTP URL for metadata and token lookup.
    const httpUrl = new URL(url)
    httpUrl.protocol = httpUrl.protocol === "wss:" ? "https:" : "http:"
    httpUrl.pathname = httpUrl.pathname.replace(/\/ws$/, "")
    httpUrl.search = ""
    const isOrigin = httpUrl.host === window.location.host
    const baseUrl = isOrigin ? "" : httpUrl.toString().replace(/\/$/, "")
    const token = getToken(isOrigin ? undefined : baseUrl)
    if (token) {
      url += `?token=${encodeURIComponent(token)}`
    }
    const ws = new WebSocket(url)

    ws.onopen = () => {
      setConnected(true)
      setConnectionGeneration((generation) => generation + 1)
      attemptRef.current = 0
      void coordinateFrontendUpdate(__LEGATO_FRONTEND_BUILD_ID__, {
        fetchBuildID: async () => {
          const response = await fetch(`${baseUrl}/frontend-meta`, { cache: "no-store" })
          if (!response.ok) return null
          const meta = await response.json() as { build_id?: string }
          return meta.build_id ?? null
        },
        storage: window.sessionStorage,
        updateServiceWorker: updateRegisteredServiceWorkers,
        reload: () => window.location.reload(),
        onUpdateRequired: () => setUpdateRequired(true),
      })
    }

    ws.onclose = () => {
      setConnected(false)
      wsRef.current = null
      const delay = Math.min(1000 * Math.pow(2, attemptRef.current), 30000)
      attemptRef.current++
      // eslint-disable-next-line react-hooks/immutability
      reconnectTimeoutRef.current = setTimeout(connect, delay)
    }

    ws.onmessage = (event) => {
      try {
        const msg: WSMessage = JSON.parse(event.data)
        handlersRef.current.forEach((handler) => handler(msg))
      } catch {
        // ignore malformed messages
      }
    }

    wsRef.current = ws
  }, [])

  // Connect on mount and reconnect when wsUrl changes.
  useEffect(() => {
    // Close existing connection if URL changed.
    if (reconnectTimeoutRef.current != null) {
      clearTimeout(reconnectTimeoutRef.current)
      reconnectTimeoutRef.current = null
    }
    if (wsRef.current) {
      wsRef.current.onclose = null // prevent auto-reconnect
      wsRef.current.close()
      wsRef.current = null
    }
    attemptRef.current = 0
    connect()
    return () => {
      if (reconnectTimeoutRef.current != null) clearTimeout(reconnectTimeoutRef.current)
      wsRef.current?.close()
    }
  }, [wsUrl, connect])

  const send = useCallback((msg: WSMessage) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify(msg))
    }
  }, [])

  const subscribe = useCallback((handler: MessageHandler) => {
    handlersRef.current.add(handler)
    return () => {
      handlersRef.current.delete(handler)
    }
  }, [])

  const retryUpdate = useCallback(() => {
    clearFrontendUpdateAttempt(window.sessionStorage)
    setUpdateRequired(false)
    window.location.reload()
  }, [])

  return { connected, connectionGeneration, send, subscribe, updateRequired, retryUpdate, Provider: WebSocketContext.Provider }
}
