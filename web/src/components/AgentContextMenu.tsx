import { useEffect, useRef } from "react"
import type { AgentInfo } from "../hooks/useWebSocket"
import { cn } from "../lib/utils"
import type { AgentMenuEntry } from "./AgentChatHeader"

const menuWidth = 240
const itemHeight = 40
const headerHeight = 36
const viewportGap = 8

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
  const left = Math.max(viewportGap, Math.min(position.x, window.innerWidth - menuWidth - viewportGap))
  const menuHeight = headerHeight + entries.length * itemHeight + 8
  const top = Math.max(viewportGap, Math.min(position.y, window.innerHeight - menuHeight - viewportGap))

  useEffect(() => {
    menuRef.current?.querySelector<HTMLButtonElement>("[role=menuitem]")?.focus()
  }, [])

  useEffect(() => {
    const keydown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault()
        onClose()
        return
      }
      if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return
      event.preventDefault()
      const items = Array.from(menuRef.current?.querySelectorAll<HTMLButtonElement>("[role=menuitem]:not(:disabled)") ?? [])
      if (items.length === 0) return
      const at = items.indexOf(document.activeElement as HTMLButtonElement)
      const next = at < 0 ? 0 : (at + (event.key === "ArrowDown" ? 1 : -1) + items.length) % items.length
      items[next].focus()
    }
    const scroll = (event: Event) => {
      const target = event.target
      if (target instanceof Node && menuRef.current?.contains(target)) return
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
  }, [onClose])

  return (
    <div
      className="fixed inset-0 z-40"
      data-testid="agent-context-backdrop"
      onPointerDown={onClose}
    >
      <div
        ref={menuRef}
        role="menu"
        aria-label={`Actions for ${agent.task_id}`}
        className="fixed w-60 rounded-lg border border-zinc-700 bg-zinc-900 p-1 shadow-2xl"
        style={{ left, top }}
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
              onClick={() => { onClose(); void entry.onSelect() }}
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
    </div>
  )
}
