import { useEffect } from "react"
import { X } from "lucide-react"

interface ArchiveDoneModalProps {
  open: boolean
  count: number
  onClose: () => void
  onConfirm: () => void
}

export function ArchiveDoneModal({ open, count, onClose, onConfirm }: ArchiveDoneModalProps) {
  useEffect(() => {
    if (!open) return
    const handler = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement | null
      if (target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA")) return
      if (e.key === "y" || e.key === "Y") {
        e.preventDefault()
        onConfirm()
      } else if (e.key === "n" || e.key === "N") {
        e.preventDefault()
        onClose()
      }
    }
    window.addEventListener("keydown", handler)
    return () => window.removeEventListener("keydown", handler)
  }, [open, onConfirm, onClose])

  if (!open) return null

  const cardWord = count === 1 ? "card" : "cards"

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" onClick={onClose}>
      <div className="w-full max-w-sm rounded-lg border border-zinc-700 bg-zinc-900 shadow-2xl" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between border-b border-zinc-800 px-5 py-3">
          <h2 className="text-sm font-semibold text-zinc-200">Archive {count} done {cardWord}?</h2>
          <button onClick={onClose} className="rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200">
            <X size={16} />
          </button>
        </div>
        <div className="px-5 py-4">
          <p className="text-xs text-zinc-400">Archived cards are hidden from the board but kept in the database.</p>
          <div className="mt-3 text-[10px] text-zinc-600">y = confirm · n/esc = cancel</div>
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
            className="rounded bg-amber-600 px-3 py-1.5 text-xs text-white transition-colors hover:bg-amber-500"
          >
            Archive
          </button>
        </div>
      </div>
    </div>
  )
}
