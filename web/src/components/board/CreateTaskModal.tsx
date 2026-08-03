import { useState, useEffect, useRef } from "react"
import { Button } from "../ui/Button"
import { Dialog, DialogBody, DialogClose, DialogFooter, DialogHeader, DialogPanel, DialogTitle } from "../ui/Dialog"
import { InlineNotice } from "../ui/InlineNotice"

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
  loading?: boolean
}

const priorities = ["", "Low", "Medium", "High"]

export function CreateTaskModal({ open, columns, currentColumn, workspaces, onClose, onSubmit, loading = false }: CreateTaskModalProps) {
  const [title, setTitle] = useState("")
  const [description, setDescription] = useState("")
  const [column, setColumn] = useState(currentColumn || (columns[0] ?? ""))
  const [priority, setPriority] = useState("")
  const [workspaceId, setWorkspaceId] = useState<number | null>(null)
  const [error, setError] = useState("")
  const inputRef = useRef<HTMLInputElement>(null)
  const wasOpenRef = useRef(false)

  useEffect(() => {
    if (!open) {
      wasOpenRef.current = false
      return
    }
    if (wasOpenRef.current) return
    wasOpenRef.current = true
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

  return (
    <Dialog open={open} onClose={onClose} dismissible={!loading}>
      <DialogPanel>
        <DialogHeader><DialogTitle>New Task</DialogTitle><DialogClose disabled={loading} /></DialogHeader>
        <form onSubmit={handleSubmit}>
          <DialogBody className="space-y-4">
          {error && <InlineNotice variant="error" className="text-xs">{error}</InlineNotice>}
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
                <Button
                  key={c}
                  type="button"
                  variant={c === column ? "primary" : "outline"}
                  size="sm"
                  aria-pressed={c === column}
                  onClick={() => setColumn(c)}
                >
                  {c}
                </Button>
              ))}
            </div>
          </div>
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-zinc-400">Priority</label>
            <div className="flex flex-wrap gap-1">
              {priorities.map((p) => (
                <Button
                  key={p || "none"}
                  type="button"
                  variant={p === priority ? "primary" : "outline"}
                  size="sm"
                  aria-pressed={p === priority}
                  onClick={() => setPriority(p)}
                >
                  {p || "none"}
                </Button>
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
          </DialogBody>
          <DialogFooter>
            <Button type="button" variant="outline" size="sm" disabled={loading} onClick={onClose}>Cancel</Button>
            <Button type="submit" size="sm" disabled={loading}>{loading ? "Creating…" : "Create"}</Button>
          </DialogFooter>
        </form>
      </DialogPanel>
    </Dialog>
  )
}
