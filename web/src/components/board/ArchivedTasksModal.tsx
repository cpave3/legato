import { Dialog, DialogPanel, DialogHeader, DialogTitle, DialogBody } from "../ui/Dialog"
import { useEffect, useState } from "react"
import { ArchiveRestore, X } from "lucide-react"
import { useServer } from "../../hooks/useServer"
import { fetchArchivedTasks, restoreTask } from "../../lib/board"
import type { BoardCard } from "../../lib/board-types"

export function ArchivedTasksModal({ open, onClose, onRestored }: { open: boolean; onClose: () => void; onRestored: () => void }) {
  const { baseUrl } = useServer()
  const [cards, setCards] = useState<BoardCard[]>([])
  const [busy, setBusy] = useState("")
  const [error, setError] = useState("")
  const [loading, setLoading] = useState(false)
  useEffect(() => {
    if (!open) return
    let current = true
    setLoading(true)
    setError("")
    fetchArchivedTasks(baseUrl)
      .then(cards => { if (current) setCards(cards) })
      .catch(reason => { if (current) setError(reason instanceof Error ? reason.message : "Could not load archived tasks") })
      .finally(() => { if (current) setLoading(false) })
    return () => { current = false }
  }, [open, baseUrl])
  if (!open) return null
  const restore = async (id: string) => {
    setBusy(id)
    setError("")
    try {
      await restoreTask(baseUrl, id)
      setCards((current) => current.filter((card) => card.id !== id))
      onRestored()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Could not restore task")
    } finally {
      setBusy("")
    }
  }
  return <Dialog open={open} onClose={onClose}>
    <DialogPanel className="w-full max-w-xl rounded-lg border border-zinc-700 bg-zinc-900 shadow-2xl">
      <DialogHeader className="flex items-center justify-between border-b border-zinc-800 px-4 py-3">
        <DialogTitle className="text-sm font-semibold text-zinc-100">Archived tasks</DialogTitle>
        <button aria-label="Close" onClick={onClose} className="text-zinc-500 hover:text-zinc-200"><X size={16}/></button>
      </DialogHeader>
      <DialogBody className="space-y-0 max-h-96 overflow-y-auto p-2">
        {error && <p role="alert" className="p-3 text-sm text-red-300">{error}</p>}
        {loading && <p role="status" className="p-3 text-sm text-zinc-400">Loading archived tasks…</p>}
        {!loading && !error && cards.length === 0 && <div className="p-8 text-center text-sm text-zinc-500">No archived tasks</div>}
        {cards.map((card) => <div key={card.id} className="flex items-center gap-3 rounded px-3 py-2 hover:bg-zinc-800">
          <div className="min-w-0 flex-1"><div className="truncate text-sm text-zinc-200">{card.title}</div><div className="text-xs text-zinc-500">{card.id} · {card.status}</div></div>
          <button disabled={busy === card.id} onClick={() => restore(card.id)} className="flex items-center gap-1 rounded bg-indigo-600 px-2 py-1 text-xs text-white disabled:opacity-50"><ArchiveRestore size={13}/>Restore</button>
        </div>)}
      </DialogBody>
    </DialogPanel>
  </Dialog>
}
