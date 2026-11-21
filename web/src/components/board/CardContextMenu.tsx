import { createPortal } from "react-dom"
import { useMenuLayer } from "../../hooks/useMenuLayer"
import { useRef, useState, useSyncExternalStore, type ReactNode, type RefObject } from "react"
import { ChevronRight, ExternalLink, Move, Play, Repeat, Send, Trash2, Undo2, Workflow } from "lucide-react"
import type { BoardCard, PRMetaView } from "../../lib/board-types"
import type { CardWorkflowOption } from "../../lib/card-workflows"
import { macroAgentLine, type Macro } from "../../lib/macros"
import { agentKindLabel } from "../../lib/agentKind"
import { LoadingState } from "../ui/LoadingState"
import { providerLabel } from "../../lib/providers"
import type { PRMacroChoice } from "../../lib/cardMacros"
import { awayActions } from "../../lib/awayReturn"

const subscribeViewport = (notify: () => void) => {
  window.addEventListener("resize", notify)
  return () => window.removeEventListener("resize", notify)
}
const compactViewport = () => window.innerWidth < 640

export type CardContextAction = "open" | "move" | "delete" | "handoff" | "openOnWorker" | "openSource" | "recall"

// A menu beside the card's menu (on a phone, in its place, with a way back).
function MenuFlyout({ label, title, triggerRef, root, compact, position, onBack, onCloseAll, children }: {
  label: string
  title: string
  triggerRef: RefObject<HTMLElement | null>
  root: RefObject<HTMLDivElement | null>
  compact: boolean
  position: { x: number; y: number }
  onBack: () => void
  onCloseAll: () => void
  children: ReactNode
}) {
  const flyoutRef = useRef<HTMLDivElement>(null)
  useMenuLayer({ open: true, onClose: onCloseAll, containerRef: flyoutRef, outsideRef: root, triggerRef, placement: "side", position: compact ? position : undefined })
  return <div ref={flyoutRef} role="menu" aria-label={label} onPointerDown={event => event.stopPropagation()} className="fixed w-70 overflow-y-auto rounded-lg border border-zinc-700 bg-zinc-900 p-1 shadow-2xl" onKeyDownCapture={event => { if (event.key === "ArrowLeft" || event.key === "Escape") { event.preventDefault(); event.stopPropagation(); onBack() } }}>
    {compact && <button type="button" role="menuitem" onClick={onBack} className="min-h-10 w-full rounded px-3 text-left text-sm text-zinc-200 hover:bg-zinc-800">Back</button>}
    <p className="border-b border-zinc-800 px-3 py-2 text-xs font-medium uppercase tracking-wide text-zinc-500">{title}</p>
    {children}
  </div>
}

function MacroItem({ macro, onPick }: { macro: Macro; onPick: () => void }) {
  const agentLine = macroAgentLine(macro, agentKindLabel)
  return <button type="button" role="menuitem" aria-label={`Macro: ${macro.name}`} onClick={onPick} className="flex min-h-10 w-full items-center gap-3 rounded px-3 py-1 text-left text-sm text-zinc-200 hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none"><Play size={16} className="shrink-0" /><span className="min-w-0"><span className="block truncate">{macro.name}</span>{agentLine && <span className="block truncate text-[11px] text-lw-text-2">{agentLine}</span>}</span></button>
}

