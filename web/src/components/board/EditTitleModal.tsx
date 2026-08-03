import { useEffect, useRef, useState } from "react"

import { Button } from "../ui/Button"
import { Dialog, DialogBody, DialogClose, DialogFooter, DialogHeader, DialogPanel, DialogTitle } from "../ui/Dialog"

interface EditTitleModalProps {
  open: boolean
  currentTitle: string
  onClose: () => void
  onSave: (title: string) => void
  loading?: boolean
}

export function EditTitleModal({ open, currentTitle, onClose, onSave, loading = false }: EditTitleModalProps) {
  const [title, setTitle] = useState(currentTitle)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (!open) return
    setTitle(currentTitle)
    setTimeout(() => inputRef.current?.focus(), 50)
  }, [open, currentTitle])

  const handleSubmit = (event: React.FormEvent) => {
    event.preventDefault()
    if (title.trim()) onSave(title.trim())
  }

  return (
    <Dialog open={open} onClose={onClose} dismissible={!loading}>
      <DialogPanel className="max-w-sm">
        <DialogHeader><DialogTitle>Edit Title</DialogTitle><DialogClose disabled={loading} /></DialogHeader>
        <form onSubmit={handleSubmit}>
          <DialogBody className="space-y-3">
            <div className="space-y-1.5">
              <label htmlFor="edit-task-title" className="text-xs font-medium text-zinc-400">Title</label>
              <input
                id="edit-task-title"
                ref={inputRef}
                type="text"
                value={title}
                onChange={(event) => setTitle(event.target.value)}
                className="w-full rounded border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm text-zinc-200 outline-none focus:border-indigo-500"
              />
            </div>
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
