import { Outlet, NavLink } from "react-router-dom"
import { Monitor, LayoutGrid, Settings } from "lucide-react"
import { useWebSocket } from "../hooks/useWebSocket"
import { cn } from "../lib/utils"

export function Layout() {
  const { connected } = useWebSocket()

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

        {/* Settings + connection status at bottom */}
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
