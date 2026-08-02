import { useState, useRef, useEffect } from "react"
import { Outlet, NavLink, useLocation } from "react-router-dom"
import { Monitor, LayoutGrid, Settings, Server, ListChecks, ClipboardList, Workflow, Bell, MoreHorizontal, X, BookOpen } from "lucide-react"
import { useWebSocket } from "../hooks/useWebSocket"
import { useServer } from "../hooks/useServer"
import { PlanApprovalModal } from "./PlanApprovalModal"
import { OfflineDialog } from "./OfflineDialog"
import { cn } from "../lib/utils"
import { useReviewQueue } from "../hooks/useReview"
import { usePlanQueue } from "../hooks/usePlan"
import { useAttention } from "../hooks/useAttention"
import { AgentSelectionProvider } from "../hooks/useAgentSelection"
import { useWorkflowRuns } from "../hooks/useWorkflow"

export function Layout() {
  const { connected } = useWebSocket()
  const { servers, activeServerName, baseUrl, setActiveServer } = useServer()
  const { data: reviewQueue } = useReviewQueue()
  const reviewCount = reviewQueue?.length ?? 0
  const { data: planQueue } = usePlanQueue()
  const planCount = planQueue?.length ?? 0
  const { data: workflowRuns } = useWorkflowRuns()
  const workflowAttentionCount = (workflowRuns??[]).filter(run=>run.needs_attention).length
  const { data: attentionItems } = useAttention()
  const attentionCount = attentionItems?.length ?? 0
  const [switcherOpen, setSwitcherOpen] = useState(false)
  const [moreOpen, setMoreOpen] = useState(false)
  const switcherRef = useRef<HTMLDivElement>(null)
  const location = useLocation()

  useEffect(() => { setMoreOpen(false) }, [location.pathname])

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

  const primaryNav = [
    { to: "/attention", label: "Attention", icon: Bell, count: attentionCount, badge: "bg-amber-500" },
    { to: "/board", label: "Board", icon: LayoutGrid },
    { to: "/agents", label: "Agents", icon: Monitor },
    { to: "/review", label: "Review", icon: ListChecks, count: reviewCount, badge: "bg-amber-500" },
  ]

  const secondaryNav = [
    { to: "/plans", label: "Plans", icon: ClipboardList, count: planCount, badge: "bg-indigo-400" },
    { to: "/workflows", label: "Flows", icon: Workflow, count: workflowAttentionCount, badge: "bg-red-400" },
    { to: "/learning", label: "Learning", icon: BookOpen },
    { to: "/settings", label: "Settings", icon: Settings },
  ]

  const secondaryActive = secondaryNav.some(({ to }) => location.pathname.startsWith(to))

  return (
    <AgentSelectionProvider>
      <div className="flex h-dvh w-full overflow-hidden">
      {/* Sidebar */}
      <nav className="hidden w-14 flex-col items-center border-r border-zinc-800 bg-zinc-950 py-4 gap-4 md:flex">
        <NavLink
          to="/attention"
          className={({ isActive }) =>
            cn(
              "relative flex h-10 w-10 items-center justify-center rounded-lg transition-colors",
              isActive
                ? "bg-zinc-800 text-zinc-100"
                : "text-zinc-500 hover:bg-zinc-900 hover:text-zinc-300"
            )
          }
          title={attentionCount > 0 ? `Attention: ${attentionCount} items` : "Attention"}
        >
          <Bell size={20} />
          {attentionCount > 0 && (
            <span className="absolute -right-1 -top-1 min-w-4 rounded-full bg-amber-500 px-1 text-center text-[9px] font-bold leading-4 text-zinc-950">
              {attentionCount > 99 ? "99+" : attentionCount}
            </span>
          )}
        </NavLink>
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
        <NavLink
          to="/review"
          className={({ isActive }) =>
            cn(
              "relative flex h-10 w-10 items-center justify-center rounded-lg transition-colors",
              isActive
                ? "bg-zinc-800 text-zinc-100"
                : "text-zinc-500 hover:bg-zinc-900 hover:text-zinc-300"
            )
          }
          title={reviewCount > 0 ? `Review queue: ${reviewCount} tasks` : "Review queue"}
        >
          <ListChecks size={20} />
          {reviewCount > 0 && (
            <span className="absolute -right-1 -top-1 min-w-4 rounded-full bg-amber-500 px-1 text-center text-[9px] font-bold leading-4 text-zinc-950">
              {reviewCount > 99 ? "99+" : reviewCount}
            </span>
          )}
        </NavLink>
        <NavLink to="/plans" className={({isActive})=>cn("relative flex h-10 w-10 items-center justify-center rounded-lg transition-colors",isActive?"bg-zinc-800 text-zinc-100":"text-zinc-500 hover:bg-zinc-900 hover:text-zinc-300")} title={planCount>0?`Plan queue: ${planCount} plans`:"Plan queue"}>
          <ClipboardList size={20}/>{planCount>0&&<span className="absolute -right-1 -top-1 min-w-4 rounded-full bg-indigo-400 px-1 text-center text-[9px] font-bold leading-4 text-zinc-950">{planCount>99?"99+":planCount}</span>}
        </NavLink>
        <NavLink to="/workflows" className={({isActive})=>cn("flex h-10 w-10 items-center justify-center rounded-lg transition-colors",isActive?"bg-zinc-800 text-zinc-100":"text-zinc-500 hover:bg-zinc-900 hover:text-zinc-300")} title="Workflows"><Workflow size={20}/></NavLink>
        <NavLink to="/learning" className={({isActive})=>cn("flex h-10 w-10 items-center justify-center rounded-lg transition-colors",isActive?"bg-zinc-800 text-zinc-100":"text-zinc-500 hover:bg-zinc-900 hover:text-zinc-300")} title="Learning"><BookOpen size={20}/></NavLink>

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
      <main className="flex flex-1 flex-col overflow-hidden pb-[calc(3.5rem+env(safe-area-inset-bottom))] md:pb-0">
        <Outlet />
      </main>
      <nav
        aria-label="Primary navigation"
        className="fixed inset-x-0 bottom-0 z-40 grid grid-cols-5 border-t border-zinc-800 bg-zinc-950/95 pb-[env(safe-area-inset-bottom)] backdrop-blur md:hidden"
      >
        {primaryNav.map(({ to, label, icon: Icon, count, badge }, index) => (
          <NavLink
            key={to}
            to={to}
            className={({ isActive }) => cn(
              "relative flex min-h-14 flex-col items-center justify-center gap-0.5 text-[10px]",
              isActive ? "text-indigo-300" : "text-zinc-500",
            )}
          >
            <Icon size={20} />
            <span>{label}</span>
            {index === 0 && !connected && (
              <span aria-label="Disconnected" className="absolute right-[18%] top-1.5 h-2 w-2 rounded-full bg-red-500" />
            )}
            {!!count && <span className={cn("absolute right-[18%] top-1.5 min-w-4 rounded-full px-1 text-center text-[9px] font-bold leading-4 text-zinc-950", badge)}>{count > 99 ? "99+" : count}</span>}
          </NavLink>
        ))}
        <button
          onClick={() => setMoreOpen((v) => !v)}
          className={cn(
            "relative flex min-h-14 flex-col items-center justify-center gap-0.5 text-[10px]",
            moreOpen || secondaryActive ? "text-indigo-300" : "text-zinc-500",
          )}
        >
          <MoreHorizontal size={20} />
          <span>More</span>
          {secondaryNav.some(({ count }) => !!count) && (
            <span className="absolute right-[18%] top-1.5 min-w-4 rounded-full bg-zinc-600 px-1 text-center text-[9px] font-bold leading-4 text-zinc-200">
              {secondaryNav.reduce((sum, { count }) => sum + (count ?? 0), 0) > 99 ? "99+" : secondaryNav.reduce((sum, { count }) => sum + (count ?? 0), 0)}
            </span>
          )}
        </button>
      </nav>

      {/* More sheet */}
      {moreOpen && (
        <div className="fixed inset-0 z-50 md:hidden">
          <div className="absolute inset-0 bg-black/50" onClick={() => setMoreOpen(false)} />
          <div className="absolute inset-x-0 bottom-0 rounded-t-xl border-t border-zinc-700 bg-zinc-950 pb-[env(safe-area-inset-bottom)] shadow-2xl">
            <div className="flex items-center justify-between px-5 py-3 border-b border-zinc-800">
              <span className="text-sm font-medium text-zinc-300">More</span>
              <button onClick={() => setMoreOpen(false)} className="text-zinc-500 hover:text-zinc-300">
                <X size={18} />
              </button>
            </div>
            <div className="flex flex-col py-1">
              {secondaryNav.map(({ to, label, icon: Icon, count, badge }) => (
                <NavLink
                  key={to}
                  to={to}
                  className={({ isActive }) => cn(
                    "relative flex items-center gap-3 px-5 py-3 text-sm",
                    isActive ? "text-indigo-300 bg-zinc-900" : "text-zinc-400 hover:bg-zinc-900",
                  )}
                >
                  <Icon size={18} />
                  <span>{label}</span>
                  {!!count && <span className={cn("ml-auto rounded-full px-1.5 py-0.5 text-[10px] font-bold text-zinc-950", badge)}>{count > 99 ? "99+" : count}</span>}
                </NavLink>
              ))}
            </div>
          </div>
        </div>
      )}

      <OfflineDialog />
      <PlanApprovalModal />
      </div>
    </AgentSelectionProvider>
  )
}