export function CardContextMenu({
  card,
  position,
  workflows = [],
  workflowLoading = false,
  workflowWarning = "",
  canHandOff = false,
  reviewPasses = [],
  reviewStack = false,
  onReviewPasses,
  macros = [],
  onMacro,
  prMacros = [],
  onPRMacro,
  onManageMacros,
  onAction,
  onWorkflow,
  onClose,
}: {
  card: BoardCard
  position: { x: number; y: number }
  workflows?: CardWorkflowOption[]
  workflowLoading?: boolean
  workflowWarning?: string
  // Whether this main has a worker to hand the task off to.
  canHandOff?: boolean
  // The card's pull requests a review loop can start on, with its own agent
  // as the author: one item each, so a card with several asks which.
  reviewPasses?: PRMetaView[]
  // Whether the review loop workflow takes a scope. Then a card whose pull
  // requests (in stack order) are several in one repository offers the
  // whole stack first, started on the bottom one, and each one alone.
  reviewStack?: boolean
  onReviewPasses?: (pr: PRMetaView, scope?: "stack" | "pr") => void
  // The macros for a task: each starts the task's agent with it, or queues
  // it to the agent running.
  macros?: Macro[]
  onMacro?: (macro: Macro) => void
  // The card's open pull requests, bottom first, each with its macros: one
  // item each opens them, and a pick starts the macro on that PR.
  prMacros?: PRMacroChoice[]
  onPRMacro?: (pr: PRMetaView, macro: Macro) => void
  // Opens the Macros page.
  onManageMacros?: () => void
  onAction: (action: CardContextAction) => void
  onWorkflow?: (workflow: CardWorkflowOption) => void
  onClose: () => void
}) {
  const menuRef = useRef<HTMLDivElement>(null)
  // The item whose flyout is open: Workflows, or a pull request's Macros.
  const flyoutTriggerRef = useRef<HTMLElement | null>(null)
  const [flyout, setFlyout] = useState<string | null>(null)
  const root = useRef<HTMLDivElement>(null)
  const compact = useSyncExternalStore(subscribeViewport, compactViewport)
  const openFlyout = (key: string, trigger: HTMLElement) => { flyoutTriggerRef.current = trigger; setFlyout(key) }
  const closeFlyout = () => { setFlyout(null); flyoutTriggerRef.current?.focus() }
  const { restoreFocus } = useMenuLayer({ open: true, onClose, containerRef: menuRef, outsideRef: root, position })
  const pick = (run: () => void) => { restoreFocus(); onClose(); run() }
  const flyoutTrigger = (key: string) => ({
    "aria-haspopup": "menu" as const,
    "aria-expanded": flyout === key,
    onMouseEnter: (event: React.MouseEvent<HTMLElement>) => { if (!compact) openFlyout(key, event.currentTarget) },
    onKeyDown: (event: React.KeyboardEvent<HTMLElement>) => { if (event.key === "ArrowRight") { event.preventDefault(); openFlyout(key, event.currentTarget) } },
    onClick: (event: React.MouseEvent<HTMLElement>) => openFlyout(key, event.currentTarget),
  })
  const prKey = (pr: PRMetaView) => `pr:${pr.repo}#${pr.pr_number}`
  const openPR = flyout?.startsWith("pr:") ? prMacros.find(choice => prKey(choice.pr) === flyout) : undefined
  // An away task can change only on its worker: it offers its details and
  // the worker. Another may be handed off once a worker is paired.
  const stack = reviewStack && reviewPasses.length > 1 && reviewPasses.every(pr => pr.repo === reviewPasses[0].repo)
  const reviewItems: { key: string; label: string; pr: PRMetaView; scope?: "stack" | "pr" }[] = [
    ...(stack ? [{ key: "stack", label: `Review passes · stack #${reviewPasses[0].pr_number}–#${reviewPasses[reviewPasses.length - 1].pr_number}`, pr: reviewPasses[0], scope: "stack" as const }] : []),
    ...reviewPasses.map(pr => ({ key: `${pr.repo}-${pr.pr_number}`, label: `Review passes · #${pr.pr_number}`, pr, scope: stack ? "pr" as const : undefined })),
  ]
  const source = card.source_url ? [{ action: "openSource" as const, label: `Open in ${providerLabel(card.provider)}`, icon: <ExternalLink size={16} /> }] : []
  const items: { action: CardContextAction; label: string; icon: React.ReactNode }[] = card.away
    ? [
      { action: "open", label: "Open details", icon: <ExternalLink size={16} /> },
      ...source,
      ...(card.away.url ? [{ action: "openOnWorker" as const, label: `Open on ${card.away.worker}`, icon: <ExternalLink size={16} /> }] : []),
      ...(awayActions(card.away).recall ? [{ action: "recall" as const, label: "Recall…", icon: <Undo2 size={16} /> }] : []),
    ]
    : [
      { action: "open", label: "Open details", icon: <ExternalLink size={16} /> },
      ...source,
      { action: "move", label: "Move", icon: <Move size={16} /> },
      ...(canHandOff ? [{ action: "handoff" as const, label: "Hand off…", icon: <Send size={16} /> }] : []),
      { action: "delete", label: "Delete", icon: <Trash2 size={16} /> },
    ]
  return createPortal(<div ref={root} className="fixed inset-0 z-40" onPointerDown={onClose} data-testid="card-context-backdrop">
    <div ref={menuRef} role="menu" aria-label={`Actions for ${card.title}`} onPointerDown={event => event.stopPropagation()} className="fixed w-60 rounded-lg border border-zinc-700 bg-zinc-900 p-1 shadow-2xl" style={{ visibility: compact && flyout ? "hidden" : undefined }}>
      <p className="truncate border-b border-zinc-800 px-3 py-2 text-xs text-zinc-500">{card.id} · {card.title}</p>
      {items.map(item => <button key={item.action} type="button" role="menuitem" onClick={() => { restoreFocus(); onClose(); onAction(item.action) }} className={`flex min-h-10 w-full items-center gap-3 rounded px-3 text-left text-sm hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none ${item.action === "delete" ? "text-red-300" : "text-zinc-200"}`}>{item.icon}<span>{item.label}</span></button>)}
      {!card.away && onReviewPasses && reviewItems.map(item => <button key={`review-${item.key}`} type="button" role="menuitem" onClick={() => { restoreFocus(); onClose(); if (item.scope) onReviewPasses(item.pr, item.scope); else onReviewPasses(item.pr) }} className="flex min-h-10 w-full items-center gap-3 rounded px-3 text-left text-sm text-zinc-200 hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none"><Repeat size={16} /><span>{item.label}</span></button>)}
      {!card.away && ((onMacro && macros.length > 0) || (onPRMacro && prMacros.length > 0)) && <>
        <p className="mt-1 border-t border-zinc-800 px-3 pb-1 pt-2 text-xs font-medium uppercase tracking-wide text-zinc-500">Macros</p>
        {onMacro && macros.map(macro => <MacroItem key={`macro-${macro.name}`} macro={macro} onPick={() => pick(() => onMacro(macro))} />)}
        {onPRMacro && prMacros.map(choice => <button key={prKey(choice.pr)} type="button" role="menuitem" aria-label={`Macros for #${choice.pr.pr_number}`} {...flyoutTrigger(prKey(choice.pr))} className="flex min-h-10 w-full items-center gap-3 rounded px-3 text-left text-sm text-zinc-200 hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none"><Play size={16} className="shrink-0" /><span className="min-w-0 truncate"><span className="font-lw-mono">#{choice.pr.pr_number}</span> <span className="text-[11px] text-lw-text-2">{choice.pr.repo}</span></span><ChevronRight className="ml-auto shrink-0" size={15} /></button>)}
        {onManageMacros && <button type="button" role="menuitem" onClick={() => pick(onManageMacros)} className="flex min-h-10 w-full items-center rounded px-3 text-left text-sm text-lw-link hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none">Manage macros</button>}
      </>}
      {!card.away && <button type="button" role="menuitem" {...flyoutTrigger("workflows")} className="mt-1 flex min-h-10 w-full items-center gap-3 border-t border-zinc-800 px-3 pt-1 text-left text-sm text-zinc-200 hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none"><Workflow size={16} /><span>Workflows</span><ChevronRight className="ml-auto" size={15} /></button>}
    </div>
    {flyout === "workflows" && <MenuFlyout key="workflows" label={`Workflows for ${card.title}`} title="Workflows" triggerRef={flyoutTriggerRef} root={root} compact={compact} position={position} onBack={closeFlyout} onCloseAll={() => { restoreFocus(); onClose() }}>
      {workflowLoading && <LoadingState size="inline" label="Loading workflows…" className="px-3 py-2" />}
      {!workflowLoading && workflows.length === 0 && <p className="px-3 py-2 text-sm text-zinc-500">No eligible workflows</p>}
      {workflows.map(workflow => <button key={workflow.definition.name} type="button" role="menuitem" onClick={() => pick(() => onWorkflow?.(workflow))} className="flex min-h-10 w-full items-center rounded px-3 text-left text-sm text-zinc-200 hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none"><span className="truncate">{workflow.definition.title}</span></button>)}
      {workflowWarning && <p className="border-t border-zinc-800 px-3 py-2 text-xs text-amber-300">{workflowWarning}</p>}
    </MenuFlyout>}
    {openPR && onPRMacro && <MenuFlyout key={flyout} label={`Macros for #${openPR.pr.pr_number}`} title={`Macros · #${openPR.pr.pr_number}`} triggerRef={flyoutTriggerRef} root={root} compact={compact} position={position} onBack={closeFlyout} onCloseAll={() => { restoreFocus(); onClose() }}>
      {openPR.macros.map(macro => <MacroItem key={macro.name} macro={macro} onPick={() => pick(() => onPRMacro(openPR.pr, macro))} />)}
    </MenuFlyout>}
  </div>, document.body)
}
