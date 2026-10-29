import { useEffect, useState, type ReactNode } from "react"
import { Link, useLocation } from "react-router-dom"
import { ChevronRight, X } from "lucide-react"
import { cn } from "../lib/utils"
import { badgeText, badgeTone, navIcons, sectionActive, sectionName, type NavSection } from "../lib/navSections"
import { animatedAgentActivityEnabled, setAnimatedAgentActivityEnabled } from "../lib/appearancePreferences"
import { NavIcon } from "./NavRail"

function Badge({ count, tone, className }: { count: number; tone: NavSection["tone"]; className?: string }) {
  return (
    <span aria-hidden="true" className={cn("h-4 min-w-4 rounded-lg px-1 text-center font-lw-mono text-[10px] leading-4", badgeTone[tone ?? "neutral"], className)}>
      {badgeText(count)}
    </span>
  )
}

const tabClass = (active: boolean) => cn(
  "relative flex min-h-12 flex-col items-center justify-center gap-[3px] text-[10px] font-medium leading-3 no-underline",
  active ? "text-lw-text" : "text-[#7d7c87]",
)

// MobileTabBar is the bar along the foot of a phone: the four sections used
// most, and More for the rest.
export function MobileTabBar({ sections, connected, moreOpen, onMore }: {
  sections: Record<string, NavSection>
  connected: boolean
  moreOpen: boolean
  onMore: () => void
}) {
  const { pathname, search } = useLocation()
  // On a phone the agent list and the ensemble are one tab, with the
  // ensemble's grid and its count.
  const agents: NavSection = { ...sections.agents, icon: navIcons.ensemble, count: sections.ensemble.count, tone: "live", noun: sections.ensemble.noun }
  const tabs = [sections.attention, sections.board, agents, sections.review]
  const rest = [sections.plans, sections.learning, sections.workflows]
  const restCount = rest.reduce((sum, section) => sum + (section.count ?? 0), 0)
  const restActive = moreOpen || [...rest, sections.reports, sections.audit, sections.settings].some((section) => sectionActive(section, pathname, search))

  return (
    <nav aria-label="Primary navigation" className="fixed inset-x-0 bottom-0 z-40 grid grid-cols-5 border-t border-[#222228] bg-lw-rail px-1 pb-[env(safe-area-inset-bottom)] pt-1.5 font-lw md:hidden">
      {tabs.map((section) => {
        const active = section.key === "agents" ? pathname.startsWith("/agents") : sectionActive(section, pathname, search)
        return (
          <Link key={section.key} to={section.to} aria-label={sectionName(section)} aria-current={active ? "page" : undefined} className={tabClass(active)}>
            <NavIcon d={section.icon} size={22} />
            <span>{section.label}</span>
            {section.key === "attention" && !connected && <span aria-label="Disconnected" className="absolute right-[22%] top-1 size-2 rounded-full bg-lw-error" />}
            {!!section.count && <Badge count={section.count} tone={section.tone} className="absolute left-1/2 top-0 ml-1.5" />}
          </Link>
        )
      })}
      <button type="button" onClick={onMore} aria-label={restCount ? `More: ${restCount} waiting` : "More"} aria-expanded={moreOpen} className={tabClass(restActive)}>
        <NavIcon d={navIcons.more} size={22} />
        <span>More</span>
        {restCount > 0 && <Badge count={restCount} tone="neutral" className="absolute left-1/2 top-0 ml-1.5" />}
      </button>
    </nav>
  )
}

function Group({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div role="group" aria-label={label} className="flex flex-col gap-1.5">
      <span aria-hidden="true" className="pl-1 text-xs font-semibold tracking-[1px] text-lw-text-2 uppercase">{label}</span>
      <div className="divide-y divide-[#222228] rounded-xl border border-lw-line bg-lw-surface">{children}</div>
    </div>
  )
}

const row = "flex min-h-[52px] items-center justify-between gap-3 px-3.5 text-[15px] text-lw-text no-underline"

function SectionRow({ section, label }: { section: NavSection; label?: string }) {
  return (
    <Link to={section.to} className={row}>
      <span>{label ?? section.label}</span>
      <span className="flex items-center gap-2">
        {!!section.count && <Badge count={section.count} tone={section.tone} />}
        <ChevronRight size={16} aria-hidden="true" className="text-lw-faint" />
      </span>
    </Link>
  )
}

function Toggle({ label, on, onChange }: { label: string; on: boolean; onChange: (on: boolean) => void }) {
  return (
    <div className={row}>
      <span>{label}</span>
      <button
        type="button"
        role="switch"
        aria-checked={on}
        aria-label={label}
        onClick={() => onChange(!on)}
        className={cn("flex h-[26px] w-11 shrink-0 items-center rounded-full p-[3px] transition-colors", on ? "justify-end bg-lw-accent" : "justify-start bg-lw-line-strong")}
      >
        <span className="size-5 rounded-full bg-white" />
      </button>
    </div>
  )
}

