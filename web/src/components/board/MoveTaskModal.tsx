import { useEffect, useMemo } from "react"
import { cn } from "../../lib/utils"
import { X } from "lucide-react"

interface MoveTaskModalProps {
  open: boolean
  taskId: string
  taskTitle: string
  columns: string[]
  currentColumn: string
  onClose: () => void
  onMove: (column: string) => void
  loading?: boolean
}

function buildShortcuts(columns: string[]): Map<string, string> {
  const shortcuts = new Map<string, string>()
  const used = new Set<string>()
  for (const col of columns) {
    const key = col[0]?.toLowerCase()
    if (key && !used.has(key)) {
      shortcuts.set(key, col)
      used.add(key)
    }
  }
  let num = 1
  for (const col of columns) {
    if (!Array.from(shortcuts.values()).includes(col) && num <= 9) {
      shortcuts.set(String(num), col)
      num++
    }
  }
  return shortcuts
}

export function MoveTaskModal({ open, taskId, taskTitle, columns, currentColumn, onClose, onMove, loading = false }: MoveTaskModalProps) {
  const shortcuts = useMemo(() => buildShortcuts(columns), [columns])
  const shortcutFor = (col: string): string => {
    for (const [k, v] of shortcuts) {
      if (v === col) return k
    }
    return ""
  }

  useEffect(() => {
    if (!open) return
    const handler = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement | null
      if (target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA")) return
      const col = shortcuts.get(e.key.toLowerCase())
      if (col && col !== currentColumn && !loading) {
        e.preventDefault()
        onMove(col)
      }
    }
    window.addEventListener("keydown", handler)
    return () => window.removeEventListener("keydown", handler)
  }, [open, shortcuts, currentColumn, onMove, loading])

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" onClick={onClose}>
      <div className="w-full max-w-sm rounded-lg border border-zinc-700 bg-zinc-900 shadow-2xl" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between border-b border-zinc-800 px-5 py-3">
          <h2 className="text-sm font-semibold text-zinc-200">Move {taskId}</h2>
          <button onClick={onClose} className="rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200">
            <X size={16} />
          </button>
        </div>
        {taskTitle && (
          <div className="border-b border-zinc-800 px-5 py-2 text-xs text-zinc-500">{taskTitle}</div>
        )}
        <div className="px-5 py-3 space-y-1">
          {columns.map((col) => {
            const isCurrent = col === currentColumn
            return (
              <div key={col} className="flex items-center gap-2">
                {isCurrent ? (
                  <span data-testid="current-col" className="rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] text-zinc-500">current</span>
                ) : (
                  <kbd className="min-w-[1.5rem] rounded bg-zinc-800 px-1.5 py-0.5 text-center text-[10px] font-bold text-[#AFA9EC]">
                    {shortcutFor(col)}
                  </kbd>
                )}
                <button
                  disabled={isCurrent || loading}
                  onClick={() => !isCurrent && onMove(col)}
                  className={cn(
                    "flex-1 rounded px-2 py-1 text-left text-xs transition-colors",
                    isCurrent
                      ? "text-zinc-600 cursor-not-allowed"
                      : "text-zinc-300 hover:bg-zinc-800 hover:text-zinc-100"
                  )}
                >
                  {col}
                </button>
              </div>
            )
          })}
        </div>
        <div className="border-t border-zinc-800 px-5 py-2 text-[10px] text-zinc-600">Press key to move · esc cancel</div>
      </div>
    </div>
  )
}
