import { useState, useEffect, useRef } from "react"
import { cn } from "../../lib/utils"
import { X } from "lucide-react"

interface CreateTaskModalProps {
  open: boolean
  columns: string[]
  currentColumn: string
  workspaces: { id: number; name: string; color: string }[]
  onClose: () => void
  onSubmit: (values: {
    title: string
    description: string
    column: string
    priority: string
    workspace_id: number | null
  }) => void
}

const priorities = ["", "Low", "Medium", "High"]

export function CreateTaskModal({ open, columns, currentColumn, workspaces, onClose, onSubmit }: CreateTaskModalProps) {
  const [title, setTitle] = useState("")
  const [description, setDescription] = useState("")
  const [column, setColumn] = useState(currentColumn || (columns[0] ?? ""))
  const [priority, setPriority] = useState("")
  const [workspaceId, setWorkspaceId] = useState<number | null>(null)
  const [error, setError] = useState("")
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (!open) return
    setTitle("")
    setDescription("")
    setColumn(currentColumn || (columns[0] ?? ""))
    setPriority("")
    setWorkspaceId(null)
    setError("")
    setTimeout(() => inputRef.current?.focus(), 50)
  }, [open, columns, currentColumn])

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!title.trim()) {
      setError("Title is required")
      return
    }
    onSubmit({
      title: title.trim(),
      description: description.trim(),
      column,
      priority,
      workspace_id: workspaceId,
    })
  }

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" onClick={onClose}>
      <div className="w-full max-w-md rounded-lg border border-zinc-700 bg-zinc-900 shadow-2xl" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between border-b border-zinc-800 px-5 py-3">
          <h2 className="text-sm font-semibold text-zinc-200">New Task</h2>
          <button onClick={onClose} className="rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200">
            <X size={16} />
          </button>
        </div>
        <form onSubmit={handleSubmit} className="px-5 py-4 space-y-4">
          {error && (
            <div className="rounded border border-red-800 bg-red-950/50 px-3 py-2 text-xs text-red-300">{error}</div>
          )}
          <div className="space-y-1.5">
            <label htmlFor="create-title" className="text-xs font-medium text-zinc-400">Title</label>
            <input
              id="create-title"
              ref={inputRef}
              type="text"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              className="w-full rounded border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm text-zinc-200 outline-none focus:border-indigo-500"
              required
            />
          </div>
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-zinc-400">Column</label>
            <div className="flex flex-wrap gap-1">
              {columns.map((c) => (
                <button
                  key={c}
                  type="button"
                  onClick={() => setColumn(c)}
                  className={cn(
                    "rounded px-2 py-1 text-xs font-medium transition-colors",
                    c === column
                      ? "bg-indigo-600 text-white"
                      : "border border-zinc-700 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200"
                  )}
                >
                  {c}
                </button>
              ))}
            </div>
          </div>
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-zinc-400">Priority</label>
            <div className="flex flex-wrap gap-1">
              {priorities.map((p) => (
                <button
                  key={p || "none"}
                  type="button"
                  onClick={() => setPriority(p)}
                  className={cn(
                    "rounded px-2 py-1 text-xs font-medium transition-colors",
                    p === priority
                      ? "bg-indigo-600 text-white"
                      : "border border-zinc-700 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200"
                  )}
                >
                  {p || "none"}
                </button>
              ))}
            </div>
          </div>
          {workspaces.length > 0 && (
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-zinc-400">Workspace</label>
              <select
                value={workspaceId ?? ""}
                onChange={(e) => setWorkspaceId(e.target.value ? Number(e.target.value) : null)}
                className="w-full rounded border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm text-zinc-200 outline-none focus:border-indigo-500"
              >
                <option value="">None</option>
                {workspaces.map((ws) => (
                  <option key={ws.id} value={ws.id}>
                    {ws.name}
                  </option>
                ))}
              </select>
            </div>
          )}
          <div className="space-y-1.5">
            <label htmlFor="create-desc" className="text-xs font-medium text-zinc-400">Description</label>
            <textarea
              id="create-desc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={4}
              className="w-full rounded border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm text-zinc-200 outline-none focus:border-indigo-500"
            />
          </div>
          <div className="flex justify-end">
            <button
              type="submit"
              className="rounded bg-indigo-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-indigo-500"
            >
              Create
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
