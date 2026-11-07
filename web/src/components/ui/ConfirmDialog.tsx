import type { ReactNode } from "react"

import { Button } from "./Button"
import { Dialog, DialogBody, DialogFooter, DialogHeader, DialogPanel, DialogTitle } from "./Dialog"

interface ConfirmDialogProps {
  title: string
  message: ReactNode
  confirmLabel?: string
  destructive?: boolean
  onConfirm: () => void
  onCancel: () => void
}

// A small modal that asks before something that is hard to undo.
export function ConfirmDialog({ title, message, confirmLabel = "Confirm", destructive = false, onConfirm, onCancel }: ConfirmDialogProps) {
  return (
    <Dialog open onClose={onCancel}>
      <DialogPanel className="max-w-sm">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        <DialogBody>
          <p className="text-sm text-zinc-300">{message}</p>
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="ghost" size="sm" onClick={onCancel}>Cancel</Button>
          <Button type="button" variant={destructive ? "destructive" : "primary"} size="sm" onClick={onConfirm}>{confirmLabel}</Button>
        </DialogFooter>
      </DialogPanel>
    </Dialog>
  )
}
