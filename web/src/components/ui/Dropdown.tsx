import { useEffect, useRef, type ReactNode } from "react"
import { cn } from "../../lib/utils"

interface DropdownProps {
  trigger: ReactNode
  children: ReactNode
  open: boolean
  onOpenChange: (open: boolean) => void
  align?: "left" | "right"
  minWidth?: number
  className?: string
}

export function Dropdown({ trigger, children, open, onOpenChange, align = "right", minWidth = 160, className }: DropdownProps) {
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        onOpenChange(false)
      }
    }
    document.addEventListener("mousedown", handler)
    return () => document.removeEventListener("mousedown", handler)
  }, [open, onOpenChange])

  return (
    <div className={cn("relative", className)} ref={ref}>
      {trigger}
      {open && (
        <div
          className={cn(
            "absolute bottom-full mb-1 rounded border border-zinc-700 bg-zinc-900 shadow-xl py-1 z-10",
            align === "right" ? "right-0" : "left-0",
          )}
          style={{ minWidth }}
        >
          {children}
        </div>
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
