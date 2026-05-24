import { useState, useEffect } from "react"
import { WifiOff, ChevronRight, Settings } from "lucide-react"
import { useWebSocket } from "../hooks/useWebSocket"
import { useServer } from "../hooks/useServer"

export function OfflineOverlay({ delayMs = 2000 }: { delayMs?: number }) {
  const { connected } = useWebSocket()
  const { baseUrl, servers, activeServerName, isRemote, setActiveServer } = useServer()
  const [visible, setVisible] = useState(false)

  // Delay showing the banner to avoid a flash on slow initial connections.
  useEffect(() => {
    if (connected) {
      setVisible(false)
      return
    }
    const timer = setTimeout(() => setVisible(true), delayMs)
    return () => clearTimeout(timer)
  }, [connected, delayMs])

  if (!visible) return null

  const hasRemoteServers = servers.length > 0

  return (
    <div
      role="alert"
      aria-live="polite"
      className="sticky top-0 z-50 border-b border-amber-900/50 bg-zinc-950/95 backdrop-blur-sm px-4 py-3"
    >
      <div className="flex flex-col gap-3 md:flex-row md:items-center">
        <div className="flex items-center gap-3 shrink-0">
          <WifiOff className="h-5 w-5 text-amber-500" />
          <div>
            <p className="text-sm font-medium text-zinc-200">
              Connection Lost
            </p>
            <p className="text-xs text-zinc-500">
              Unable to reach {isRemote ? activeServerName : "the Legato server"}. Reconnecting automatically&hellip;
            </p>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2 md:ml-auto">
          {/* Server switcher */}
          {hasRemoteServers && (
            <>
              <span className="hidden sm:inline text-xs text-zinc-500">Switch to:</span>
              <button
                onClick={() => setActiveServer("")}
                className={`flex items-center gap-1 rounded px-2 py-1 text-xs transition-colors ${
                  !baseUrl ? "bg-amber-500/20 text-amber-400" : "text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200"
                }`}
              >
                Local
                {!baseUrl && <ChevronRight className="h-3 w-3" />}
              </button>
              {servers.map((s) => (
                <button
                  key={s.url}
                  onClick={() => setActiveServer(s.url)}
                  className={`flex items-center gap-1 rounded px-2 py-1 text-xs transition-colors ${
                    baseUrl === s.url
                      ? "bg-amber-500/20 text-amber-400"
                      : "text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200"
                  }`}
                >
                  {s.name}
                  {baseUrl === s.url && <ChevronRight className="h-3 w-3" />}
                </button>
              ))}
            </>
          )}

          {!hasRemoteServers && (
            <a
              href="#/settings"
              onClick={(e) => {
                e.preventDefault()
                window.location.hash = "/settings"
              }}
              className="flex items-center gap-1.5 rounded px-2 py-1 text-xs text-zinc-400 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
            >
              <Settings className="h-3 w-3" />
              Go to Settings
            </a>
          )}

          <button
            onClick={() => window.location.reload()}
            className="shrink-0 rounded px-3 py-1 text-xs text-zinc-400 border border-zinc-700 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
          >
            Retry
          </button>
        </div>
      </div>

      {isRemote && (
        <p className="mt-1.5 text-[11px] text-zinc-600">
          Check that the server is running and its TLS certificate is trusted on this device.
        </p>
      )}
    </div>
  )
}
