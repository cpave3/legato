import { useState, useEffect, useMemo } from "react"
import { Outlet, useLocation } from "react-router-dom"
import { useWebSocket } from "../hooks/useWebSocket"
import { useServer } from "../hooks/useServer"
import { PlanApprovalModal } from "./PlanApprovalModal"
import { OfflineDialog } from "./OfflineDialog"
import { AgentSelectionProvider } from "../hooks/useAgentSelection"
import { useNavigationSummary } from "../hooks/useNavigationSummary"
import { navSections } from "../lib/navSections"
import { NavRail } from "./NavRail"
import { MobileTabBar, MoreSheet } from "./MobileNav"

export function Layout() {
  const { connected, updateRequired, retryUpdate } = useWebSocket()
  const { servers, activeServerName, baseUrl, setActiveServer } = useServer()
  const { data: summary } = useNavigationSummary()
  const sections = useMemo(() => navSections(summary), [summary])
  const [moreOpen, setMoreOpen] = useState(false)
  const location = useLocation()

  useEffect(() => { setMoreOpen(false) }, [location.pathname])

  return (
    <AgentSelectionProvider>
      <div className="flex h-dvh w-full overflow-hidden">
      <NavRail sections={sections} connected={connected} servers={servers} activeServerName={activeServerName} baseUrl={baseUrl} onSwitchServer={setActiveServer} />

      {/* Main content */}
      <main className="flex flex-1 flex-col overflow-hidden pb-[calc(3.5rem+env(safe-area-inset-bottom))] md:pb-0">
        <Outlet />
      </main>
      <MobileTabBar sections={sections} connected={connected} moreOpen={moreOpen} onMore={() => setMoreOpen((open) => !open)} />
      {moreOpen && (
        <MoreSheet sections={sections} connected={connected} servers={servers} activeServerName={activeServerName} baseUrl={baseUrl}
          onSwitchServer={setActiveServer} onClose={() => setMoreOpen(false)} />
      )}

      {updateRequired && (
        <div role="alert" className="fixed inset-x-4 top-4 z-[70] mx-auto flex max-w-xl items-center justify-between gap-4 rounded-lg border border-amber-500/50 bg-zinc-950 p-4 text-sm text-zinc-200 shadow-xl">
          <span>A newer Legato web build is available, but it could not be activated automatically.</span>
          <button className="shrink-0 rounded bg-amber-400 px-3 py-1.5 font-medium text-zinc-950" onClick={retryUpdate}>Reload update</button>
        </div>
      )}
      <OfflineDialog />
      <PlanApprovalModal />
      </div>
    </AgentSelectionProvider>
  )
}
