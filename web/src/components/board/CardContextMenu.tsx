import { useEffect, useRef } from "react"
import { ChevronRight, ExternalLink, Move, Trash2, Workflow } from "lucide-react"
import type { BoardCard } from "../../lib/board-types"
import type { CardWorkflowOption } from "../../lib/card-workflows"

export type CardContextAction = "open" | "move" | "delete"

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
  const width = 240
  const height = 196
  const left = Math.max(8, Math.min(position.x, window.innerWidth - width - 8))
  const top = Math.max(8, Math.min(position.y, window.innerHeight - height - 8))

  useEffect(() => {
    const close = () => onClose()
    const keydown = (event: KeyboardEvent) => {
      if (event.key === "Escape") { close(); return }
      if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return
      const buttons = Array.from(menuRef.current?.querySelectorAll<HTMLButtonElement>("[role=menuitem]") ?? [])
      if (buttons.length === 0) return
      event.preventDefault()
      const current = buttons.indexOf(document.activeElement as HTMLButtonElement)
      const offset = event.key === "ArrowDown" ? 1 : -1
      buttons[(current + offset + buttons.length) % buttons.length].focus()
    }
    window.addEventListener("keydown", keydown)
    window.addEventListener("resize", close)
    window.addEventListener("scroll", close, true)
    menuRef.current?.querySelector<HTMLButtonElement>("button")?.focus()
    return () => {
      window.removeEventListener("keydown", keydown)
      window.removeEventListener("resize", close)
      window.removeEventListener("scroll", close, true)
    }
  }, [onClose])

  const items: { action: CardContextAction; label: string; icon: React.ReactNode }[] = [
    { action: "open", label: "Open details", icon: <ExternalLink size={16} /> },
    { action: "move", label: "Move", icon: <Move size={16} /> },
    { action: "delete", label: "Delete", icon: <Trash2 size={16} /> },
  ]

  return (
    <div className="fixed inset-0 z-40" onPointerDown={onClose} data-testid="card-context-backdrop">
      <div
        ref={menuRef}
        role="menu"
        aria-label={`Actions for ${card.title}`}
        onPointerDown={(event) => event.stopPropagation()}
        className="fixed w-60 overflow-y-auto rounded-lg border border-zinc-700 bg-zinc-900 p-1 shadow-2xl"
        style={{ left, top, maxHeight: height }}
      >
        <p className="truncate border-b border-zinc-800 px-3 py-2 text-xs text-zinc-500">{card.id} · {card.title}</p>
        {items.map((item) => (
          <button
            key={item.action}
            type="button"
            role="menuitem"
            onClick={() => onAction(item.action)}
            className={`flex min-h-10 w-full items-center gap-3 rounded px-3 text-left text-sm hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none ${item.action === "delete" ? "text-red-300" : "text-zinc-200"}`}
          >
            {item.icon}
            <span>{item.label}</span>
          </button>
        ))}
        <div className="mt-1 border-t border-zinc-800 pt-1">
          <p className="flex items-center gap-2 px-3 py-2 text-xs font-medium uppercase tracking-wide text-zinc-500"><Workflow size={14} /> Workflows</p>
          {workflowLoading && <p className="px-3 py-2 text-sm text-zinc-500">Loading workflows…</p>}
          {!workflowLoading && workflows.length === 0 && <p className="px-3 py-2 text-sm text-zinc-500">No eligible workflows</p>}
          {workflows.map(workflow => <button key={workflow.definition.name} type="button" role="menuitem" onClick={() => onWorkflow?.(workflow)} className="flex min-h-10 w-full items-center gap-2 rounded px-3 text-left text-sm text-zinc-200 hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none"><span className="truncate">{workflow.definition.title}</span><ChevronRight className="ml-auto shrink-0" size={15} /></button>)}
          {workflowWarning && <p className="px-3 py-2 text-xs text-amber-300">{workflowWarning}</p>}
        </div>
      </div>
    </div>
  )
}
