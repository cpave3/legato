import { useEffect, useInsertionEffect, useLayoutEffect, useRef, type RefObject } from "react"

type Layer = { container: HTMLElement; branches: Set<HTMLElement> }
const layers: Layer[] = []
const isolated = new Map<HTMLElement, string | null>()
let bodyOverflow = ""

function isolateBackground() {
  for (const [element, value] of isolated) {
    if (value === null) element.removeAttribute("inert")
    else element.setAttribute("inert", value)
  }
  isolated.clear()
  const top = layers.at(-1)
  const paths = new Set<HTMLElement>()
  for (const root of top ? [top.container, ...top.branches] : []) {
    for (let branch: HTMLElement | null = root; branch && branch !== document.body; branch = branch.parentElement) paths.add(branch)
  }
  for (const branch of paths) {
    if (branch === document.body) continue
    for (const sibling of branch.parentElement?.children ?? []) {
      if (paths.has(sibling as HTMLElement) || !(sibling instanceof HTMLElement)) continue
      if (!isolated.has(sibling)) isolated.set(sibling, sibling.getAttribute("inert"))
      sibling.setAttribute("inert", "")
    }
  }
}

function contains(layer: Layer, target: Node | null) {
  return layer.container.contains(target) || Array.from(layer.branches).some(branch => branch.contains(target))
}

// A portaled picker belongs to the modal containing its trigger, not to the background.
export function registerModalBranch(container: HTMLElement, trigger: HTMLElement | null) {
  const owner = layers.at(-1)
  if (!owner || !contains(owner, trigger)) return () => {}
  owner.branches.add(container)
  isolateBackground()
  return () => { owner.branches.delete(container); isolateBackground() }
}

const focusSelector = 'button:not([disabled]),a[href],input:not([disabled]),select:not([disabled]),textarea:not([disabled]),summary,[tabindex]:not([tabindex="-1"])'

export function useModalLayer({ open, onClose, containerRef, dismissible = true }: {
  open: boolean
  onClose: () => void
  containerRef: RefObject<HTMLElement | null>
  dismissible?: boolean
}) {
  const latest = useRef({ onClose, dismissible })
  const opener = useRef<HTMLElement | null>(null)
  const pendingRestore = useRef<HTMLElement | null>(null)
  const restore = () => {
    const target = pendingRestore.current
    pendingRestore.current = null
    if (target?.isConnected && (!layers.length || contains(layers.at(-1)!, target))) target.focus({ preventScroll: true })
  }
  useEffect(() => () => restore(), [])
  useInsertionEffect(() => {
    if (open) opener.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
  }, [open])
  useLayoutEffect(() => { latest.current = { onClose, dismissible } })

  useLayoutEffect(() => {
    const container = containerRef.current
    if (!open || !container) return
    const layer = { container, branches: new Set<HTMLElement>() }
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
        if (node instanceof HTMLDetailsElement && !node.open && !node.querySelector(":scope > summary")?.contains(element)) return false
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
      if (topmost() && !contains(layer, event.target as Node)) focusFirst()
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
      if (wasTop) pendingRestore.current = opener.current
    }
  }, [open, containerRef])
  useLayoutEffect(() => { if (!open) restore() }, [open])
}
