import { useState, useEffect, useRef } from "react"
import { useServer } from "../hooks/useServer"
import { useToast } from "../hooks/useToast"
import { apiFetch } from "../lib/api"
import { startSwarm } from "../lib/swarm"
import { cn } from "../lib/utils"
import { X, Play } from "lucide-react"

interface ParentCard {
  key: string
  title: string
  status: string
}

export function StartSwarmModal({
  open,
  onClose,
  preSelectedParentId,
  onStarted,
}: {
  open: boolean
  onClose: () => void
  preSelectedParentId?: string
  onStarted?: () => void
}) {
  const { baseUrl } = useServer()
  const { addToast } = useToast()
  const [parentId, setParentId] = useState(preSelectedParentId ?? "")
  const [workingDir, setWorkingDir] = useState("")
  const [cards, setCards] = useState<ParentCard[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState("")
  const inputRef = useRef<HTMLInputElement>(null)

  // Fetch tasks from /api/tasks for the picker.
  useEffect(() => {
    if (!open) return
    setError("")
    setLoading(false)
    setParentId(preSelectedParentId ?? "")
    setWorkingDir("")

    // Fetch board cards.
    apiFetch(baseUrl, "/api/tasks")
      .then(async (res) => {
        if (!res.ok) return
        const data: Record<string, { key: string; title: string; status: string }[]> = await res.json()
        const allCards: ParentCard[] = []
        for (const col of Object.values(data)) {
          for (const c of col) {
            allCards.push(c)
          }
        }
        setCards(allCards)
      })
      .catch(() => {
        // ignore fetch errors
      })

    setTimeout(() => inputRef.current?.focus(), 50)
  }, [open, preSelectedParentId, baseUrl])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!parentId.trim() || !workingDir.trim()) return
    setLoading(true)
    setError("")
    try {
      await startSwarm(baseUrl, parentId.trim(), workingDir.trim())
      addToast("Swarm started", "success")
      onStarted?.()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to start swarm")
    } finally {
      setLoading(false)
    }
  }

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div className="w-full max-w-md rounded-lg border border-zinc-700 bg-zinc-900 shadow-2xl">
        <div className="flex items-center justify-between border-b border-zinc-800 px-5 py-3">
          <h2 className="text-sm font-semibold text-zinc-200">Start Swarm</h2>
          <button
            onClick={onClose}
            className="rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
          >
            <X size={16} />
          </button>
        </div>

        <form onSubmit={handleSubmit} className="px-5 py-4 space-y-4">
          {error && (
            <div className="rounded border border-red-800 bg-red-950/50 px-3 py-2 text-xs text-red-300">
              {error}
            </div>
          )}

          <div className="space-y-1.5">
            <label className="text-xs font-medium text-zinc-400">Parent Task</label>
            {preSelectedParentId ? (
              <div className="rounded border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm font-mono text-zinc-300">
                {preSelectedParentId}
              </div>
            ) : (
              <select
                value={parentId}
                onChange={(e) => setParentId(e.target.value)}
                className="w-full rounded border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm text-zinc-200 outline-none focus:border-indigo-500"
                required
              >
                <option value="">Select parent task...</option>
                {cards.map((c) => (
                  <option key={c.key} value={c.key}>
                    {c.key} — {c.title}
                  </option>
                ))}
              </select>
            )}
          </div>

          <div className="space-y-1.5">
            <label className="text-xs font-medium text-zinc-400">Working Directory</label>
            <input
              ref={inputRef}
              type="text"
              value={workingDir}
              onChange={(e) => setWorkingDir(e.target.value)}
              placeholder="/path/to/project"
              className="w-full rounded border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm text-zinc-200 outline-none focus:border-indigo-500 font-mono"
              required
            />
          </div>

          <div className="flex justify-end">
            <button
              type="submit"
              disabled={loading || !parentId.trim() || !workingDir.trim()}
              className={cn(
                "flex items-center gap-1.5 rounded bg-indigo-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-indigo-500 disabled:opacity-40"
              )}
            >
              <Play size={14} />
              {loading ? "Starting..." : "Start Swarm"}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
