import { useState, useEffect, useRef } from "react"
import { useServer } from "../hooks/useServer"
import { useSettings } from "../hooks/useSettings"
import { useToast } from "../hooks/useToast"
import { apiFetch } from "../lib/api"
import { startSwarm } from "../lib/swarm"
import { cn } from "../lib/utils"
import { RepositoryPresetSelect, type RepositoryPreset } from "./RepositoryPresetSelect"
import { Play } from "lucide-react"
import { Dialog, DialogBody, DialogClose, DialogHeader, DialogPanel, DialogTitle } from "./ui/Dialog"

interface ParentCard {
  key: string
  title: string
  status: string
}

export function StartSwarmModal({
  open,
  onClose,
  preSelectedParentId,
  initialWorkingDir = "",
  onStarted,
}: {
  open: boolean
  onClose: () => void
  preSelectedParentId?: string
  initialWorkingDir?: string
  onStarted?: () => void
}) {
  const { baseUrl } = useServer()
  const settings = useSettings()
  const { addToast } = useToast()
  const [parentId, setParentId] = useState(preSelectedParentId ?? "")
  const [workingDir, setWorkingDir] = useState("")
  const [repositoryPreset, setRepositoryPreset] = useState("")
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
    setWorkingDir(initialWorkingDir)
    setRepositoryPreset("")

    if (!preSelectedParentId) {
      apiFetch(baseUrl, "/api/tasks")
        .then(async (res) => {
          if (!res.ok) return
          const data: Record<string, { key: string; title: string; status: string }[]> = await res.json()
          const allCards: ParentCard[] = []
          for (const col of Object.values(data)) {
            for (const c of col) allCards.push(c)
          }
          setCards(allCards)
        })
        .catch(() => {
          // Keep the picker empty while the server reconnects.
        })
    }

    setTimeout(() => inputRef.current?.focus(), 50)
  }, [open, preSelectedParentId, initialWorkingDir, baseUrl])

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

  return (
    <Dialog open={open} onClose={onClose} dismissible={!loading}>
      <DialogPanel>
        <DialogHeader>
          <DialogTitle>Start Swarm</DialogTitle>
          <DialogClose disabled={loading} />
        </DialogHeader>

        <form onSubmit={handleSubmit}>
          <DialogBody>
          {error && (
            <div className="rounded border border-red-800 bg-red-950/50 px-3 py-2 text-xs text-red-300">
              {error}
            </div>
          )}

          <div className="space-y-1.5">
            <label htmlFor="swarm-parent-task" className="text-xs font-medium text-zinc-400">Parent Task</label>
            {preSelectedParentId ? (
              <div id="swarm-parent-task" className="rounded border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm font-mono text-zinc-300">
                {preSelectedParentId}
              </div>
            ) : (
              <select
                id="swarm-parent-task"
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

          <RepositoryPresetSelect
            repositories={(settings.data?.repositories ?? []) as RepositoryPreset[]}
            value={repositoryPreset}
            onChange={(name, repository) => {
              setRepositoryPreset(name)
              if (repository) setWorkingDir(repository.primary_dir)
            }}
          />

          <div className="space-y-1.5">
            <label htmlFor="swarm-working-directory" className="text-xs font-medium text-zinc-400">Working Directory</label>
            <input
              id="swarm-working-directory"
              ref={inputRef}
              type="text"
              value={workingDir}
              onChange={(e) => {
                setWorkingDir(e.target.value)
                setRepositoryPreset("")
              }}
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
          </DialogBody>
        </form>
      </DialogPanel>
    </Dialog>
  )
}
