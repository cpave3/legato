import { useEffect, useRef, useState, type ReactNode } from "react"
import { Mic, MoreHorizontal } from "lucide-react"
import { cn } from "../lib/utils"
import { AgentKindBadge } from "./AgentKindBadge"

export type AgentChatState = "needs-you" | "running" | "background" | "error" | "idle"

export interface AgentMenuEntry {
  label: string
  icon?: ReactNode
  onSelect: () => unknown
  disabled?: boolean
  // Ends the agent, or something like it: listed last, apart, in red.
  danger?: boolean
}

interface AgentChatHeaderProps {
  state: AgentChatState
  // How long the agent has been in that state.
  time?: string
  title: string
  taskId: string
  agentKind?: string
  native?: boolean
  // Small things worth saying beside the title: a mode, a sync, an error.
  notes?: ReactNode
  // The pull request of the agent's task, when it has one.
  pullRequest?: number
  panels?: SidePanelsProps
  // Brings what the agent waits on into view.
  onShowRequest?: () => void
  menu: AgentMenuEntry[]
}

const chips: Record<AgentChatState, { glyph: string; label: string; tone: string }> = {
  "needs-you": { glyph: "◆", label: "Needs you", tone: "bg-lw-needs-soft text-lw-needs" },
  running: { glyph: "●", label: "Running", tone: "bg-lw-live-soft text-lw-live" },
  background: { glyph: "◌", label: "Background", tone: "bg-lw-done-soft text-lw-done" },
  error: { glyph: "✕", label: "Error", tone: "bg-lw-error-soft text-lw-error" },
  idle: { glyph: "○", label: "Idle", tone: "bg-lw-idle-soft text-lw-text-2" },
}

// What can be opened in the column beside an agent's chat.
export type SidePanel = "related" | "artifacts"

export interface SidePanelsProps {
  // The panel open now, if any.
  open: SidePanel | null
  // live: something related is still in progress, such as a review being captured.
  related: { count: number; live: boolean }
  // fresh: artifacts the person has not looked at yet.
  artifacts: { count: number; fresh: number }
  // Opens a panel, or closes it when it is the one open.
  onSelect: (panel: SidePanel) => void
  // A studio the agent opened for the person to record in.
  studio?: { onOpen: () => void }
}

// The way into the studio, where the person records. A studio is a tool, not
// something the agent made to be read, so it is offered by itself and is not
// counted with the artifacts.
function StudioButton({ onOpen }: { onOpen: () => void }) {
  return (
    <button
      type="button"
      onClick={onOpen}
      aria-label="Studio"
      title="The studio, where you record"
      className="flex h-[30px] shrink-0 items-center gap-1.5 rounded-md border border-lw-line-strong bg-lw-raised px-3 text-xs text-lw-text transition-colors hover:border-[#4a4955]"
    >
      <Mic size={12} />
      <span className="hidden sm:inline">Studio</span>
    </button>
  )
}

const segment = "flex h-[26px] items-center gap-1.5 rounded px-2.5 text-xs transition-colors disabled:cursor-default disabled:opacity-50"

// SidePanelSwitch chooses what the column beside the chat shows: the work
// related to the agent's task, or what the agent has shown.
export function SidePanelSwitch({ open, related, artifacts, onSelect, studio }: SidePanelsProps) {
  const look = (panel: SidePanel) => cn(segment, open === panel ? "bg-lw-selected text-lw-text" : "text-lw-text-2 enabled:hover:text-lw-text")
  return (
    <>
      {studio && <StudioButton onOpen={studio.onOpen} />}
      <div role="group" aria-label="Side panel" className="flex shrink-0 rounded-md border border-lw-line bg-lw-surface p-0.5">
        <button
          type="button"
          aria-pressed={open === "related"}
          aria-label={`Related work (${related.count})${related.live ? ", in progress" : ""}`}
          title="Plans, reviews and pull requests of this agent's task"
          onClick={() => onSelect("related")}
          className={look("related")}
        >
          Related <span className="font-lw-mono text-lw-text-2">{related.count}</span>
          {related.live && <span aria-hidden="true" className="size-1.5 rounded-full bg-lw-live" />}
        </button>
        <button
          type="button"
          aria-pressed={open === "artifacts"}
          aria-label={`Artifacts (${artifacts.count})${artifacts.fresh > 0 ? `, ${artifacts.fresh} new` : ""}`}
          title={artifacts.count > 0 ? "What this agent has shown" : "This agent has shown nothing yet"}
          disabled={artifacts.count === 0}
          onClick={() => onSelect("artifacts")}
          className={look("artifacts")}
        >
          Artifacts <span className="font-lw-mono">{artifacts.count}</span>
          {artifacts.fresh > 0 && <span aria-hidden="true" className="size-1.5 rounded-full bg-lw-link" />}
        </button>
      </div>
    </>
  )
}

