import { useEffect } from "react"
import { X, AlertTriangle } from "lucide-react"

interface DeleteTaskModalProps {
  open: boolean
  taskId: string
  taskTitle: string
  isRemote: boolean
  onClose: () => void
  onConfirm: () => void
  loading?: boolean
}

export function DeleteTaskModal({ open, taskId, taskTitle, isRemote, onClose, onConfirm, loading = false }: DeleteTaskModalProps) {
  useEffect(() => {
    if (!open) return
    const handler = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement | null
      if (target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA")) return
      if ((e.key === "y" || e.key === "Y") && !loading) {
        e.preventDefault()
        onConfirm()
      } else if (e.key === "n" || e.key === "N") {
        e.preventDefault()
        onClose()
      }
    }
    window.addEventListener("keydown", handler)
    return () => window.removeEventListener("keydown", handler)
  }, [open, onConfirm, onClose, loading])

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" onClick={onClose}>
      <div className="w-full max-w-sm rounded-lg border border-zinc-700 bg-zinc-900 shadow-2xl" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between border-b border-zinc-800 px-5 py-3">
          <h2 className="text-sm font-semibold text-zinc-200">Delete {taskId}</h2>
          <button onClick={onClose} className="rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200">
            <X size={16} />
          </button>
        </div>
        <div className="px-5 py-4 space-y-3">
          <p className="text-xs text-zinc-400">{taskTitle}</p>
          {isRemote && (
            <div className="flex items-start gap-2 rounded border border-red-900/50 bg-red-950/30 px-3 py-2">
              <AlertTriangle size={14} className="mt-0.5 shrink-0 text-red-400" />
              <p className="text-xs text-red-300">
                This will remove the local reference only. The remote task will not be affected.
              </p>
            </div>
          )}
          <div className="flex items-center gap-2 text-[10px] text-zinc-600">
            <span>y = confirm</span>
            <span>n / esc = cancel</span>
          </div>
        </div>
        <div className="flex items-center justify-end gap-2 border-t border-zinc-800 px-5 py-3">
          <button
            onClick={onClose}
            className="rounded border border-zinc-700 px-3 py-1.5 text-xs text-zinc-400 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
          >
            Cancel
          </button>
          <button
            onClick={onConfirm}
            disabled={loading}
            className="rounded bg-red-600 px-3 py-1.5 text-xs text-white transition-colors hover:bg-red-500 disabled:cursor-wait disabled:opacity-50"
          >
            {loading ? "Deleting…" : "Delete"}
          </button>
        </div>
      </div>
    </div>
  )
}
