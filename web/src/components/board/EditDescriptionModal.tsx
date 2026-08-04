import { useEffect, useRef, useState } from "react"

import { Button } from "../ui/Button"
import { Dialog, DialogBody, DialogClose, DialogFooter, DialogHeader, DialogPanel, DialogTitle } from "../ui/Dialog"

interface EditDescriptionModalProps {
  open: boolean
  currentDescription: string
  onClose: () => void
  onSave: (description: string) => void
  providerBacked?: boolean
  loading?: boolean
}

export function EditDescriptionModal({ open, currentDescription, onClose, onSave, providerBacked = false, loading = false }: EditDescriptionModalProps) {
  const [description, setDescription] = useState(currentDescription)
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    if (!open) return
    setDescription(currentDescription)
    setTimeout(() => textareaRef.current?.focus(), 50)
  }, [open, currentDescription])

  const handleSubmit = (event: React.FormEvent) => {
    event.preventDefault()
    onSave(description)
  }

  return (
    <Dialog open={open} onClose={onClose} dismissible={!loading}>
      <DialogPanel className="flex max-h-[calc(100dvh-2rem)] max-w-2xl flex-col">
        <DialogHeader><div><DialogTitle>{providerBacked ? "Edit Legato details" : "Edit description"}</DialogTitle><p className="mt-0.5 text-xs text-zinc-500">{providerBacked ? "The provider description is read-only. These details stay in Legato and are included in task context." : "Update the task context agents and reviewers will read."}</p></div><DialogClose disabled={loading} /></DialogHeader>
        <form onSubmit={handleSubmit} className="flex min-h-0 flex-col">
          <DialogBody className="min-h-0 overflow-y-auto">
            <div className="mb-1.5 flex items-center justify-between gap-3">
              <label htmlFor="edit-task-description" className="text-xs font-medium text-zinc-400">Description</label>
              <span className="text-[10px] text-zinc-600">Markdown supported</span>
            </div>
            <textarea
              id="edit-task-description"
              ref={textareaRef}
              rows={14}
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              className="block min-h-64 w-full resize-y rounded-md border border-zinc-700 bg-zinc-950 px-3 py-2 font-mono text-sm leading-6 text-zinc-200 outline-none focus:border-indigo-500"
            />
          </DialogBody>
          <DialogFooter>
            <Button type="button" variant="outline" size="sm" disabled={loading} onClick={onClose}>Cancel</Button>
            <Button type="submit" size="sm" disabled={loading}>{loading ? "Saving…" : "Save"}</Button>
          </DialogFooter>
        </form>
      </DialogPanel>
    </Dialog>
  )
}
