import { createPortal } from "react-dom"
import { useMenuLayer } from "../../hooks/useMenuLayer"
import { useRef, useState, useSyncExternalStore } from "react"
import { ChevronRight, ExternalLink, Move, Repeat, Send, Trash2, Workflow } from "lucide-react"
import type { BoardCard, PRMetaView } from "../../lib/board-types"
import type { CardWorkflowOption } from "../../lib/card-workflows"
import { LoadingState } from "../ui/LoadingState"

const subscribeViewport = (notify: () => void) => {
  window.addEventListener("resize", notify)
  return () => window.removeEventListener("resize", notify)
}
const compactViewport = () => window.innerWidth < 640

export type CardContextAction = "open" | "move" | "delete" | "handoff" | "openOnWorker"

export function CardContextMenu({
  card,
  position,
  workflows = [],
  workflowLoading = false,
  workflowWarning = "",
  canHandOff = false,
  reviewPasses = [],
  onReviewPasses,
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
  onReviewPasses?: (pr: PRMetaView) => void
  onAction: (action: CardContextAction) => void
  onWorkflow?: (workflow: CardWorkflowOption) => void
  onClose: () => void
}) {
  const menuRef = useRef<HTMLDivElement>(null)
  const flyoutRef = useRef<HTMLDivElement>(null)
  const workflowTriggerRef = useRef<HTMLButtonElement>(null)
  const [flyoutOpen, setFlyoutOpen] = useState(false)
  const root = useRef<HTMLDivElement>(null)
  const compact = useSyncExternalStore(subscribeViewport, compactViewport)
  const closeFlyout = () => { setFlyoutOpen(false); workflowTriggerRef.current?.focus() }
  const { restoreFocus } = useMenuLayer({ open: true, onClose, containerRef: menuRef, outsideRef: root, position })
  useMenuLayer({ open: flyoutOpen, onClose: () => { restoreFocus(); onClose() }, containerRef: flyoutRef, outsideRef: root, triggerRef: workflowTriggerRef, placement: "side", position: compact ? position : undefined })
  // An away task can change only on its worker: it offers its details and
  // the worker. Another may be handed off once a worker is paired.
  const items: { action: CardContextAction; label: string; icon: React.ReactNode }[] = card.away
    ? [
      { action: "open", label: "Open details", icon: <ExternalLink size={16} /> },
      ...(card.away.url ? [{ action: "openOnWorker" as const, label: `Open on ${card.away.worker}`, icon: <ExternalLink size={16} /> }] : []),
    ]
    : [
      { action: "open", label: "Open details", icon: <ExternalLink size={16} /> },
      { action: "move", label: "Move", icon: <Move size={16} /> },
      ...(canHandOff ? [{ action: "handoff" as const, label: "Hand off…", icon: <Send size={16} /> }] : []),
      { action: "delete", label: "Delete", icon: <Trash2 size={16} /> },
    ]
  return createPortal(<div ref={root} className="fixed inset-0 z-40" onPointerDown={onClose} data-testid="card-context-backdrop">
    <div ref={menuRef} role="menu" aria-label={`Actions for ${card.title}`} onPointerDown={event => event.stopPropagation()} className="fixed w-60 rounded-lg border border-zinc-700 bg-zinc-900 p-1 shadow-2xl" style={{ visibility: compact && flyoutOpen ? "hidden" : undefined }}>
      <p className="truncate border-b border-zinc-800 px-3 py-2 text-xs text-zinc-500">{card.id} · {card.title}</p>
      {items.map(item => <button key={item.action} type="button" role="menuitem" onClick={() => { restoreFocus(); onClose(); onAction(item.action) }} className={`flex min-h-10 w-full items-center gap-3 rounded px-3 text-left text-sm hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none ${item.action === "delete" ? "text-red-300" : "text-zinc-200"}`}>{item.icon}<span>{item.label}</span></button>)}
      {!card.away && onReviewPasses && reviewPasses.map(pr => <button key={`review-${pr.repo}-${pr.pr_number}`} type="button" role="menuitem" onClick={() => { restoreFocus(); onClose(); onReviewPasses(pr) }} className="flex min-h-10 w-full items-center gap-3 rounded px-3 text-left text-sm text-zinc-200 hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none"><Repeat size={16} /><span>Review passes · #{pr.pr_number}</span></button>)}
      {!card.away && <button ref={workflowTriggerRef} type="button" role="menuitem" aria-haspopup="menu" aria-expanded={flyoutOpen} onMouseEnter={() => { if (!compact) setFlyoutOpen(true) }} onKeyDown={event => { if (event.key === "ArrowRight") { event.preventDefault(); setFlyoutOpen(true) } }} onClick={() => setFlyoutOpen(true)} className="mt-1 flex min-h-10 w-full items-center gap-3 border-t border-zinc-800 px-3 pt-1 text-left text-sm text-zinc-200 hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none"><Workflow size={16} /><span>Workflows</span><ChevronRight className="ml-auto" size={15} /></button>}
    </div>
    {flyoutOpen && <div ref={flyoutRef} role="menu" aria-label={`Workflows for ${card.title}`} onPointerDown={event => event.stopPropagation()} className="fixed w-70 overflow-y-auto rounded-lg border border-zinc-700 bg-zinc-900 p-1 shadow-2xl" onKeyDownCapture={event => { if (event.key === "ArrowLeft" || event.key === "Escape") { event.preventDefault(); event.stopPropagation(); closeFlyout() } }}>
      {compact && <button type="button" role="menuitem" onClick={closeFlyout} className="min-h-10 w-full rounded px-3 text-left text-sm text-zinc-200 hover:bg-zinc-800">Back</button>}
      <p className="border-b border-zinc-800 px-3 py-2 text-xs font-medium uppercase tracking-wide text-zinc-500">Workflows</p>
      {workflowLoading && <LoadingState size="inline" label="Loading workflows…" className="px-3 py-2" />}
      {!workflowLoading && workflows.length === 0 && <p className="px-3 py-2 text-sm text-zinc-500">No eligible workflows</p>}
      {workflows.map(workflow => <button key={workflow.definition.name} type="button" role="menuitem" onClick={() => { restoreFocus(); onClose(); onWorkflow?.(workflow) }} className="flex min-h-10 w-full items-center rounded px-3 text-left text-sm text-zinc-200 hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none"><span className="truncate">{workflow.definition.title}</span></button>)}
      {workflowWarning && <p className="border-t border-zinc-800 px-3 py-2 text-xs text-amber-300">{workflowWarning}</p>}
    </div>}
  </div>, document.body)
}