const PROMPT_DETECTION_KEY = "legato:prompt-detection"

// MoreSheet holds what the tab bar has no room for: the server in use, the
// other queues and tools, and the settings reached for most on a phone.
export function MoreSheet({ sections, connected, servers, activeServerName, baseUrl, onSwitchServer, onClose }: {
  sections: Record<string, NavSection>
  connected: boolean
  servers: { name: string; url: string }[]
  activeServerName: string
  baseUrl: string
  onSwitchServer: (url: string) => void
  onClose: () => void
}) {
  const [switching, setSwitching] = useState(false)
  const [promptDetection, setPromptDetection] = useState(() => localStorage.getItem(PROMPT_DETECTION_KEY) !== "false")
  const [animated, setAnimated] = useState(animatedAgentActivityEnabled)

  useEffect(() => {
    const escape = (event: KeyboardEvent) => { if (event.key === "Escape") onClose() }
    document.addEventListener("keydown", escape)
    return () => document.removeEventListener("keydown", escape)
  }, [onClose])

  const address = baseUrl ? baseUrl.replace(/^https?:\/\//, "") : window.location.host
  const server = (label: string, url: string) => (
    <button key={url || "local"} type="button" onClick={() => { onSwitchServer(url); setSwitching(false) }}
      className={cn(row, "w-full text-left", baseUrl === url && "text-lw-link")}>
      {label}
    </button>
  )

  return (
    // It opens above the tab bar, which stays in reach: More closes it again.
    // A sheet spans the screen, so it sets aside index.css's margin for
    // dialogs on a phone.
    <div className="fixed inset-x-0 top-0 bottom-[calc(3.5rem+env(safe-area-inset-bottom))] z-30 md:hidden">
      <div aria-hidden="true" className="absolute inset-0 bg-black/50" onClick={onClose} />
      <div role="dialog" aria-label="More" style={{ maxWidth: "none" }} className="absolute inset-x-0 bottom-0 flex max-h-[85%] flex-col rounded-t-2xl border-t border-lw-line-strong bg-lw-ground font-lw text-lw-text shadow-2xl">
        <div className="flex items-center justify-between px-4 pb-3 pt-4">
          <h2 className="m-0 text-2xl font-semibold leading-[30px]">More</h2>
          <button type="button" onClick={onClose} aria-label="Close" className="flex size-11 items-center justify-center rounded-full text-lw-text-2 hover:text-lw-text">
            <X size={20} />
          </button>
        </div>
        <div className="flex flex-col gap-[18px] overflow-y-auto px-4 pb-4">
          <div className="rounded-xl border border-lw-line bg-lw-surface">
            <div className="flex items-center gap-3 px-3.5 py-3">
              <span className={cn("size-2.5 shrink-0 rounded-full", connected ? "bg-lw-live" : "bg-lw-error")} />
              <div className="flex min-w-0 flex-1 flex-col">
                <span className="font-semibold">{activeServerName}</span>
                <span className="truncate font-lw-mono text-xs text-lw-text-2">{address} · {connected ? "synced" : "offline"}</span>
              </div>
              {servers.length > 0 && (
                <button type="button" onClick={() => setSwitching((value) => !value)} aria-label="Switch server" aria-expanded={switching} className="min-h-11 shrink-0 text-[13px] text-lw-text-2">
                  Switch ›
                </button>
              )}
            </div>
            {switching && (
              <div className="divide-y divide-[#222228] border-t border-[#222228]">
                {server("Local", "")}
                {servers.map((entry) => server(entry.name, entry.url))}
              </div>
            )}
          </div>
          <Group label="Queues">
            <SectionRow section={sections.plans} />
            <SectionRow section={sections.learning} />
          </Group>
          <Group label="Tools">
            <SectionRow section={sections.workflows} label="Workflows" />
            <SectionRow section={sections.reports} />
            <SectionRow section={sections.audit} label="Audit log" />
          </Group>
          <Group label="Settings">
            <Toggle label="Prompt detection" on={promptDetection} onChange={(on) => { setPromptDetection(on); localStorage.setItem(PROMPT_DETECTION_KEY, on ? "true" : "false") }} />
            <Toggle label="Animated agent activity" on={animated} onChange={(on) => { setAnimated(on); setAnimatedAgentActivityEnabled(on) }} />
            <Link to="/settings?pair=qr" className={row}><span>Pair with QR code</span><ChevronRight size={16} aria-hidden="true" className="text-lw-faint" /></Link>
            <Link to="/settings" className={row}><span>All settings</span><ChevronRight size={16} aria-hidden="true" className="text-lw-faint" /></Link>
          </Group>
        </div>
      </div>
    </div>
  )
}
