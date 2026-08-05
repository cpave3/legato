import { useState, useEffect, useRef } from "react"
import { VoiceRecorder } from "../VoiceRecorder"
import { Button } from "../ui/Button"
import { Dialog, DialogBody, DialogClose, DialogFooter, DialogHeader, DialogPanel, DialogTitle } from "../ui/Dialog"
import { InlineNotice } from "../ui/InlineNotice"
import { useTextareaTranscription } from "./useTextareaTranscription"

interface CreateTaskModalProps {
  open: boolean
  columns: string[]
  currentColumn: string
  workspaces: { id: number; name: string; color: string }[]
  baseUrl: string
  voiceEnabled: boolean
  defaultWorkspaceId?: number | null
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

const priorities = [
  { value: "", label: "No priority" },
  { value: "Low", label: "Low" },
  { value: "Medium", label: "Medium" },
  { value: "High", label: "High" },
]

export function CreateTaskModal({ open, columns, currentColumn, workspaces, baseUrl, voiceEnabled, defaultWorkspaceId = null, onClose, onSubmit, loading = false }: CreateTaskModalProps) {
  const [title, setTitle] = useState("")
  const [description, setDescription] = useState("")
  const { textareaRef, rememberSelection, insertTranscription } = useTextareaTranscription(setDescription)
  const [column, setColumn] = useState(currentColumn || (columns[0] ?? ""))
  const [priority, setPriority] = useState("")
  const [workspaceId, setWorkspaceId] = useState<number | null>(defaultWorkspaceId)
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
    setWorkspaceId(defaultWorkspaceId)
    setError("")
    setTimeout(() => inputRef.current?.focus(), 50)
  }, [open, columns, currentColumn, defaultWorkspaceId])

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
      <DialogPanel className="max-h-[calc(100dvh-2rem)] max-w-2xl">
        <DialogHeader><div><DialogTitle>New Task</DialogTitle><p className="mt-0.5 text-xs text-zinc-500">Capture the work now; placement can change later.</p></div><DialogClose disabled={loading} /></DialogHeader>
        <form onSubmit={handleSubmit}>
          <DialogBody className="max-h-[calc(100dvh-9rem)] overflow-y-auto">
            {error && <InlineNotice variant="error" className="mb-4 text-xs">{error}</InlineNotice>}
            <div data-create-task-layout className="grid gap-6 md:grid-cols-[minmax(0,1fr)_15rem]">
              <fieldset className="min-w-0 space-y-4">
                <legend className="sr-only">Task details</legend>
                <div className="space-y-1.5">
                  <label htmlFor="create-title" className="text-xs font-medium text-zinc-300">Title <span className="text-red-400">*</span></label>
                  <input
                    id="create-title"
                    ref={inputRef}
                    type="text"
                    value={title}
                    onChange={(event) => setTitle(event.target.value)}
                    placeholder="What needs to be done?"
                    className="min-h-11 w-full rounded-md border border-zinc-700 bg-zinc-950 px-3 text-sm text-zinc-100 outline-none placeholder:text-zinc-600 focus:border-indigo-500"
                    required
                  />
                </div>
                <div className="space-y-1.5">
                  <div className="flex items-center justify-between gap-3">
                    <label htmlFor="create-desc" className="text-xs font-medium text-zinc-300">Description</label>
                    <div className="flex items-center gap-2">
                      <span className="text-[10px] text-zinc-600">Markdown supported</span>
                      {voiceEnabled && <VoiceRecorder baseUrl={baseUrl} transcriptionOnly onTranscription={insertTranscription} />}
                    </div>
                  </div>
                  <textarea
                    id="create-desc"
                    ref={textareaRef}
                    value={description}
                    onChange={(event) => setDescription(event.target.value)}
                    onSelect={rememberSelection}
                    onBlur={rememberSelection}
                    rows={10}
                    placeholder="Add context, constraints, and acceptance criteria…"
                    className="block min-h-56 w-full resize-y rounded-md border border-zinc-700 bg-zinc-950 px-3 py-2 font-mono text-sm leading-6 text-zinc-200 outline-none placeholder:text-zinc-600 focus:border-indigo-500"
                  />
                </div>
              </fieldset>

              <fieldset className="space-y-4 rounded-lg border border-zinc-800 bg-zinc-950/40 p-4 md:self-start">
                <legend className="sr-only">Placement</legend>
                <div>
                  <h3 className="text-xs font-semibold text-zinc-300">Placement</h3>
                  <p className="mt-1 text-[11px] leading-4 text-zinc-600">Choose where the task starts.</p>
                </div>
                <label className="block text-xs font-medium text-zinc-400">Column
                  <select aria-label="Column" value={column} onChange={(event) => setColumn(event.target.value)} className="mt-1 min-h-11 w-full rounded-md border border-zinc-700 bg-zinc-950 px-3 text-sm text-zinc-200 outline-none focus:border-indigo-500">
                    {columns.map((value) => <option key={value} value={value}>{value}</option>)}
                  </select>
                </label>
                <label className="block text-xs font-medium text-zinc-400">Priority
                  <select aria-label="Priority" value={priority} onChange={(event) => setPriority(event.target.value)} className="mt-1 min-h-11 w-full rounded-md border border-zinc-700 bg-zinc-950 px-3 text-sm text-zinc-200 outline-none focus:border-indigo-500">
                    {priorities.map((option) => <option key={option.value || "none"} value={option.value}>{option.label}</option>)}
                  </select>
                </label>
                {workspaces.length > 0 && <label className="block text-xs font-medium text-zinc-400">Workspace
                  <select aria-label="Workspace" value={workspaceId ?? ""} onChange={(event) => setWorkspaceId(event.target.value ? Number(event.target.value) : null)} className="mt-1 min-h-11 w-full rounded-md border border-zinc-700 bg-zinc-950 px-3 text-sm text-zinc-200 outline-none focus:border-indigo-500">
                    <option value="">No workspace</option>
                    {workspaces.map((workspace) => <option key={workspace.id} value={workspace.id}>{workspace.name}</option>)}
                  </select>
                </label>}
              </fieldset>
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
