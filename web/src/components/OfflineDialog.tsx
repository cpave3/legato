import { useEffect, useRef, useState } from "react"
import { Link } from "react-router-dom"
import { ChevronRight, Plus, QrCode, Settings, WifiOff, X } from "lucide-react"
import { useWebSocket } from "../hooks/useWebSocket"
import { useServer } from "../hooks/useServer"

export function OfflineDialog({ delayMs = 2000 }: { delayMs?: number }) {
  const { connected } = useWebSocket()
  const { baseUrl, servers, activeServerName, isRemote, setActiveServer } = useServer()
  const [visible, setVisible] = useState(false)
  const dialogRef = useRef<HTMLElement>(null)
  const dismissRef = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    if (connected) {
      setVisible(false)
      return
    }

    const timer = setTimeout(() => setVisible(true), delayMs)
    return () => clearTimeout(timer)
  }, [connected, delayMs])

  useEffect(() => {
    if (!visible) return

    const previousFocus = document.activeElement as HTMLElement | null
    dismissRef.current?.focus()

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setVisible(false)
        return
      }
      if (event.key !== "Tab" || !dialogRef.current) return

      const focusable = Array.from(dialogRef.current.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), [tabindex]:not([tabindex="-1"])',
      ))
      const first = focusable[0]
      const last = focusable.at(-1)
      if (!first || !last) return

      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault()
        first.focus()
      }
    }

    document.addEventListener("keydown", onKeyDown)
    return () => {
      document.removeEventListener("keydown", onKeyDown)
      previousFocus?.focus()
    }
  }, [visible])

  if (!visible) return null

  const hasRemoteServers = servers.length > 0

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-zinc-950/55 p-4 backdrop-blur-[2px]">
      <section
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="offline-dialog-title"
        aria-describedby="offline-dialog-description"
        className="max-h-full w-full max-w-lg overflow-y-auto rounded-xl border border-amber-900/60 bg-zinc-950 shadow-2xl shadow-black/60"
      >
        <div className="flex items-start gap-3 border-b border-zinc-800 px-5 py-4">
          <div className="mt-0.5 rounded-full bg-amber-500/10 p-2">
            <WifiOff className="h-5 w-5 text-amber-500" />
          </div>
          <div className="min-w-0 flex-1">
            <h2 id="offline-dialog-title" className="font-semibold text-zinc-100">
              Connection Lost
            </h2>
            <p id="offline-dialog-description" className="mt-1 text-sm text-zinc-400">
              Unable to reach {isRemote ? activeServerName : "the Legato server"}. Reconnecting automatically&hellip;
            </p>
          </div>
          <button
            ref={dismissRef}
            type="button"
            onClick={() => setVisible(false)}
            aria-label="Dismiss connection warning"
            className="flex h-11 w-11 shrink-0 items-center justify-center rounded text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="space-y-4 px-5 py-4">
          {hasRemoteServers ? (
            <div>
              <p className="mb-2 text-xs font-medium uppercase tracking-wide text-zinc-500">Switch server</p>
              <div className="flex flex-wrap gap-2">
                <button
                  type="button"
                  onClick={() => setActiveServer("")}
                  aria-pressed={!baseUrl}
                  className={`flex min-h-11 items-center gap-1 rounded-md px-3 text-sm transition-colors ${
                    !baseUrl ? "bg-amber-500/20 text-amber-400" : "bg-zinc-900 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200"
                  }`}
                >
                  Local
                  {!baseUrl && <ChevronRight className="h-3.5 w-3.5" />}
                </button>
                {servers.map((server) => (
                  <button
                    type="button"
                    key={server.url}
                    onClick={() => setActiveServer(server.url)}
                    aria-pressed={baseUrl === server.url}
                    className={`flex min-h-11 items-center gap-1 rounded-md px-3 text-sm transition-colors ${
                      baseUrl === server.url
                        ? "bg-amber-500/20 text-amber-400"
                        : "bg-zinc-900 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200"
                    }`}
                  >
                    {server.name}
                    {baseUrl === server.url && <ChevronRight className="h-3.5 w-3.5" />}
                  </button>
                ))}
              </div>
            </div>
          ) : (
            <Link
              to="/settings"
              className="inline-flex min-h-11 items-center gap-2 rounded-md bg-zinc-900 px-3 text-sm text-zinc-300 transition-colors hover:bg-zinc-800"
            >
              <Settings className="h-4 w-4" />
              Go to Settings
            </Link>
          )}

          {isRemote && (
            <p className="text-xs leading-relaxed text-zinc-500">
              Check that the server is running and its TLS certificate is trusted on this device.
            </p>
          )}
        </div>

        <div className="flex flex-wrap items-center gap-2 border-t border-zinc-800 px-5 py-4">
          <Link to="/settings" className="inline-flex min-h-11 items-center gap-1.5 rounded-md px-3 text-sm text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200">
            <Plus className="h-4 w-4" /> Add server
          </Link>
          <Link to="/settings?pair=qr" className="inline-flex min-h-11 items-center gap-1.5 rounded-md px-3 text-sm text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200">
            <QrCode className="h-4 w-4" /> Scan QR
          </Link>
          <button
            type="button"
            onClick={() => window.location.reload()}
            className="ml-auto min-h-11 rounded-md bg-amber-500 px-4 text-sm font-medium text-zinc-950 transition-colors hover:bg-amber-400"
          >
            Retry
          </button>
        </div>
      </section>
    </div>
  )
}
