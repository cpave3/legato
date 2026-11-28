import { useEffect, useRef, useState, useCallback, createContext, useContext } from "react"
import { getToken } from "../lib/auth"
import { clearFrontendUpdateAttempt, coordinateFrontendUpdate, updateRegisteredServiceWorkers } from "../lib/frontendUpdate"
import type { WSMessage } from "../lib/agentWire"

type MessageHandler = (msg: WSMessage) => void

// lostAfterMs is how long the page goes without a connection before it is
// lost. A phone back from the lock screen or another app usually reconnects
// well within it: the server is still there. Past it, the server is likely
// down, and the page says so.
export const lostAfterMs = 10_000

interface WebSocketContextValue {
  connected: boolean
  // lost: no connection for lostAfterMs. Until then a drop is "reconnecting".
  lost: boolean
  // reconnect tries at once, without reloading the page.
  reconnect: () => void
  connectionGeneration: number
  // send says whether the message left: false while there is no connection.
  send: (msg: WSMessage) => boolean
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
  const [lost, setLost] = useState(false)
  const lostTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
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
      if (lostTimerRef.current != null) {
        clearTimeout(lostTimerRef.current)
        lostTimerRef.current = null
      }
      setLost(false)
      setConnectionGeneration((generation) => generation + 1)
      attemptRef.current = 0
      // This page is the web build of the server that served it. Another
      // server's build says nothing about it, and a reload would only load
      // this one again.
      if (!isOrigin) return
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
      // The window starts at the first failure and runs until a connection.
      lostTimerRef.current ??= setTimeout(() => setLost(true), lostAfterMs)
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
      if (lostTimerRef.current != null) clearTimeout(lostTimerRef.current)
      lostTimerRef.current = null
      wsRef.current?.close()
    }
  }, [wsUrl, connect])

  // reconnectNow skips the backoff: the page has come back into view, the
  // network is back, or the mobile app has returned to the foreground. A
  // phone that slept or switched apps usually finds its socket closed while
  // the server is still there, so trying at once is what brings it back.
  const reconnectNow = useCallback(() => {
    const current = wsRef.current
    if (current && (current.readyState === WebSocket.OPEN || current.readyState === WebSocket.CONNECTING)) return
    if (reconnectTimeoutRef.current != null) {
      clearTimeout(reconnectTimeoutRef.current)
      reconnectTimeoutRef.current = null
    }
    attemptRef.current = 0
    connect()
  }, [connect])

  useEffect(() => {
    const onVisible = () => {
      if (document.visibilityState === "visible") reconnectNow()
    }
    document.addEventListener("visibilitychange", onVisible)
    window.addEventListener("online", reconnectNow)
    window.addEventListener("pageshow", reconnectNow)
    window.addEventListener("legato:shell-return", reconnectNow)
    return () => {
      document.removeEventListener("visibilitychange", onVisible)
      window.removeEventListener("online", reconnectNow)
      window.removeEventListener("pageshow", reconnectNow)
      window.removeEventListener("legato:shell-return", reconnectNow)
    }
  }, [reconnectNow])

  const send = useCallback((msg: WSMessage): boolean => {
    if (wsRef.current?.readyState !== WebSocket.OPEN) return false
    wsRef.current.send(JSON.stringify(msg))
    return true
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

  return { connected, lost, reconnect: reconnectNow, connectionGeneration, send, subscribe, updateRequired, retryUpdate, Provider: WebSocketContext.Provider }
}
