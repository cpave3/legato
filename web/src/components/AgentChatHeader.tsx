import { useEffect, useRef, useState, type ReactNode } from "react"
import { MoreHorizontal, SquareKanban } from "lucide-react"
import { cn } from "../lib/utils"
import { AgentKindBadge } from "./AgentKindBadge"
import { ArtifactsButton, type ArtifactsButtonProps } from "./ArtifactsButton"

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
  artifacts?: ArtifactsButtonProps
  onOpenCard?: () => void
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

const toolbarButton = "flex h-[30px] shrink-0 items-center gap-1.5 rounded-md border border-lw-line-strong bg-lw-raised px-3 text-xs text-lw-text hover:border-[#4a4955]"

// The line an agent's chat opens with, whether Legato drives the agent
// natively or through its terminal: what it is doing, the task, and what can
// be done with it.
export function AgentChatHeader({ state, time, title, taskId, agentKind, native = false, notes, artifacts, onOpenCard, onShowRequest, menu }: AgentChatHeaderProps) {
  const chip = chips[state]
  const chipText = <><span aria-hidden="true">{chip.glyph} </span>{chip.label}{time && ` ${time}`}</>
  const chipClass = cn("shrink-0 rounded px-2 font-lw-mono text-[11px] uppercase", chip.tone)

  return (
    <header className="flex shrink-0 items-center justify-between gap-3 border-b border-[#222228] bg-lw-ground px-4 py-2.5 font-lw text-[13px] text-lw-text md:px-6">
      <div className="flex min-w-0 items-center gap-3">
        {state === "needs-you" && onShowRequest
          ? <button type="button" onClick={onShowRequest} aria-label="Needs you" className={cn(chipClass, "animate-pulse hover:animate-none")}>{chipText}</button>
          : <span className={chipClass}>{chipText}</span>}
        <h2 className="m-0 min-w-0 truncate text-[15px] font-semibold">{title}</h2>
        <span className="hidden shrink-0 font-lw-mono text-xs text-lw-faint sm:inline">{taskId}</span>
        {agentKind !== undefined && <AgentKindBadge kind={agentKind} isNative={native} className="hidden md:inline-flex" />}
        {notes && <span className="flex min-w-0 items-center gap-2 truncate text-xs text-lw-text-2">{notes}</span>}
      </div>
      <div className="flex shrink-0 items-center gap-2">
        {artifacts && <ArtifactsButton {...artifacts} />}
        {onOpenCard && (
          <button type="button" onClick={onOpenCard} aria-label="Open card" className={toolbarButton} title="Show this task on the board">
            <SquareKanban size={13} aria-hidden="true" className="sm:hidden" />
            <span className="hidden sm:inline">Open card</span>
          </button>
        )}
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
