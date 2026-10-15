import { Fragment, useEffect, useRef, useState } from "react"
import { Link, useLocation } from "react-router-dom"
import { Server } from "lucide-react"
import { cn } from "../lib/utils"
import { badgeText, badgeTone, railOrder, sectionActive, sectionName, type NavSection } from "../lib/navSections"

// NavIcon draws a section's icon from its path, as the design draws it.
export function NavIcon({ d, size = 18 }: { d: string; size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d={d} />
    </svg>
  )
}

function RailLink({ section }: { section: NavSection }) {
  const { pathname, search } = useLocation()
  const active = sectionActive(section, pathname, search)
  const name = sectionName(section)
  return (
    <Link
      to={section.to}
      aria-label={name}
      title={name}
      aria-current={active ? "page" : undefined}
      className={cn(
        "relative flex w-[54px] flex-col items-center gap-[3px] rounded-lg pb-[5px] pt-[7px] no-underline transition-colors",
        active ? "bg-lw-selected text-lw-text" : "text-[#8a8994] hover:bg-lw-raised hover:text-lw-text",
      )}
    >
      <NavIcon d={section.icon} />
      <span className="text-[10px] leading-3">{section.label}</span>
      {!!section.count && (
        <span aria-hidden="true" className={cn("absolute right-[5px] top-px h-4 min-w-4 rounded-lg px-1 text-center font-lw-mono text-[10px] font-medium leading-4", badgeTone[section.tone ?? "neutral"])}>
          {badgeText(section.count)}
        </span>
      )}
    </Link>
  )
}

interface NavRailProps {
  sections: Record<string, NavSection>
  connected: boolean
  servers: { name: string; url: string }[]
  activeServerName: string
  baseUrl: string
  onSwitchServer: (url: string) => void
}

// NavRail is the column of sections down the left of a wide screen: each an
// icon over its name, in groups, with what waits in it counted.
export function NavRail({ sections, connected, servers, activeServerName, baseUrl, onSwitchServer }: NavRailProps) {
  return (
    <nav aria-label="Primary" className="hidden w-16 shrink-0 flex-col items-center gap-0.5 border-r border-[#222228] bg-lw-rail pb-3.5 pt-3 font-lw md:flex">
      {railOrder.map((key) => (
        <Fragment key={key}>
          {sections[key].separated && <div role="separator" className="my-1.5 h-px w-7 bg-lw-line" />}
          <RailLink section={sections[key]} />
        </Fragment>
      ))}
      <div className="mt-auto flex flex-col items-center gap-2.5">
        <RailLink section={sections.settings} />
        {servers.length > 0 && <ServerSwitcher servers={servers} activeServerName={activeServerName} baseUrl={baseUrl} onSwitch={onSwitchServer} />}
        <div title={connected ? "Connected" : "Disconnected"} className={cn("flex items-center gap-1 text-[10px]", connected ? "text-lw-faint" : "text-lw-error")}>
          <span className={cn("size-[7px] rounded-full", connected ? "bg-lw-live" : "bg-lw-error")} />
          {connected ? "sync" : "offline"}
        </div>
      </div>
    </nav>
  )
}

// The servers this page can talk to, when there are more than this one.
function ServerSwitcher({ servers, activeServerName, baseUrl, onSwitch }: {
  servers: { name: string; url: string }[]
  activeServerName: string
  baseUrl: string
  onSwitch: (url: string) => void
}) {
  const [open, setOpen] = useState(false)
  const box = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const outside = (event: MouseEvent) => {
      if (box.current && !box.current.contains(event.target as Node)) setOpen(false)
    }
    document.addEventListener("click", outside, true)
    return () => document.removeEventListener("click", outside, true)
  }, [open])

  const choose = (url: string) => { onSwitch(url); setOpen(false) }
  const item = (label: string, url: string) => (
    <button key={url || "local"} type="button" onClick={() => choose(url)}
      className={cn("flex w-full items-center px-3 py-1.5 text-left text-xs", baseUrl === url ? "bg-lw-selected text-lw-link" : "text-lw-text hover:bg-lw-raised")}>
      <span className="truncate">{label}</span>
    </button>
  )

  return (
    <div ref={box} className="relative">
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        aria-label={`Server: ${activeServerName}`}
        title={`Server: ${activeServerName}`}
        className={cn("flex size-9 items-center justify-center rounded-lg hover:bg-lw-raised", baseUrl ? "text-lw-link" : "text-[#8a8994] hover:text-lw-text")}
      >
        <Server size={16} />
      </button>
      {open && (
        <div className="absolute bottom-0 left-full z-50 ml-2 min-w-[180px] rounded-md border border-lw-line-strong bg-lw-surface py-1 shadow-xl">
          {item("Local", "")}
          {servers.map((server) => item(server.name, server.url))}
        </div>
      )}
    </div>
  )
}
