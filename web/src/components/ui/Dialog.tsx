import { X } from "lucide-react"
import {
  createContext,
  type HTMLAttributes,
  type ReactNode,
  useContext,
  useId,
  useRef,
} from "react"

import { cn } from "../../lib/utils"
import { Button } from "./Button"
import { useModalLayer } from "../../hooks/useModalLayer"

interface DialogContextValue {
  onClose: () => void
  titleId: string
}

const DialogContext = createContext<DialogContextValue | null>(null)

function useDialogContext() {
  const context = useContext(DialogContext)
  if (!context) throw new Error("Dialog components must be rendered inside Dialog")
  return context
}

interface DialogProps {
  open: boolean
  onClose: () => void
  children: ReactNode
  dismissible?: boolean
  // Classes for the backdrop, such as less padding on a phone.
  className?: string
}

export function Dialog({ open, onClose, children, dismissible = true, className }: DialogProps) {
  const titleId = useId()
  const containerRef = useRef<HTMLDivElement>(null)
  useModalLayer({ open, onClose, containerRef, dismissible })

  if (!open) return null

  return (
    <DialogContext.Provider value={{ onClose, titleId }}>
      <div
        ref={containerRef}
        data-testid="dialog-backdrop"
        className={cn("fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4", className)}
        onMouseDown={(event) => {
          if (dismissible && event.target === event.currentTarget) onClose()
        }}
      >
        {children}
      </div>
    </DialogContext.Provider>
  )
}

export function DialogPanel({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  const { titleId } = useDialogContext()
  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      className={cn("flex max-h-[calc(100dvh-2rem)] w-full max-w-md flex-col overflow-hidden rounded-lg border border-zinc-700 bg-zinc-900 shadow-2xl", className)}
      {...props}
    />
  )
}

export function DialogHeader({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("flex shrink-0 items-center justify-between gap-4 border-b border-zinc-800 px-5 py-3", className)} {...props} />
}

export function DialogTitle({ className, ...props }: HTMLAttributes<HTMLHeadingElement>) {
  const { titleId } = useDialogContext()
  return <h2 id={titleId} className={cn("text-sm font-semibold text-zinc-200", className)} {...props} />
}

export function DialogClose({ className, ...props }: Omit<React.ComponentProps<typeof Button>, "children">) {
  const { onClose } = useDialogContext()
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-sm"
      aria-label="Close dialog"
      onClick={onClose}
      className={className}
      {...props}
    >
      <X aria-hidden="true" className="size-4" />
    </Button>
  )
}

export function DialogBody({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("min-h-0 flex-1 space-y-4 overflow-y-auto overscroll-contain px-5 py-4", className)} {...props} />
}

export function DialogFooter({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("flex shrink-0 flex-wrap items-center justify-end gap-2 border-t border-zinc-800 px-5 py-3", className)} {...props} />
}
