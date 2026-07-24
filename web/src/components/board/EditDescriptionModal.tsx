import { useState, useEffect, useRef } from "react"

import { X } from "lucide-react"

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

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    onSave(description)
  }

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" onClick={onClose}>
      <div className="flex h-[80vh] w-full max-w-xl flex-col rounded-lg border border-zinc-700 bg-zinc-900 shadow-2xl" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between border-b border-zinc-800 px-5 py-3">
          <h2 className="text-sm font-semibold text-zinc-200">Edit Description</h2>
          <button onClick={onClose} className="rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200">
            <X size={16} />
          </button>
        </div>
        <form onSubmit={handleSubmit} className="flex flex-1 flex-col px-5 py-3">
          <textarea
            ref={textareaRef}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            className="flex-1 resize-none rounded border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm text-zinc-200 outline-none focus:border-indigo-500"
          />
          <div className="mt-3 flex items-center justify-end gap-2">
            <button
              type="button"
              onClick={onClose}
              className="rounded border border-zinc-700 px-3 py-1.5 text-xs text-zinc-400 hover:bg-zinc-800"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={loading}
              className="rounded bg-indigo-600 px-3 py-1.5 text-xs text-white hover:bg-indigo-500 disabled:cursor-wait disabled:opacity-50"
            >
              {loading ? "Saving…" : "Save"}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
