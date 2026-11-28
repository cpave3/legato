import { createPortal } from "react-dom"
import { useMenuLayer } from "./ui/useMenuLayer"
import { useRef, useState, type ReactNode } from "react"
import { ArrowLeft, LayoutGrid, List, Mic, MoreHorizontal } from "lucide-react"
import { Link } from "react-router-dom"
import { cn } from "../lib/utils"
import { AgentKindBadge } from "./AgentKindBadge"
import { AgentStageChip, type AgentStageInfo } from "./AgentStageChip"
import { SubagentsLine } from "./SubagentTree"
import { useMediaQuery } from "../hooks/useMediaQuery"
import type { BackgroundJob } from "../lib/agentWire"
import type { AgentMenuEntry } from "../lib/agentMenu"

export type AgentChatState = "needs-you" | "running" | "background" | "error" | "idle"

interface AgentChatHeaderProps {
  state: AgentChatState
  // How long the agent has been in that state.
  time?: string
  title: string
  taskId: string
  agentKind?: string
  // The engram the agent runs as, shown beside its tool.
  engram?: string
  native?: boolean
  // Small things worth saying beside the title: a mode, a sync, an error.
  notes?: ReactNode
  // The pull request of the agent's task, when it has one.
  pullRequest?: number
  panels?: SidePanelsProps
  // How the agent's subagents are shown, when it has any.
  subagents?: SubagentSwitchProps
  // What the agent runs in the background: its subagents, at any depth,
  // are counted beside the title and open as a tree.
  backgroundJobs?: BackgroundJob[]
  // Brings what the agent waits on into view.
  onShowRequest?: () => void
  // A way to ask the Reeve about the agent, offered while it needs the person.
  ask?: ReactNode
  menu: AgentMenuEntry[]
  // Where the agent's work stands; none for an agent with no stage (the
  // Reeve). It adds Set stage to the menu.
  stage?: AgentStageInfo
}

