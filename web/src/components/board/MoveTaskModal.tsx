import { useEffect, useMemo } from "react"
import { cn } from "../../lib/utils"
import type { Workspace } from "../../lib/board-types"
import { X } from "lucide-react"

interface MoveTaskModalProps {
  open: boolean
  taskId: string
  taskTitle: string
  columns: string[]
  currentColumn: string
  workspaces: Workspace[]
  currentWorkspaceId: number | null
  onClose: () => void
  onMoveColumn: (column: string) => void
  onMoveWorkspace: (workspaceId: number | null) => void
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

export function MoveTaskModal({
  open,
  taskId,
  taskTitle,
  columns,
  currentColumn,
  workspaces,
  currentWorkspaceId,
  onClose,
  onMoveColumn,
  onMoveWorkspace,
  loading = false,
}: MoveTaskModalProps) {
  const shortcuts = useMemo(() => buildShortcuts(columns), [columns])
  const shortcutFor = (col: string): string => {
    for (const [key, value] of shortcuts) {
      if (value === col) return key
    }
    return ""
  }

  useEffect(() => {
    if (!open) return
    const handler = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null
      if (target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA")) return
      const column = shortcuts.get(event.key.toLowerCase())
      if (column && column !== currentColumn && !loading) {
        event.preventDefault()
        onMoveColumn(column)
      }
    }
    window.addEventListener("keydown", handler)
    return () => window.removeEventListener("keydown", handler)
  }, [open, shortcuts, currentColumn, onMoveColumn, loading])

  if (!open) return null

  const destinationButton = (label: string, current: boolean, onClick: () => void, shortcut = "") => (
    <div key={label} className="flex items-center gap-2">
      {current ? (
        <span className="rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] text-zinc-500">current</span>
      ) : shortcut ? (
        <kbd className="min-w-[1.5rem] rounded bg-zinc-800 px-1.5 py-0.5 text-center text-[10px] font-bold text-[#AFA9EC]">{shortcut}</kbd>
      ) : (
        <span className="min-w-[1.5rem]" />
      )}
      <button
        disabled={current || loading}
        onClick={onClick}
        className={cn(
          "flex-1 rounded px-2 py-1 text-left text-xs transition-colors",
          current ? "cursor-not-allowed text-zinc-600" : "text-zinc-300 hover:bg-zinc-800 hover:text-zinc-100",
        )}
      >
        {label}{current && <span className="sr-only"> current</span>}
      </button>
    </div>
  )

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" onClick={() => !loading && onClose()}>
      <div role="dialog" aria-modal="true" aria-label={`Move ${taskId}`} className="w-full max-w-sm rounded-lg border border-zinc-700 bg-zinc-900 shadow-2xl" onClick={(event) => event.stopPropagation()}>
        <div className="flex items-center justify-between border-b border-zinc-800 px-5 py-3">
          <h2 className="text-sm font-semibold text-zinc-200">Move {taskId}</h2>
          <button aria-label="Close" disabled={loading} onClick={onClose} className="rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200 disabled:cursor-not-allowed">
            <X size={16} />
          </button>
        </div>
        {taskTitle && <div className="border-b border-zinc-800 px-5 py-2 text-xs text-zinc-500">{taskTitle}</div>}
        <div className="max-h-[70vh] space-y-4 overflow-y-auto px-5 py-3">
          <section className="space-y-1">
            <h3 className="text-[10px] font-bold uppercase tracking-wider text-zinc-500">Column</h3>
            {columns.map((column) => destinationButton(column, column === currentColumn, () => onMoveColumn(column), shortcutFor(column)))}
          </section>
          <section className="space-y-1">
            <h3 className="text-[10px] font-bold uppercase tracking-wider text-zinc-500">Workspace</h3>
            {destinationButton("Unassigned", currentWorkspaceId === null, () => onMoveWorkspace(null))}
            {workspaces.map((workspace) => destinationButton(workspace.name, workspace.id === currentWorkspaceId, () => onMoveWorkspace(workspace.id)))}
          </section>
        </div>
        <div className="border-t border-zinc-800 px-5 py-2 text-[10px] text-zinc-600">Press column key to move · esc cancel</div>
      </div>
    </div>
  )
}
