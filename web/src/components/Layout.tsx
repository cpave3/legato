import { useState, useEffect, useMemo, useRef } from "react"
import { Outlet, useLocation } from "react-router-dom"
import { useWebSocket } from "../hooks/useWebSocket"
import { useServer } from "../hooks/useServer"
import { PlanApprovalModal } from "./PlanApprovalModal"
import { OfflineDialog } from "./OfflineDialog"
import { ApprovalTray } from "./ApprovalTray"
import { AutoRunNotice } from "./AutoRunNotice"
import { useFocusMode } from "../lib/focusMode"
import { ApprovalNotifier } from "./ApprovalNotifier"
import { notificationsSupported } from "../lib/approvalNotifications"
import { AgentSelectionProvider } from "../hooks/useAgentSelection"
import { useNavigationSummary } from "../hooks/useNavigationSummary"
import { navSections } from "../lib/navSections"
import { NavRail } from "./NavRail"
import { MobileTabBar, MoreSheet } from "./MobileNav"
import { TintBand } from "./TintBand"
import { useSettings } from "../hooks/useSettings"
import { UpdateNotice } from "./UpdateNotice"
import { DoctorBanner } from "./DoctorBanner"
import { SetupChip } from "./SetupChip"
import { VoiceLauncher } from "../voice/VoiceLauncher"
import { ReeveNameContext } from "../lib/reeveName"
import { NeedsYouRail } from "./needsYou/NeedsYouRail"
import { MainWidthContext } from "../hooks/useMainWidth"
import { useElementWidth } from "../hooks/useElementWidth"

export function Layout() {
  const { connected: open, lost, updateRequired, retryUpdate } = useWebSocket()
  // A drop that is still reconnecting is not shown as offline: the page is
  // likely back within a second (OfflineDialog shows Reconnecting meanwhile).
  const connected = open || !lost
  const { servers, activeServerName, baseUrl, setActiveServer } = useServer()
  const { data: summary } = useNavigationSummary()
  // Focus mode waits for the next question with nothing pending, so the tray
  // stays while it is on.
  const focusMode = useFocusMode()
  // The tint of the server in use, so it is clear which Legato this is.
  const settings = useSettings().data
  const tint = settings?.tint
  const sections = useMemo(() => navSections(summary), [summary])
  const [moreOpen, setMoreOpen] = useState(false)
  const location = useLocation()
  // Pages lay out by the room they have beside the rail.
  const mainRef = useRef<HTMLElement>(null)
  const mainWidth = useElementWidth(mainRef)

  useEffect(() => { setMoreOpen(false) }, [location.pathname])

  return (
    <ReeveNameContext.Provider value={settings?.reeve_name ?? ""}>
    <AgentSelectionProvider>
      <div className="flex h-dvh w-full overflow-hidden">
      <TintBand tint={tint} />
      <NavRail sections={sections} connected={connected} servers={servers} activeServerName={activeServerName} baseUrl={baseUrl} onSwitchServer={setActiveServer} tint={tint} />

      {/* Main content */}
      <main ref={mainRef} className="flex min-w-0 flex-1 flex-col overflow-hidden pb-[calc(3.5rem+env(safe-area-inset-bottom))] md:pb-0">
        {settings?.setup_incomplete && <DoctorBanner baseUrl={baseUrl} />}
        {settings?.setup?.show && (
          <div className="flex justify-end px-3 pt-2">
            <SetupChip baseUrl={baseUrl} summary={settings.setup} />
          </div>
        )}
        <MainWidthContext.Provider value={mainWidth}>
          <Outlet />
        </MainWidthContext.Provider>
      </main>
      <NeedsYouRail />
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
      {!updateRequired && <UpdateNotice status={settings?.updates} baseUrl={baseUrl} />}
      {((summary?.approvals ?? 0) > 0 || (summary?.peer_approvals ?? 0) > 0 || focusMode) && <ApprovalTray />}
      <AutoRunNotice />
      {notificationsSupported() && <ApprovalNotifier />}
      <OfflineDialog />
      <PlanApprovalModal />
      <VoiceLauncher baseUrl={baseUrl} />
      </div>
    </AgentSelectionProvider>
    </ReeveNameContext.Provider>
  )
}
