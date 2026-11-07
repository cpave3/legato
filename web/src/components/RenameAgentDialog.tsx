import { useState, type FormEvent } from "react"

import { useServer } from "../hooks/useServer"
import { patchTask } from "../lib/board"
import { Button } from "./ui/Button"
import { Dialog, DialogBody, DialogClose, DialogFooter, DialogHeader, DialogPanel, DialogTitle } from "./ui/Dialog"

// Names an agent by renaming its task, which is where the agent's title lives.
export function RenameAgentDialog({ taskId, title, onRenamed, onClose }: {
  taskId: string
  title: string
  onRenamed: (title: string) => void
  onClose: () => void
}) {
  const { baseUrl } = useServer()
  const [name, setName] = useState(title)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState("")
  const trimmed = name.trim()
  const canSave = trimmed !== "" && trimmed !== title && !saving

  async function submit(event: FormEvent) {
    event.preventDefault()
    if (!canSave) return
    setSaving(true)
    setError("")
    try {
      await patchTask(baseUrl, taskId, { title: trimmed })
      onRenamed(trimmed)
      onClose()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not rename the agent")
      setSaving(false)
    }
  }

  return (
    <Dialog open onClose={onClose}>
      <DialogPanel className="max-w-sm">
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>Rename agent</DialogTitle>
            <DialogClose />
          </DialogHeader>
          <DialogBody>
            {error && <div role="alert" className="rounded border border-red-800 bg-red-950/50 px-3 py-2 text-xs text-red-300">{error}</div>}
            <label className="block text-xs text-zinc-500" htmlFor="rename-agent-name">Name</label>
            <input
              id="rename-agent-name"
              value={name}
              onChange={(event) => setName(event.target.value)}
              className="w-full rounded border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm text-zinc-200 outline-none focus:border-indigo-500"
              autoFocus
              onFocus={(event) => event.currentTarget.select()}
            />
          </DialogBody>
          <DialogFooter>
            <Button type="button" variant="ghost" size="sm" onClick={onClose}>Cancel</Button>
            <Button type="submit" size="sm" disabled={!canSave}>Rename</Button>
          </DialogFooter>
        </form>
      </DialogPanel>
    </Dialog>
  )
}
