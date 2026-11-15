import { useInsertionEffect, useLayoutEffect, useRef, type RefObject } from "react"

type Layer = { container: HTMLElement }
const layers: Layer[] = []
const isolated = new Map<HTMLElement, string | null>()
let bodyOverflow = ""

function isolateBackground() {
  for (const [element, value] of isolated) {
    if (value === null) element.removeAttribute("inert")
    else element.setAttribute("inert", value)
  }
  isolated.clear()
  let branch = layers.at(-1)?.container
  while (branch && branch !== document.body) {
    const parent = branch.parentElement
    if (!parent) break
    for (const sibling of parent.children) {
      if (sibling === branch || !(sibling instanceof HTMLElement)) continue
      isolated.set(sibling, sibling.getAttribute("inert"))
      sibling.setAttribute("inert", "")
    }
    branch = parent
  }
}

const focusSelector = 'button:not([disabled]),a[href],input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])'

export function useModalLayer({ open, onClose, containerRef, dismissible = true }: {
  open: boolean
  onClose: () => void
  containerRef: RefObject<HTMLElement | null>
  dismissible?: boolean
}) {
  const latest = useRef({ onClose, dismissible })
  const opener = useRef<HTMLElement | null>(null)
  useInsertionEffect(() => {
    if (open) opener.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
  }, [open])
  useLayoutEffect(() => { latest.current = { onClose, dismissible } })

  useLayoutEffect(() => {
    const container = containerRef.current
    if (!open || !container) return
    const layer = { container }
    if (layers.length === 0) {
      bodyOverflow = document.body.style.overflow
      document.body.style.overflow = "hidden"
    }
    layers.push(layer)
    isolateBackground()
    const topmost = () => layers.at(-1) === layer
    const controls = () => Array.from(container.querySelectorAll<HTMLElement>(focusSelector)).filter(element => {
      if (element.matches(':disabled') || element.closest('[inert],[hidden],[aria-hidden="true"]')) return false
      for (let node: HTMLElement | null = element; node && node !== container; node = node.parentElement) {
        const style = getComputedStyle(node)
        if (style.display === "none" || style.visibility === "hidden") return false
      }
      return true
    })
    const focusFirst = () => {
      const preferred = container.querySelector<HTMLElement>('[data-autofocus],[autofocus]')
      const fallback = container.matches('[role="dialog"],[role="alertdialog"]') ? container : container.querySelector<HTMLElement>('[role="dialog"],[role="alertdialog"]') ?? container
      if (!fallback.hasAttribute("tabindex")) fallback.tabIndex = -1
      const target = preferred && controls().includes(preferred) ? preferred : controls()[0] ?? fallback
      target.focus({ preventScroll: true })
    }
    focusFirst()
    const keydown = (event: KeyboardEvent) => {
      if (!topmost() || event.defaultPrevented) return
      if (event.key === "Escape") {
        event.preventDefault()
        event.stopImmediatePropagation()
        if (latest.current.dismissible) latest.current.onClose()
      } else if (event.key === "Tab") {
        const available = controls()
        const first = available[0]
        const last = available.at(-1)
        if (!first) {
          event.preventDefault()
          focusFirst()
        } else if (!container.contains(document.activeElement) || (event.shiftKey && document.activeElement === first) || (!event.shiftKey && document.activeElement === last)) {
          event.preventDefault()
          ;(event.shiftKey ? last : first)?.focus({ preventScroll: true })
        }
        event.stopPropagation()
      }
    }
    const focusin = (event: FocusEvent) => {
      if (topmost() && !container.contains(event.target as Node)) focusFirst()
    }
    const observer = new MutationObserver(() => { if (topmost()) isolateBackground() })
    observer.observe(document.body, { childList: true, subtree: true })
    document.addEventListener("keydown", keydown)
    document.addEventListener("focusin", focusin)
    return () => {
      observer.disconnect()
      document.removeEventListener("keydown", keydown)
      document.removeEventListener("focusin", focusin)
      const wasTop = topmost()
      layers.splice(layers.indexOf(layer), 1)
      isolateBackground()
      if (layers.length === 0) document.body.style.overflow = bodyOverflow
      if (wasTop && opener.current?.isConnected) opener.current.focus({ preventScroll: true })
    }
  }, [open, containerRef])
}
