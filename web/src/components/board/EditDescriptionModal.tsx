import { useEffect, useRef, useState } from "react"

import { Button } from "../ui/Button"
import { Dialog, DialogBody, DialogClose, DialogFooter, DialogHeader, DialogPanel, DialogTitle } from "../ui/Dialog"

interface EditDescriptionModalProps {
  open: boolean
  currentDescription: string
  onClose: () => void
  onSave: (description: string) => void
  loading?: boolean
}

export function EditDescriptionModal({ open, currentDescription, onClose, onSave, loading = false }: EditDescriptionModalProps) {
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
      <DialogPanel className="flex h-[min(80vh,42rem)] max-w-xl flex-col">
        <DialogHeader><DialogTitle>Edit description</DialogTitle><DialogClose disabled={loading} /></DialogHeader>
        <form onSubmit={handleSubmit} className="flex min-h-0 flex-1 flex-col">
          <DialogBody className="flex min-h-0 flex-1 flex-col">
            <label htmlFor="edit-task-description" className="mb-1.5 text-xs font-medium text-zinc-400">Description</label>
            <textarea
              id="edit-task-description"
              ref={textareaRef}
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              className="min-h-48 flex-1 resize-none rounded-md border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm text-zinc-200 outline-none focus:border-indigo-500"
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
