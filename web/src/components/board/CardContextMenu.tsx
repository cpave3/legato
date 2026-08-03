import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react"
import { ChevronRight, ExternalLink, Move, Trash2, Workflow } from "lucide-react"
import type { BoardCard } from "../../lib/board-types"
import type { CardWorkflowOption } from "../../lib/card-workflows"

export type CardContextAction = "open" | "move" | "delete"

const primaryWidth = 240
const primaryHeight = 196
const flyoutWidth = 280
const viewportGap = 8
const panelGap = 4

export function CardContextMenu({
  card,
  position,
  workflows = [],
  workflowLoading = false,
  workflowWarning = "",
  onAction,
  onWorkflow,
  onClose,
}: {
  card: BoardCard
  position: { x: number; y: number }
  workflows?: CardWorkflowOption[]
  workflowLoading?: boolean
  workflowWarning?: string
  onAction: (action: CardContextAction) => void
  onWorkflow?: (workflow: CardWorkflowOption) => void
  onClose: () => void
}) {
  const menuRef = useRef<HTMLDivElement>(null)
  const flyoutRef = useRef<HTMLDivElement>(null)
  const workflowTriggerRef = useRef<HTMLButtonElement>(null)
  const [flyoutOpen, setFlyoutOpen] = useState(false)
  const focusFlyoutOnOpen = useRef(false)
  const suppressFocusOpen = useRef(false)
  const [flyoutAnchorTop, setFlyoutAnchorTop] = useState(position.y)
  const left = Math.max(viewportGap, Math.min(position.x, window.innerWidth - primaryWidth - viewportGap))
  const top = Math.max(viewportGap, Math.min(position.y, window.innerHeight - primaryHeight - viewportGap))
  const flyoutLeft = left + primaryWidth + panelGap + flyoutWidth <= window.innerWidth - viewportGap
    ? left + primaryWidth + panelGap
    : Math.max(viewportGap, left - flyoutWidth - panelGap)
  const flyoutTop = Math.max(viewportGap, Math.min(flyoutAnchorTop, window.innerHeight - viewportGap - 40))
  const flyoutMaxHeight = Math.max(80, window.innerHeight - flyoutTop - viewportGap)

  const openFlyout = useCallback((focusFirst = false) => {
    const rect = workflowTriggerRef.current?.getBoundingClientRect()
    setFlyoutAnchorTop(rect && rect.height > 0 ? rect.top : top + 156)
    focusFlyoutOnOpen.current = focusFirst
    setFlyoutOpen(true)
  }, [top])

  const closeFlyout = useCallback(() => {
    setFlyoutOpen(false)
    suppressFocusOpen.current = true
    workflowTriggerRef.current?.focus()
    suppressFocusOpen.current = false
  }, [])

  useLayoutEffect(() => {
    if (flyoutOpen && focusFlyoutOnOpen.current) {
      focusFlyoutOnOpen.current = false
      flyoutRef.current?.querySelector<HTMLButtonElement>("[role=menuitem]")?.focus()
    }
  }, [flyoutOpen])

  useEffect(() => {
    menuRef.current?.querySelector<HTMLButtonElement>("button")?.focus()
  }, [])

  useEffect(() => {
    const keydown = (event: KeyboardEvent) => {
      const active = document.activeElement
      const inFlyout = !!flyoutRef.current?.contains(active)
      if (event.key === "Escape") {
        event.preventDefault()
        if (flyoutOpen) closeFlyout()
        else onClose()
        return
      }
      if (active === workflowTriggerRef.current && (event.key === "ArrowRight" || event.key === "Enter")) {
        event.preventDefault()
        openFlyout(true)
        return
      }
      if (inFlyout && event.key === "ArrowLeft") {
        event.preventDefault()
        closeFlyout()
        return
      }
      if (inFlyout && event.key === "Enter") {
        const index = Array.from(flyoutRef.current?.querySelectorAll<HTMLButtonElement>("[role=menuitem]") ?? []).indexOf(active as HTMLButtonElement)
        if (index >= 0 && workflows[index]) {
          event.preventDefault()
          onWorkflow?.(workflows[index])
        }
        return
      }
      if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return
      const panel = inFlyout ? flyoutRef.current : menuRef.current
      const buttons = Array.from(panel?.querySelectorAll<HTMLButtonElement>("[role=menuitem]") ?? [])
      if (buttons.length === 0) return
      event.preventDefault()
      const current = buttons.indexOf(active as HTMLButtonElement)
      const offset = event.key === "ArrowDown" ? 1 : -1
      buttons[(current + offset + buttons.length) % buttons.length].focus()
    }
    const scroll = (event: Event) => {
      const target = event.target
      if (target instanceof Node && (menuRef.current?.contains(target) || flyoutRef.current?.contains(target))) return
      onClose()
    }
    window.addEventListener("keydown", keydown)
    window.addEventListener("resize", onClose)
    window.addEventListener("scroll", scroll, true)
    return () => {
      window.removeEventListener("keydown", keydown)
      window.removeEventListener("resize", onClose)
      window.removeEventListener("scroll", scroll, true)
    }
  }, [closeFlyout, flyoutOpen, onClose, onWorkflow, openFlyout, workflows])

  const items: { action: CardContextAction; label: string; icon: React.ReactNode }[] = [
    { action: "open", label: "Open details", icon: <ExternalLink size={16} /> },
    { action: "move", label: "Move", icon: <Move size={16} /> },
    { action: "delete", label: "Delete", icon: <Trash2 size={16} /> },
  ]

  return <div className="fixed inset-0 z-40" onPointerDown={onClose} data-testid="card-context-backdrop">
    <div ref={menuRef} role="menu" aria-label={`Actions for ${card.title}`} onPointerDown={event => event.stopPropagation()} className="fixed w-60 rounded-lg border border-zinc-700 bg-zinc-900 p-1 shadow-2xl" style={{ left, top }}>
      <p className="truncate border-b border-zinc-800 px-3 py-2 text-xs text-zinc-500">{card.id} · {card.title}</p>
      {items.map(item => <button key={item.action} type="button" role="menuitem" onClick={() => onAction(item.action)} className={`flex min-h-10 w-full items-center gap-3 rounded px-3 text-left text-sm hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none ${item.action === "delete" ? "text-red-300" : "text-zinc-200"}`}>{item.icon}<span>{item.label}</span></button>)}
      <button ref={workflowTriggerRef} type="button" role="menuitem" aria-haspopup="menu" aria-expanded={flyoutOpen} onMouseEnter={() => openFlyout()} onFocus={() => { if (!suppressFocusOpen.current) openFlyout() }} onClick={() => openFlyout(true)} className="mt-1 flex min-h-10 w-full items-center gap-3 border-t border-zinc-800 px-3 pt-1 text-left text-sm text-zinc-200 hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none"><Workflow size={16} /><span>Workflows</span><ChevronRight className="ml-auto" size={15} /></button>
    </div>
    {flyoutOpen && <div ref={flyoutRef} role="menu" aria-label={`Workflows for ${card.title}`} onPointerDown={event => event.stopPropagation()} className="fixed w-70 overflow-y-auto rounded-lg border border-zinc-700 bg-zinc-900 p-1 shadow-2xl" style={{ left: flyoutLeft, top: flyoutTop, width: flyoutWidth, maxHeight: flyoutMaxHeight }}>
      <p className="border-b border-zinc-800 px-3 py-2 text-xs font-medium uppercase tracking-wide text-zinc-500">Workflows</p>
      {workflowLoading && <p className="px-3 py-2 text-sm text-zinc-500">Loading workflows…</p>}
      {!workflowLoading && workflows.length === 0 && <p className="px-3 py-2 text-sm text-zinc-500">No eligible workflows</p>}
      {workflows.map(workflow => <button key={workflow.definition.name} type="button" role="menuitem" onClick={() => onWorkflow?.(workflow)} className="flex min-h-10 w-full items-center rounded px-3 text-left text-sm text-zinc-200 hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none"><span className="truncate">{workflow.definition.title}</span></button>)}
      {workflowWarning && <p className="border-t border-zinc-800 px-3 py-2 text-xs text-amber-300">{workflowWarning}</p>}
    </div>}
  </div>
}
