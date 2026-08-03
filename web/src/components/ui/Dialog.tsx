import { X } from "lucide-react"
import {
  createContext,
  type HTMLAttributes,
  type ReactNode,
  useContext,
  useEffect,
  useId,
  useRef,
} from "react"

import { cn } from "../../lib/utils"
import { Button } from "./Button"

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
}

const focusableSelector = [
  "button:not([disabled])",
  "a[href]",
  "input:not([disabled])",
  "select:not([disabled])",
  "textarea:not([disabled])",
  '[tabindex]:not([tabindex="-1"])',
].join(",")

export function Dialog({ open, onClose, children, dismissible = true }: DialogProps) {
  const titleId = useId()
  const containerRef = useRef<HTMLDivElement>(null)
  const restoreFocusRef = useRef<HTMLElement | null>(null)
  const onCloseRef = useRef(onClose)
  const dismissibleRef = useRef(dismissible)

  useEffect(() => {
    onCloseRef.current = onClose
    dismissibleRef.current = dismissible
  }, [dismissible, onClose])

  useEffect(() => {
    if (!open) return

    restoreFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const container = containerRef.current
    const focusable = Array.from(container?.querySelectorAll<HTMLElement>(focusableSelector) ?? [])
    focusable[0]?.focus()

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape" && dismissibleRef.current) {
        event.preventDefault()
        onCloseRef.current()
        return
      }
      if (event.key !== "Tab") return

      const elements = Array.from(container?.querySelectorAll<HTMLElement>(focusableSelector) ?? [])
      if (elements.length === 0) return
      const first = elements[0]
      const last = elements[elements.length - 1]
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault()
        first.focus()
      }
    }

    document.addEventListener("keydown", handleKeyDown)
    return () => {
      document.removeEventListener("keydown", handleKeyDown)
      restoreFocusRef.current?.focus()
    }
  }, [open])

  if (!open) return null

  return (
    <DialogContext.Provider value={{ onClose, titleId }}>
      <div
        ref={containerRef}
        data-testid="dialog-backdrop"
        className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
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
      className={cn("w-full max-w-md overflow-hidden rounded-lg border border-zinc-700 bg-zinc-900 shadow-2xl", className)}
      {...props}
    />
  )
}

export function DialogHeader({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("flex items-center justify-between gap-4 border-b border-zinc-800 px-5 py-3", className)} {...props} />
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
  return <div className={cn("space-y-4 px-5 py-4", className)} {...props} />
}

export function DialogFooter({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("flex items-center justify-end gap-2 border-t border-zinc-800 px-5 py-3", className)} {...props} />
}
