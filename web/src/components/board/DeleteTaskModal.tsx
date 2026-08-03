import { useEffect } from "react"
import { AlertTriangle } from "lucide-react"

import { Button } from "../ui/Button"
import { Dialog, DialogBody, DialogClose, DialogFooter, DialogHeader, DialogPanel, DialogTitle } from "../ui/Dialog"

interface DeleteTaskModalProps {
  open: boolean
  taskId: string
  taskTitle: string
  isRemote: boolean
  onClose: () => void
  onConfirm: () => void
  loading?: boolean
}

export function DeleteTaskModal({ open, taskId, taskTitle, isRemote, onClose, onConfirm, loading = false }: DeleteTaskModalProps) {
  useEffect(() => {
    if (!open) return
    const handler = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null
      if (target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA")) return
      if ((event.key === "y" || event.key === "Y") && !loading) {
        event.preventDefault()
        onConfirm()
      } else if (event.key === "n" || event.key === "N") {
        event.preventDefault()
        onClose()
      }
    }
    window.addEventListener("keydown", handler)
    return () => window.removeEventListener("keydown", handler)
  }, [open, onConfirm, onClose, loading])

  return (
    <Dialog open={open} onClose={onClose} dismissible={!loading}>
      <DialogPanel className="max-w-sm">
        <DialogHeader><DialogTitle>Delete {taskId}</DialogTitle><DialogClose disabled={loading} /></DialogHeader>
        <DialogBody className="space-y-3">
          <p className="text-xs text-zinc-400">{taskTitle}</p>
          {isRemote && (
            <div className="flex items-start gap-2 rounded border border-red-900/50 bg-red-950/30 px-3 py-2">
              <AlertTriangle size={14} className="mt-0.5 shrink-0 text-red-400" />
              <p className="text-xs text-red-300">This will remove the local reference only. The remote task will not be affected.</p>
            </div>
          )}
          <div className="flex items-center gap-2 text-[10px] text-zinc-600">
            <span>y = confirm</span><span>n / esc = cancel</span>
          </div>
        </DialogBody>
        <DialogFooter>
          <Button variant="outline" size="sm" disabled={loading} onClick={onClose}>Cancel</Button>
          <Button variant="destructive" size="sm" disabled={loading} onClick={onConfirm}>{loading ? "Deleting…" : "Delete"}</Button>
        </DialogFooter>
      </DialogPanel>
    </Dialog>
  )
}