const chips: Record<AgentChatState, { glyph: string; label: string; tone: string }> = {
  "needs-you": { glyph: "◆", label: "Needs you", tone: "bg-lw-needs-soft text-lw-needs" },
  running: { glyph: "●", label: "Running", tone: "bg-lw-live-soft text-lw-live" },
  background: { glyph: "◌", label: "Background", tone: "bg-lw-live-soft text-lw-live" },
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
function StudioButton({ onOpen, touch }: { onOpen: () => void; touch?: boolean }) {
  return (
    <button
      type="button"
      onClick={onOpen}
      aria-label="Studio"
      title="The studio, where you record"
      className={cn("flex h-[30px] shrink-0 items-center gap-1.5 rounded-md border border-lw-line-strong bg-lw-raised px-3 text-xs text-lw-text transition-colors hover:border-[var(--theme-border,#4a4955)]", touch && "max-sm:size-11 max-sm:justify-center max-sm:px-0")}
    >
      <Mic size={12} />
      <span className="hidden sm:inline">Studio</span>
    </button>
  )
}

export interface SubagentSwitchProps {
  total: number
  running: number
  view: "inline" | "tiles"
  onView: (view: "inline" | "tiles") => void
}

// SubagentSwitch chooses whether subagents are drawn in the conversation or
// as tiles beside it.
export function SubagentSwitch({ total, running, view, onView }: SubagentSwitchProps) {
  const look = (option: "inline" | "tiles") => cn(segment, view === option ? "bg-lw-selected text-lw-text" : "text-lw-text-2 hover:text-lw-text")
  return (
    <div role="group" aria-label="Sub-agents" className="flex shrink-0 rounded-md border border-lw-line bg-lw-surface p-0.5">
      <button type="button" aria-pressed={view === "inline"} aria-label="Inline" title="Sub-agents in the conversation" onClick={() => onView("inline")} className={look("inline")}>
        <List size={12} aria-hidden="true" /><span className="hidden lg:inline">Inline</span>
      </button>
      <button
        type="button"
        aria-pressed={view === "tiles"}
        aria-label={`Tiles (${total})${running > 0 ? `, ${running} running` : ""}`}
        title="Sub-agents as tiles beside the conversation"
        onClick={() => onView("tiles")}
        className={look("tiles")}
      >
        <LayoutGrid size={12} aria-hidden="true" /><span className="hidden lg:inline">Tiles</span>
        <span className="font-lw-mono text-lw-text-2">{total}</span>
        {running > 0 && <span aria-hidden="true" className="size-1.5 rounded-full bg-lw-live" />}
      </button>
    </div>
  )
}

const segment = "flex h-[26px] items-center gap-1.5 rounded px-2.5 text-xs transition-colors disabled:cursor-default disabled:opacity-50"
// Full-size targets on a phone, sharing the row they are given.
const touchSegment = "max-sm:h-11 max-sm:flex-1 max-sm:justify-center max-sm:px-3"

// SidePanelSwitch chooses what the column beside the chat shows: the work
// related to the agent's task, or what the agent has shown.
export function SidePanelSwitch({ open, related, artifacts, onSelect, studio, touch }: SidePanelsProps & { touch?: boolean }) {
  const look = (panel: SidePanel) => cn(segment, touch && touchSegment, open === panel ? "bg-lw-selected text-lw-text" : "text-lw-text-2 enabled:hover:text-lw-text")
  return (
    <>
      {studio && <StudioButton onOpen={studio.onOpen} touch={touch} />}
      <div role="group" aria-label="Side panel" className={cn("flex shrink-0 rounded-md border border-lw-line bg-lw-surface p-0.5", touch && "max-sm:flex-1 max-sm:p-0")}>
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
// be done with it. On a phone it is two parts: what the agent is doing, which
// wraps, and the controls, one row of full-size targets.
export function AgentChatHeader({ state, time, title, taskId, agentKind, engram, native = false, notes, pullRequest, panels, subagents, backgroundJobs, onShowRequest, ask, menu, stage }: AgentChatHeaderProps) {
  const chip = chips[state]
  const phone = useMediaQuery(phoneQuery)
  const [stageOpen, setStageOpen] = useState(false)
  const entries = [
    ...(stage ? [{ label: "Set stage", onSelect: () => setStageOpen(true) }] : []),
    // A phone's control row has no room for the switch.
    ...(phone && subagents && subagents.total > 0 ? [subagentViewEntry(subagents)] : []),
    ...menu,
  ]
  // On a phone the agent picker above says how long; the time is left out.
  const chipText = <><span aria-hidden="true">{chip.glyph} </span>{chip.label}{time && <span className="max-sm:hidden"> {time}</span>}</>
  const chipClass = cn("shrink-0 rounded px-2 font-lw-mono text-[11px] uppercase", chip.tone)

  return (
    <header className="flex shrink-0 items-center justify-between gap-2 border-b border-[var(--theme-border,#222228)] bg-lw-ground px-3 py-2 font-lw text-[13px] text-lw-text max-sm:flex-col max-sm:items-stretch max-sm:gap-0 sm:flex-wrap sm:gap-3 sm:px-4 sm:py-2.5 md:px-6">
      {/* On a phone the title is the picker's, so a line with nothing else on it
          takes no room. Wider, the controls move to a line of their own only
          when they would leave this less than 20rem. */}
      <div className="flex min-w-0 items-center gap-3 sm:flex-[1_1_20rem] max-sm:flex-wrap max-sm:gap-x-2 max-sm:gap-y-1.5 max-sm:has-[>:not(h2)]:pb-2">
        {state === "needs-you" && onShowRequest
          ? <button type="button" onClick={onShowRequest} aria-label="Needs you" className={cn(chipClass, "animate-pulse hover:animate-none max-sm:h-11")}>{chipText}</button>
          // The picker says what the agent is doing on a phone; a request to
          // bring into view stays.
          : <span className={cn(chipClass, "max-sm:hidden")}>{chipText}</span>}
        {/* On a phone the control row has no room for it; it belongs beside the request. */}
        {phone && state === "needs-you" && ask}
        {stage && <AgentStageChip info={stage} open={stageOpen} onOpenChange={setStageOpen} />}
        {/* On a phone the agent picker above names the agent. On a crowded
            wider screen the title gives way first; the stage, the ID, the
            tool and the sub-agents only once it has gone, and never over
            what is beside them. */}
        <h2 title={title} className="m-0 min-w-0 shrink-[1000] truncate text-[15px] font-semibold max-sm:sr-only">{title}</h2>
        <span className="hidden min-w-0 shrink truncate font-lw-mono text-xs text-lw-faint sm:inline">{taskId}{pullRequest ? ` · PR #${pullRequest}` : ""}</span>
        {agentKind !== undefined && <AgentKindBadge kind={agentKind} engram={engram} isNative={native} className="hidden min-w-0 shrink overflow-hidden md:inline-flex" />}
        <SubagentsLine jobs={backgroundJobs} className="max-w-full shrink" touch />
        {notes && <span className="flex min-w-0 items-center gap-2 truncate text-xs text-lw-text-2">{notes}</span>}
      </div>
      <div className="flex shrink-0 items-center gap-2 sm:ml-auto">
        {!phone && state === "needs-you" && ask}
        {!phone && subagents && subagents.total > 0 && <SubagentSwitch {...subagents} />}
        {panels && <SidePanelSwitch {...panels} touch />}
        <AgentMoreMenu entries={entries} touch />
      </div>
    </header>
  )
}

// Tailwind's max-sm: narrower than its sm breakpoint, 640px.
const phoneQuery = "(max-width: 639px)"

// The sub-agent switch as one entry of the ⋯ menu: the other view.
function subagentViewEntry({ total, view, onView }: SubagentSwitchProps): AgentMenuEntry {
  return view === "inline"
    ? { label: `Sub-agents as tiles (${total})`, icon: <LayoutGrid size={12} aria-hidden="true" />, onSelect: () => onView("tiles") }
    : { label: "Sub-agents in the conversation", icon: <List size={12} aria-hidden="true" />, onSelect: () => onView("inline") }
}

// The way back to the workflow run an agent was opened from: a line of its
// own above the agent's header, so it never covers the controls.
export function BackToWorkflow({ runId }: { runId: string }) {
  return (
    <Link
      to={`/workflows/runs/${encodeURIComponent(runId)}`}
      className="flex min-h-11 shrink-0 items-center gap-1.5 border-b border-[var(--theme-border,#222228)] bg-lw-ground px-3 text-xs text-lw-link hover:underline sm:min-h-8 sm:px-4 md:px-6"
    >
      <ArrowLeft size={12} aria-hidden="true" />Back to workflow
    </Link>
  )
}

// The ⋯ button and the menu of an agent's other actions, or of another
// row's, named by label.
// touch: a full-size target on a phone.
export function AgentMoreMenu({ entries, label = "More actions", touch }: { entries: AgentMenuEntry[]; label?: string; touch?: boolean }) {
  const [open, setOpen] = useState(false)
  const box = useRef<HTMLDivElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  useMenuLayer({ open, onOpen: () => setOpen(true), onClose: () => setOpen(false), containerRef: box, triggerRef: trigger, align: "right" })
  const plain = entries.filter((entry) => !entry.danger)
  const danger = entries.filter((entry) => entry.danger)

  if (entries.length === 0) return null

  const item = (entry: AgentMenuEntry) => (
    <button
      key={entry.label}
      type="button"
      role="menuitem"
      disabled={entry.disabled}
      onClick={() => { setOpen(false); trigger.current?.focus(); entry.onSelect() }}
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
    <div className="relative">
      <button
        type="button"
        ref={trigger}
        aria-label={label}
        title={label}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
        className={cn("flex h-[30px] w-[30px] items-center justify-center rounded-md border border-lw-line-strong text-lw-text hover:border-[var(--theme-border,#4a4955)]", touch && "max-sm:size-11", open ? "bg-lw-selected" : "bg-lw-raised")}
      >
        <MoreHorizontal size={14} />
      </button>
      {open && createPortal(
        <div ref={box} role="menu" className="absolute right-0 top-full z-30 mt-1 max-h-[60vh] min-w-[200px] max-w-[calc(100vw-2rem)] overflow-y-auto rounded-md border border-lw-line-strong bg-lw-surface py-1 shadow-xl">
          {plain.map(item)}
          {danger.length > 0 && plain.length > 0 && <div className="my-1 border-t border-lw-line" />}
          {danger.map(item)}
        </div>, document.body
      )}
    </div>
  )
}
