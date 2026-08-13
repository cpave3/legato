import { useEffect, useRef } from "react"
import { Skull } from "lucide-react"
import type { AgentInfo } from "../hooks/useWebSocket"

const menuWidth = 240
const menuHeight = 96
const viewportGap = 8

export function AgentContextMenu({
  agent,
  position,
  onKill,
  onClose,
}: {
  agent: AgentInfo
  position: { x: number; y: number }
  onKill: () => void
  onClose: () => void
}) {
  const menuRef = useRef<HTMLDivElement>(null)
  const left = Math.max(viewportGap, Math.min(position.x, window.innerWidth - menuWidth - viewportGap))
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
      menuRef.current?.querySelector<HTMLButtonElement>("[role=menuitem]")?.focus()
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
        <button
          type="button"
          role="menuitem"
          onClick={onKill}
          className="flex min-h-10 w-full items-center gap-3 rounded px-3 text-left text-sm text-red-300 hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none"
        >
          <Skull size={16} />
          <span>Kill agent</span>
        </button>
      </div>
    </div>
  )
}
