import { useState, useRef, useEffect } from "react"
import { Outlet, NavLink } from "react-router-dom"
import { Monitor, LayoutGrid, Settings, Server } from "lucide-react"
import { useWebSocket } from "../hooks/useWebSocket"
import { useServer } from "../hooks/useServer"
import { cn } from "../lib/utils"

export function Layout() {
  const { connected } = useWebSocket()
  const { servers, activeServerName, baseUrl, setActiveServer } = useServer()
  const [switcherOpen, setSwitcherOpen] = useState(false)
  const switcherRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!switcherOpen) return
    const onClick = (e: MouseEvent) => {
      if (switcherRef.current && !switcherRef.current.contains(e.target as Node)) {
        setSwitcherOpen(false)
      }
    }
    document.addEventListener("click", onClick, true)
    return () => document.removeEventListener("click", onClick, true)
  }, [switcherOpen])

  const hasRemoteServers = servers.length > 0

  return (
    <div className="flex h-dvh w-full overflow-hidden">
      {/* Sidebar */}
      <nav className="flex w-14 flex-col items-center border-r border-zinc-800 bg-zinc-950 py-4 gap-4">
        <NavLink
          to="/agents"
          className={({ isActive }) =>
            cn(
              "flex h-10 w-10 items-center justify-center rounded-lg transition-colors",
              isActive
                ? "bg-zinc-800 text-zinc-100"
                : "text-zinc-500 hover:bg-zinc-900 hover:text-zinc-300"
            )
          }
          title="Agents"
        >
          <Monitor size={20} />
        </NavLink>
        <NavLink
          to="/board"
          className={({ isActive }) =>
            cn(
              "flex h-10 w-10 items-center justify-center rounded-lg transition-colors",
              isActive
                ? "bg-zinc-800 text-zinc-100"
                : "text-zinc-500 hover:bg-zinc-900 hover:text-zinc-300"
            )
          }
          title="Board"
        >
          <LayoutGrid size={20} />
        </NavLink>

        {/* Settings + server switcher + connection status at bottom */}
        <div className="mt-auto flex flex-col items-center gap-4">
        <NavLink
          to="/settings"
          className={({ isActive }) =>
            cn(
              "flex h-10 w-10 items-center justify-center rounded-lg transition-colors",
              isActive
                ? "bg-zinc-800 text-zinc-100"
                : "text-zinc-500 hover:bg-zinc-900 hover:text-zinc-300"
            )
          }
          title="Settings"
        >
          <Settings size={20} />
        </NavLink>

        {/* Server switcher — only shown when remote servers are configured */}
        {hasRemoteServers && (
          <div className="relative" ref={switcherRef}>
            <button
              onClick={() => setSwitcherOpen((v) => !v)}
              className={cn(
                "flex h-10 w-10 items-center justify-center rounded-lg transition-colors",
                baseUrl
                  ? "text-indigo-400 hover:bg-zinc-900"
                  : "text-zinc-500 hover:bg-zinc-900 hover:text-zinc-300"
              )}
              title={`Server: ${activeServerName}`}
            >
              <Server size={18} />
            </button>
            {switcherOpen && (
              <div className="absolute bottom-0 left-full ml-2 rounded border border-zinc-700 bg-zinc-900 shadow-xl py-1 min-w-[180px] z-50">
                <button
                  onClick={() => { setActiveServer(""); setSwitcherOpen(false) }}
                  className={cn(
                    "flex w-full items-center gap-2 px-3 py-1.5 text-xs transition-colors",
                    !baseUrl ? "text-indigo-400 bg-zinc-800" : "text-zinc-300 hover:bg-zinc-800"
                  )}
                >
                  Local
                </button>
                {servers.map((s) => (
                  <button
                    key={s.url}
                    onClick={() => { setActiveServer(s.url); setSwitcherOpen(false) }}
                    className={cn(
                      "flex w-full items-center gap-2 px-3 py-1.5 text-xs transition-colors truncate",
                      baseUrl === s.url ? "text-indigo-400 bg-zinc-800" : "text-zinc-300 hover:bg-zinc-800"
                    )}
                  >
                    {s.name}
                  </button>
                ))}
              </div>
            )}
          </div>
        )}

        <div title={connected ? "Connected" : "Disconnected"}>
          <div
            className={cn(
              "h-2.5 w-2.5 rounded-full",
              connected ? "bg-emerald-500" : "bg-red-500"
            )}
          />
        </div>
        </div>
      </nav>

      {/* Main content */}
      <main className="flex flex-1 flex-col overflow-hidden">
        <Outlet />
      </main>
    </div>
  )
}
