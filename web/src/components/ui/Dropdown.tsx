import { createPortal } from "react-dom"
import { useMenuLayer } from "../../hooks/useMenuLayer"
import { useLayoutEffect, useId, useRef, type ReactNode } from "react"
import { cn } from "../../lib/utils"

interface DropdownProps {
  trigger: ReactNode
  children: ReactNode
  open: boolean
  onOpenChange: (open: boolean) => void
  align?: "left" | "right"
  minWidth?: number
  className?: string
  // panelClassName is added to the open panel's, such as a z-index that
  // puts a menu opened in a sheet above the sheet.
  panelClassName?: string
  kind?: "menu" | "dialog"
}

export function Dropdown({ trigger, children, open, onOpenChange, align = "right", minWidth = 160, className, panelClassName, kind = "menu" }: DropdownProps) {
  const ref = useRef<HTMLDivElement>(null)

  const panel = useRef<HTMLDivElement>(null)
  const anchor = useRef<HTMLElement | null>(null)
  const id = useId()
  useLayoutEffect(() => {
    anchor.current = ref.current?.querySelector<HTMLElement>('button,[role="button"]') ?? null
    anchor.current?.setAttribute('aria-haspopup', kind)
    anchor.current?.setAttribute('aria-expanded', String(open))
    anchor.current?.setAttribute('aria-controls', id)
  }, [open, kind, id])
  useMenuLayer({ open, onOpen: () => onOpenChange(true), onClose: () => onOpenChange(false), containerRef: panel, triggerRef: anchor, align, placement: "above", keyboard: kind === "menu" })

  return (
    <div className={cn("relative", className)} ref={ref}>
      {trigger}
      {open && createPortal(
        <div
          ref={panel}
          id={id}
          role={kind === "menu" ? "menu" : undefined}
          className={cn(
            "absolute bottom-full mb-1 rounded border border-zinc-700 bg-zinc-900 shadow-xl py-1 z-10",
            align === "right" ? "right-0" : "left-0",
            panelClassName,
          )}
          style={{ minWidth: `min(${minWidth}px, calc(100vw - 16px))` }}
        >
          {children}
        </div>, document.body
      )}
    </div>
  )
}

interface DropdownItemProps {
  children: ReactNode
  onClick?: () => void
  active?: boolean
  className?: string
}

export function DropdownItem({ children, onClick, active, className }: DropdownItemProps) {
  return (
    <button
      type="button"
      role="menuitem"
      onClick={onClick}
      className={cn(
        "flex w-full items-center gap-2 px-3 py-1.5 text-xs transition-colors",
        active ? "text-zinc-200 bg-zinc-800" : "text-zinc-300 hover:bg-zinc-800",
        className,
      )}
    >
      {children}
    </button>
  )
}
