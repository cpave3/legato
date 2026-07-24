import { useEffect, useState } from "react"
import { X } from "lucide-react"
import { useServer } from "../../hooks/useServer"
import { cancelSwarm } from "../../lib/swarm"

interface CancelSwarmModalProps {
  taskId: string
  taskTitle: string
  isOpen: boolean
  onClose: () => void
  onConfirm: () => void
}

export function CancelSwarmModal({ taskId, taskTitle, isOpen, onClose, onConfirm }: CancelSwarmModalProps) {
  const { baseUrl } = useServer()
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState("")

  useEffect(() => {
    if (!isOpen) {
      setLoading(false)
      setError("")
    }
  }, [isOpen])

  useEffect(() => {
    if (!isOpen) return
    const handler = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement | null
      if (target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA")) return
      if (e.key === "y" || e.key === "Y") {
        e.preventDefault()
        handleConfirm()
      } else if (e.key === "n" || e.key === "N") {
        e.preventDefault()
        onClose()
      }
    }
    window.addEventListener("keydown", handler)
    return () => window.removeEventListener("keydown", handler)
  }, [isOpen, onClose])

  const handleConfirm = async () => {
    if (loading) return
    setLoading(true)
    setError("")
    try {
      await cancelSwarm(baseUrl, taskId)
      onConfirm()
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to cancel swarm")
    } finally {
      setLoading(false)
    }
  }

  if (!isOpen) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" onClick={onClose}>
      <div className="w-full max-w-sm rounded-lg border border-zinc-700 bg-zinc-900 shadow-2xl" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between border-b border-zinc-800 px-5 py-3">
          <h2 className="text-sm font-semibold text-zinc-200">Cancel swarm for {taskTitle}?</h2>
          <button onClick={onClose} className="rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200">
            <X size={16} />
          </button>
        </div>
        <div className="px-5 py-4">
          {error && (
            <div role="alert" className="mb-3 rounded border border-red-800 bg-red-950/50 px-3 py-2 text-xs text-red-300">
              {error}
            </div>
          )}
          <p className="text-xs text-zinc-400">
            Kills the conductor + all live workers, deletes all sub-tasks, and clears the swarm state on this card.
          </p>
          <div className="mt-3 text-[10px] text-zinc-600">y = confirm · n/esc = cancel</div>
        </div>
        <div className="flex items-center justify-end gap-2 border-t border-zinc-800 px-5 py-3">
          <button
            onClick={onClose}
            disabled={loading}
            className="rounded border border-zinc-700 px-3 py-1.5 text-xs text-zinc-400 transition-colors hover:bg-zinc-800 hover:text-zinc-200 disabled:opacity-40"
          >
            Cancel
          </button>
          <button
            onClick={handleConfirm}
            disabled={loading}
            className="rounded bg-red-600 px-3 py-1.5 text-xs text-white transition-colors hover:bg-red-500 disabled:opacity-40"
          >
            {loading ? "Cancelling..." : "Confirm"}
          </button>
        </div>
      </div>
    </div>
  )
}
