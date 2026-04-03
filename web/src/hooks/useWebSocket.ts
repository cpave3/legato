import { useEffect, useRef, useState, useCallback, createContext, useContext } from "react"
import { getToken } from "../lib/auth"

export interface WSMessage {
  type: string
  agent_id?: string
  content?: string
  full?: boolean
  keys?: string
  cols?: number
  rows?: number
  agents?: AgentInfo[]
  prompt?: PromptState | null
  error?: string
}

export interface AgentInfo {
  id: number
  task_id: string
  task_title: string
  tmux_session: string
  command: string
  status: string
  activity: string
  started_at: string
  ended_at?: string
  working_seconds: number
  waiting_seconds: number
}

export interface PromptState {
  type: "tool_approval" | "plan_approval" | "free_text" | "working"
  context?: string
  actions?: { label: string; keys: string }[]
}

type MessageHandler = (msg: WSMessage) => void

interface WebSocketContextValue {
  connected: boolean
  send: (msg: WSMessage) => void
  subscribe: (handler: MessageHandler) => () => void
}

const WebSocketContext = createContext<WebSocketContextValue>(null as unknown as WebSocketContextValue)

export function useWebSocket() {
  return useContext(WebSocketContext)
}

export function useWebSocketProvider(wsUrl: string) {
  const [connected, setConnected] = useState(false)
  const wsRef = useRef<WebSocket | null>(null)
  const handlersRef = useRef<Set<MessageHandler>>(new Set())
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const attemptRef = useRef(0)
  const wsUrlRef = useRef(wsUrl)
  wsUrlRef.current = wsUrl

  const connect = useCallback(() => {
    let url = wsUrlRef.current
    // Derive the base URL for token lookup — strip /ws and protocol.
    const baseUrl = url.replace(/^wss?:\/\//, "https://").replace(/\/ws$/, "")
    const isOrigin = new URL(url.replace(/^wss?:/, "https:")).host === window.location.host
    const token = getToken(isOrigin ? undefined : baseUrl)
    if (token) {
      url += `?token=${encodeURIComponent(token)}`
    }
    const ws = new WebSocket(url)

    ws.onopen = () => {
      setConnected(true)
      attemptRef.current = 0
    }

    ws.onclose = () => {
      setConnected(false)
      wsRef.current = null
      const delay = Math.min(1000 * Math.pow(2, attemptRef.current), 30000)
      attemptRef.current++
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

  return { connected, send, subscribe, Provider: WebSocketContext.Provider }
}
