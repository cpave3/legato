import { useState, useEffect, useMemo, useRef } from "react"
import { Outlet, useLocation } from "react-router-dom"
import { useWebSocket } from "../hooks/useWebSocket"
import { useServer } from "../hooks/useServer"
import { PlanApprovalModal } from "./PlanApprovalModal"
import { OfflineDialog } from "./OfflineDialog"
import { ApprovalFocus } from "./ApprovalTray"
import { useApprovalLink } from "../hooks/useApprovalLink"
import { NeedsYouSheet } from "./needsYou/NeedsYouSheet"
import { useRailState } from "../lib/railState"
import { useMediaQuery } from "../hooks/useMediaQuery"
import { AutoRunNotice } from "./AutoRunNotice"
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
import { useFocusMode } from "../lib/focusMode"
import { MainWidthContext } from "../hooks/useMainWidth"
import { useElementWidth } from "../hooks/useElementWidth"

export function Layout() {
  const { connected: open, lost, updateRequired, retryUpdate } = useWebSocket()
  // A drop that is still reconnecting is not shown as offline: the page is
  // likely back within a second (OfflineDialog shows Reconnecting meanwhile).
  const connected = open || !lost
  const { servers, activeServerName, baseUrl, setActiveServer } = useServer()
  const { data: summary } = useNavigationSummary()
  // ?approval=<id> opens the rail, or the phone's tray, on that approval.
  useApprovalLink()
  // Focus mode waits for the next question with nothing pending, so it stays
  // mounted while it is on.
  const focusMode = useFocusMode()
  // The tint of the server in use, so it is clear which Legato this is.
  const settings = useSettings().data
  const tint = settings?.tint
  const sections = useMemo(() => navSections(summary), [summary])
  const [moreOpen, setMoreOpen] = useState(false)
  // On a phone the needs-you rail is a sheet the tab bar opens.
  const [needsYouOpen, setNeedsYouOpen] = useState(false)
  const location = useLocation()
  // Pages lay out by the room they have beside the rail.
  const mainRef = useRef<HTMLElement>(null)
  const mainWidth = useElementWidth(mainRef)

  useEffect(() => { setMoreOpen(false); setNeedsYouOpen(false) }, [location.pathname])
  // ?approval= or a notification opens the sheet on a phone, as the rail
  // opens on it from md up.
  const phone = useMediaQuery("(max-width: 767px)")
  const { wanted } = useRailState()
  useEffect(() => {
    if (wanted && phone) { setMoreOpen(false); setNeedsYouOpen(true) }
  }, [wanted, phone])

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
      <MobileTabBar sections={sections} connected={connected} moreOpen={moreOpen} onMore={() => { setNeedsYouOpen(false); setMoreOpen((open) => !open) }}
        needsYouOpen={needsYouOpen} onNeedsYou={() => { setMoreOpen(false); setNeedsYouOpen((open) => !open) }} />
      {needsYouOpen && <NeedsYouSheet onClose={() => setNeedsYouOpen(false)} />}
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
      {((summary?.approvals ?? 0) > 0 || focusMode) && <ApprovalFocus />}
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
