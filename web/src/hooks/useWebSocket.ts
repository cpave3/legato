import { useEffect, useRef, useState, useCallback, createContext, useContext } from "react"

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

export function useWebSocketProvider() {
  const [connected, setConnected] = useState(false)
  const wsRef = useRef<WebSocket | null>(null)
  const handlersRef = useRef<Set<MessageHandler>>(new Set())
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const attemptRef = useRef(0)

  const connect = useCallback(() => {
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:"
    const wsUrl = `${protocol}//${window.location.host}/ws`
    const ws = new WebSocket(wsUrl)

    ws.onopen = () => {
      setConnected(true)
      attemptRef.current = 0
    }

    ws.onclose = () => {
      setConnected(false)
      wsRef.current = null
      // Exponential backoff: 1s, 2s, 4s, 8s, 16s, max 30s
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

  useEffect(() => {
    connect()
    return () => {
      if (reconnectTimeoutRef.current != null) clearTimeout(reconnectTimeoutRef.current)
      wsRef.current?.close()
    }
  }, [connect])

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
