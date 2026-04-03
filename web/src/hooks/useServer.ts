import { createContext, useContext, useState, useCallback, useMemo } from "react"

export interface ServerEntry {
  name: string
  url: string
}

export interface ServerContextValue {
  /** Base URL for REST API calls. Empty string = origin. */
  baseUrl: string
  /** Full WebSocket URL (wss://... or ws://...). */
  wsUrl: string
  /** Whether the active server is a remote (non-origin) server. */
  isRemote: boolean
  /** Display name of the active server. */
  activeServerName: string
  /** All configured remote servers. */
  servers: ServerEntry[]
  /** Set the active server. Empty string = origin. */
  setActiveServer: (url: string) => void
  /** Add a remote server. */
  addServer: (name: string, url: string) => void
  /** Remove a remote server by URL. Reverts to origin if active. */
  removeServer: (url: string) => void
}

const ServerContext = createContext<ServerContextValue>(null as unknown as ServerContextValue)

export function useServer() {
  return useContext(ServerContext)
}

const SERVERS_KEY = "legato:servers"
const ACTIVE_KEY = "legato:active-server"

function readServers(): ServerEntry[] {
  try {
    const raw = localStorage.getItem(SERVERS_KEY)
    if (raw) return JSON.parse(raw)
  } catch { /* ignore corrupt data */ }
  return []
}

function writeServers(servers: ServerEntry[]) {
  localStorage.setItem(SERVERS_KEY, JSON.stringify(servers))
}

function readActiveUrl(): string {
  return localStorage.getItem(ACTIVE_KEY) ?? ""
}

function deriveWsUrl(baseUrl: string): string {
  if (!baseUrl) {
    // Origin — use current page location.
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:"
    return `${protocol}//${window.location.host}/ws`
  }
  try {
    const parsed = new URL(baseUrl)
    const wsProtocol = parsed.protocol === "https:" ? "wss:" : "ws:"
    return `${wsProtocol}//${parsed.host}/ws`
  } catch {
    return `ws://${baseUrl}/ws`
  }
}

export function useServerProvider() {
  const [activeUrl, setActiveUrlState] = useState(readActiveUrl)
  const [servers, setServersState] = useState(readServers)

  const setActiveServer = useCallback((url: string) => {
    localStorage.setItem(ACTIVE_KEY, url)
    setActiveUrlState(url)
  }, [])

  const addServer = useCallback((name: string, url: string) => {
    setServersState((prev) => {
      // Avoid duplicates by URL.
      if (prev.some((s) => s.url === url)) return prev
      const next = [...prev, { name, url }]
      writeServers(next)
      return next
    })
  }, [])

  const removeServer = useCallback((url: string) => {
    setServersState((prev) => {
      const next = prev.filter((s) => s.url !== url)
      writeServers(next)
      return next
    })
    // If removing the active server, revert to origin.
    setActiveUrlState((current) => {
      if (current === url) {
        localStorage.setItem(ACTIVE_KEY, "")
        return ""
      }
      return current
    })
  }, [])

  const activeServerName = useMemo(() => {
    if (!activeUrl) return "Local"
    const entry = servers.find((s) => s.url === activeUrl)
    if (entry) return entry.name
    try { return new URL(activeUrl).hostname } catch { return activeUrl }
  }, [activeUrl, servers])

  const baseUrl = activeUrl
  const wsUrl = useMemo(() => deriveWsUrl(activeUrl), [activeUrl])
  const isRemote = activeUrl !== ""

  return {
    baseUrl,
    wsUrl,
    isRemote,
    activeServerName,
    servers,
    setActiveServer,
    addServer,
    removeServer,
    Provider: ServerContext.Provider,
  }
}
