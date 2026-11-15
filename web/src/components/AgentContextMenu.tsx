import { createPortal } from "react-dom"
import { useMenuLayer } from "../hooks/useMenuLayer"
import { useRef } from "react"
import type { AgentInfo } from "../hooks/useWebSocket"
import { cn } from "../lib/utils"
import type { AgentMenuEntry } from "./AgentChatHeader"

export function AgentContextMenu({
  agent,
  position,
  entries,
  onClose,
}: {
  agent: AgentInfo
  position: { x: number; y: number }
  // The same entries as the agent's More actions menu.
  entries: AgentMenuEntry[]
  onClose: () => void
}) {
  const menuRef = useRef<HTMLDivElement>(null)
  const root = useRef<HTMLDivElement>(null)
  const { restoreFocus } = useMenuLayer({ open: true, onClose, containerRef: menuRef, outsideRef: root, position })

  return createPortal(
    <div
      ref={root}
      className="fixed inset-0 z-40"
      data-testid="agent-context-backdrop"
      onPointerDown={onClose}
    >
      <div
        ref={menuRef}
        role="menu"
        aria-label={`Actions for ${agent.task_id}`}
        className="fixed w-60 rounded-lg border border-zinc-700 bg-zinc-900 p-1 shadow-2xl"
        onPointerDown={(event) => event.stopPropagation()}
      >
        <p className="truncate border-b border-zinc-800 px-3 py-2 text-xs text-zinc-500">
          {agent.task_id}{agent.task_title ? ` · ${agent.task_title}` : ""}
        </p>
        {entries.map((entry) => (
          <div key={entry.label}>
            {entry.danger && <div className="my-1 border-t border-zinc-800" />}
            <button
              type="button"
              role="menuitem"
              disabled={entry.disabled}
              onClick={() => { restoreFocus(); onClose(); void entry.onSelect() }}
              className={cn(
                "flex min-h-10 w-full items-center gap-3 rounded px-3 text-left text-sm hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none disabled:opacity-50",
                entry.danger ? "text-red-300" : "text-zinc-200",
              )}
            >
              {entry.icon}
              <span>{entry.label}</span>
            </button>
          </div>
        ))}
      </div>
    </div>, document.body
  )
}
