import { useEffect, useState } from "react"
import { ArchiveRestore, X } from "lucide-react"
import { useServer } from "../../hooks/useServer"
import { fetchArchivedTasks, restoreTask } from "../../lib/board"
import type { BoardCard } from "../../lib/board-types"

export function ArchivedTasksModal({ open, onClose, onRestored }: { open: boolean; onClose: () => void; onRestored: () => void }) {
  const { baseUrl } = useServer()
  const [cards, setCards] = useState<BoardCard[]>([])
  const [busy, setBusy] = useState("")
  useEffect(() => {
    if (open) fetchArchivedTasks(baseUrl).then(setCards).catch(() => setCards([]))
  }, [open, baseUrl])
  if (!open) return null
  const restore = async (id: string) => {
    setBusy(id)
    try {
      await restoreTask(baseUrl, id)
      setCards((current) => current.filter((card) => card.id !== id))
      onRestored()
    } finally {
      setBusy("")
    }
  }
  return <div className="fixed inset-0 z-50 flex items-start justify-center bg-black/60 p-4 pt-20" onClick={onClose}>
    <div className="w-full max-w-xl rounded-lg border border-zinc-700 bg-zinc-900 shadow-2xl" onClick={(e) => e.stopPropagation()}>
      <div className="flex items-center justify-between border-b border-zinc-800 px-4 py-3">
        <h2 className="text-sm font-semibold text-zinc-100">Archived tasks</h2>
        <button onClick={onClose} className="text-zinc-500 hover:text-zinc-200"><X size={16}/></button>
      </div>
      <div className="max-h-96 overflow-y-auto p-2">
        {cards.length === 0 && <div className="p-8 text-center text-sm text-zinc-500">No archived tasks</div>}
        {cards.map((card) => <div key={card.id} className="flex items-center gap-3 rounded px-3 py-2 hover:bg-zinc-800">
          <div className="min-w-0 flex-1"><div className="truncate text-sm text-zinc-200">{card.title}</div><div className="text-xs text-zinc-500">{card.id} · {card.status}</div></div>
          <button disabled={busy === card.id} onClick={() => restore(card.id)} className="flex items-center gap-1 rounded bg-indigo-600 px-2 py-1 text-xs text-white disabled:opacity-50"><ArchiveRestore size={13}/>Restore</button>
        </div>)}
      </div>
    </div>
  </div>
}
