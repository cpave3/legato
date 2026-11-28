import { useId, useLayoutEffect, useRef, type RefObject } from "react"
import { registerModalBranch } from "./useModalLayer"

interface MenuLayerOptions {
  open: boolean
  onOpen?: () => void
  onClose: () => void
  containerRef: RefObject<HTMLElement | null>
  outsideRef?: RefObject<HTMLElement | null>
  triggerRef?: RefObject<HTMLElement | null>
  position?: { x: number; y: number }
  align?: "left" | "right"
  placement?: "above" | "below" | "side"
  keyboard?: boolean
  dismissOnOutside?: boolean
}

// One lifecycle per opening: refreshing actions must not move the person's focus.
export function useMenuLayer({ open, onOpen, onClose, containerRef, triggerRef, outsideRef, position, align = "left", placement = "below", keyboard = true, dismissOnOutside = keyboard }: MenuLayerOptions) {
  const menuId = useId()
  const latest = useRef({ onOpen, onClose, position, align, placement })
  const opener = useRef<HTMLElement | null>(null)
  const initialLast = useRef(false)
  const updatePlacement = useRef<(() => void) | null>(null)
  useLayoutEffect(() => {
    latest.current = { onOpen, onClose, position, align, placement }
    updatePlacement.current?.()
  })
  useLayoutEffect(() => {
    const trigger = triggerRef?.current
    if (!trigger || !keyboard) return
    const keydown = (event: KeyboardEvent) => {
      if (open || !latest.current.onOpen || !['ArrowDown', 'ArrowUp'].includes(event.key)) return
      event.preventDefault()
      initialLast.current = event.key === 'ArrowUp'
      latest.current.onOpen()
    }
    trigger.addEventListener('keydown', keydown)
    return () => trigger.removeEventListener('keydown', keydown)
  }, [open, triggerRef, keyboard])
  useLayoutEffect(() => {
    if (!open || !containerRef.current) return
    const panel = containerRef.current
    const outsideContainer = outsideRef?.current ?? panel
    const trigger = triggerRef?.current ?? (document.activeElement instanceof HTMLElement ? document.activeElement : null)
    opener.current = trigger
    const releaseBranch = registerModalBranch(panel, trigger)
    if (keyboard && triggerRef && trigger) {
      panel.id ||= menuId
      trigger.id ||= `${menuId}-trigger`
      trigger.setAttribute("aria-controls", panel.id)
      if (!panel.hasAttribute("aria-label")) panel.setAttribute("aria-labelledby", trigger.id)
    }
    const items = () => Array.from(panel.querySelectorAll<HTMLElement>('[role^="menuitem"]')).filter(item => !item.matches(':disabled,[aria-disabled="true"]') && item.closest('[role="menu"]') === panel)
    const focus = (item?: HTMLElement) => {
      items().forEach(candidate => { candidate.tabIndex = candidate === item ? 0 : -1 })
      item?.focus({ preventScroll: true })
      item?.scrollIntoView?.({ block: "nearest" })
    }
    const place = () => {
      const viewport = window.visualViewport
      const width = viewport?.width ?? window.innerWidth
      const height = viewport?.height ?? window.innerHeight
      const x = viewport?.offsetLeft ?? 0
      const y = viewport?.offsetTop ?? 0
      Object.assign(panel.style, { position: 'fixed', maxWidth: `${Math.max(0, width - 16)}px`, maxHeight: `${Math.max(0, height - 16)}px`, overflowY: 'auto', boxSizing: 'border-box' })
      const bounds = panel.getBoundingClientRect()
      const anchor = trigger?.getBoundingClientRect()
      const options = latest.current
      let left = options.position?.x ?? (options.align === 'right' ? (anchor?.right ?? x + 8) - bounds.width : anchor?.left ?? x + 8)
      let top = options.position?.y ?? (options.placement === 'above' ? (anchor?.top ?? y + 8) - bounds.height - 4 : (anchor?.bottom ?? y + 8) + 4)
      if (!options.position && anchor) {
        if (top + bounds.height > y + height - 8 && anchor.top - bounds.height - 4 >= y + 8) top = anchor.top - bounds.height - 4
        else if (top < y + 8 && anchor.bottom + bounds.height + 4 <= y + height - 8) top = anchor.bottom + 4
      }
      if (options.placement === 'side' && anchor && !options.position) {
        left = anchor.right + 4
        if (left + bounds.width > x + width - 8) left = anchor.left - bounds.width - 4
        top = anchor.top
      }
      left = Math.max(x + 8, Math.min(left, x + width - bounds.width - 8))
      top = Math.max(y + 8, Math.min(top, y + height - bounds.height - 8))
      Object.assign(panel.style, { left: `${left}px`, top: `${top}px`, bottom: 'auto', right: 'auto', margin: '0' })
    }
    updatePlacement.current = place
    place()
    if (keyboard) {
      panel.tabIndex = -1
      focus((initialLast.current ? items().at(-1) : items()[0]) ?? panel)
      initialLast.current = false
    }
    let search = ''
    let searchTime = 0
    const keydown = (event: KeyboardEvent) => {
      if (!keyboard || event.defaultPrevented) return
      if (event.key === 'Escape' || (keyboard && event.key === 'Tab')) {
        event.preventDefault()
        event.stopPropagation()
        trigger?.focus({ preventScroll: true })
        latest.current.onClose()
        return
      }
      const all = items()
      const at = all.indexOf(document.activeElement as HTMLElement)
      let next: HTMLElement | undefined
      if (event.key === 'ArrowDown') next = all[(at + 1) % all.length]
      else if (event.key === 'ArrowUp') next = at < 0 ? all.at(-1) : all[(at - 1 + all.length) % all.length]
      else if (event.key === 'Home') next = all[0]
      else if (event.key === 'End') next = all.at(-1)
      else if (event.key.length === 1 && event.key !== ' ' && !event.ctrlKey && !event.metaKey && !event.altKey) {
        const now = Date.now()
        search = now - searchTime > 700 ? event.key : search + event.key
        searchTime = now
        const query = /^(.)\1*$/.test(search) ? search[0] : search
        next = [...all.slice(at + 1), ...all.slice(0, at + 1)].find(item => (item.getAttribute('aria-label') ?? item.textContent ?? '').trim().toLowerCase().startsWith(query.toLowerCase()))
      } else return
      event.preventDefault()
      if (next) focus(next)
    }
    const outside = (event: PointerEvent) => {
      if (!outsideContainer.contains(event.target as Node) && !trigger?.contains(event.target as Node)) latest.current.onClose()
    }
    const observer = typeof ResizeObserver !== 'undefined' ? new ResizeObserver(place) : null
    observer?.observe(panel)
    const mutations = new MutationObserver(() => {
      place()
      if (keyboard && (document.activeElement === document.body || panel.contains(document.activeElement)) && !items().includes(document.activeElement as HTMLElement)) focus(items()[0])
    })
    mutations.observe(panel, { childList: true, subtree: true })
    panel.addEventListener('keydown', keydown)
    if (dismissOnOutside) document.addEventListener('pointerdown', outside)
    window.addEventListener('resize', place)
    window.addEventListener('scroll', place, true)
    window.visualViewport?.addEventListener('resize', place)
    window.visualViewport?.addEventListener('scroll', place)
    return () => {
      updatePlacement.current = null
      releaseBranch()
      observer?.disconnect()
      mutations.disconnect()
      panel.removeEventListener('keydown', keydown)
      document.removeEventListener('pointerdown', outside)
      window.removeEventListener('resize', place)
      window.removeEventListener('scroll', place, true)
      window.visualViewport?.removeEventListener('resize', place)
      window.visualViewport?.removeEventListener('scroll', place)
      // Do not take focus back from a dialog an action just opened.
      if (document.activeElement === document.body || outsideContainer.contains(document.activeElement)) trigger?.focus({ preventScroll: true })
    }
  }, [open, containerRef, triggerRef, outsideRef, keyboard, dismissOnOutside, menuId])
  return { restoreFocus: () => opener.current?.focus({ preventScroll: true }) }
}
