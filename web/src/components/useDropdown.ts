import { useCallback, useEffect, useRef, useState } from "react"

// useDropdown is a menu that opens over the composer and closes on a click
// anywhere outside the element its ref is on.
export function useDropdown() {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener("mousedown", handler)
    return () => document.removeEventListener("mousedown", handler)
  }, [])

  const toggle = useCallback(() => setOpen((v) => !v), [])
  const show = useCallback(() => setOpen(true), [])
  const close = useCallback(() => setOpen(false), [])

  return { open, ref, toggle, show, close }
}