// The line an agent's chat opens with, whether Legato drives the agent
// natively or through its terminal: what it is doing, the task, and what can
// be done with it.
export function AgentChatHeader({ state, time, title, taskId, agentKind, native = false, notes, pullRequest, panels, onShowRequest, menu }: AgentChatHeaderProps) {
  const chip = chips[state]
  // On a phone the agent picker above says how long; the time is left out.
  const chipText = <><span aria-hidden="true">{chip.glyph} </span>{chip.label}{time && <span className="max-sm:hidden"> {time}</span>}</>
  const chipClass = cn("shrink-0 rounded px-2 font-lw-mono text-[11px] uppercase", chip.tone)

  return (
    <header className="flex shrink-0 items-center justify-between gap-2 border-b border-[#222228] bg-lw-ground px-3 py-2 font-lw text-[13px] text-lw-text sm:gap-3 sm:px-4 sm:py-2.5 md:px-6">
      <div className="flex min-w-0 items-center gap-3">
        {state === "needs-you" && onShowRequest
          ? <button type="button" onClick={onShowRequest} aria-label="Needs you" className={cn(chipClass, "animate-pulse hover:animate-none")}>{chipText}</button>
          // The picker says what the agent is doing on a phone, and the
          // switch needs the room; a request to bring into view stays.
          : <span className={cn(chipClass, "max-sm:hidden")}>{chipText}</span>}
        {/* On a phone the agent picker above names the agent, and the switch needs the room. */}
        <h2 className="m-0 min-w-0 truncate text-[15px] font-semibold max-sm:sr-only">{title}</h2>
        <span className="hidden shrink-0 font-lw-mono text-xs text-lw-faint sm:inline">{taskId}{pullRequest ? ` · PR #${pullRequest}` : ""}</span>
        {agentKind !== undefined && <AgentKindBadge kind={agentKind} isNative={native} className="hidden md:inline-flex" />}
        {notes && <span className="flex min-w-0 items-center gap-2 truncate text-xs text-lw-text-2">{notes}</span>}
      </div>
      <div className="flex shrink-0 items-center gap-2">
        {panels && <SidePanelSwitch {...panels} />}
        <AgentMoreMenu entries={menu} />
      </div>
    </header>
  )
}

// The ⋯ button and the menu of an agent's other actions.
export function AgentMoreMenu({ entries }: { entries: AgentMenuEntry[] }) {
  const [open, setOpen] = useState(false)
  const box = useRef<HTMLDivElement>(null)
  const plain = entries.filter((entry) => !entry.danger)
  const danger = entries.filter((entry) => entry.danger)

  useEffect(() => {
    if (!open) return
    const close = (event: MouseEvent) => {
      if (box.current && !box.current.contains(event.target as Node)) setOpen(false)
    }
    const escape = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false)
    }
    document.addEventListener("mousedown", close)
    document.addEventListener("keydown", escape)
    return () => {
      document.removeEventListener("mousedown", close)
      document.removeEventListener("keydown", escape)
    }
  }, [open])

  if (entries.length === 0) return null

  const item = (entry: AgentMenuEntry) => (
    <button
      key={entry.label}
      type="button"
      role="menuitem"
      disabled={entry.disabled}
      onClick={() => { setOpen(false); entry.onSelect() }}
      className={cn(
        "flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs transition-colors disabled:opacity-50",
        entry.danger ? "text-lw-error hover:bg-lw-error-soft" : "text-lw-text hover:bg-lw-raised",
      )}
    >
      {entry.icon}
      {entry.label}
    </button>
  )

  return (
    <div ref={box} className="relative">
      <button
        type="button"
        aria-label="More actions"
        title="More actions"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
        className={cn("flex h-[30px] w-[30px] items-center justify-center rounded-md border border-lw-line-strong text-lw-text hover:border-[#4a4955]", open ? "bg-lw-selected" : "bg-lw-raised")}
      >
        <MoreHorizontal size={14} />
      </button>
      {open && (
        <div role="menu" className="absolute right-0 top-full z-30 mt-1 max-h-[60vh] min-w-[200px] max-w-[calc(100vw-2rem)] overflow-y-auto rounded-md border border-lw-line-strong bg-lw-surface py-1 shadow-xl">
          {plain.map(item)}
          {danger.length > 0 && plain.length > 0 && <div className="my-1 border-t border-lw-line" />}
          {danger.map(item)}
        </div>
      )}
    </div>
  )
}
