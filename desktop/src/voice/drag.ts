import { useRef, type MouseEvent, type PointerEvent } from "react"
import { invoke } from "@tauri-apps/api/core"

// Dragging the widget (src-tauri/src/app.rs, voice_widget_drag_*): a press
// that moves at least DRAG_FROM pixels is a drag, and the click it ends with
// is not delivered, so a click on the capsule still talks. Nothing inside
// [data-no-drag] starts one.
//
// The window moves under the pointer, so where the pointer is in the page,
// less where it took hold, is how far the window still has to go. One move
// is sent at a time: what the pointer does while the window is moving is
// measured from where the window was, and dropped.
export const DRAG_FROM = 4

type Grip = { id: number; x: number; y: number; dragging: boolean; moving: boolean; queue: Promise<unknown> }

export function useWidgetDrag() {
  const grip = useRef<Grip | null>(null)
  const swallow = useRef(false)

  const send = (g: Grip, cmd: string, args?: Record<string, number>) => {
    g.queue = g.queue.then(() => invoke(cmd, args)).catch(() => {})
    return g.queue
  }
  const end = () => {
    const g = grip.current
    grip.current = null
    if (g?.dragging) {
      swallow.current = true
      send(g, "voice_widget_drag_end")
    }
  }

  return {
    onPointerDown(e: PointerEvent<HTMLElement>) {
      swallow.current = false
      if (e.button !== 0 || (e.target as Element).closest?.("[data-no-drag]")) return
      grip.current = { id: e.pointerId, x: e.clientX, y: e.clientY, dragging: false, moving: false, queue: Promise.resolve() }
    },
    onPointerMove(e: PointerEvent<HTMLElement>) {
      const g = grip.current
      if (!g || e.pointerId !== g.id || g.moving) return
      const dx = e.clientX - g.x
      const dy = e.clientY - g.y
      if (!g.dragging) {
        if (Math.hypot(dx, dy) < DRAG_FROM) return
        g.dragging = true
        try { e.currentTarget.setPointerCapture?.(e.pointerId) } catch { /* jsdom, or the pointer is gone */ }
        send(g, "voice_widget_drag_start")
      }
      if (!dx && !dy) return
      g.moving = true
      send(g, "voice_widget_drag_move", { dx, dy }).then(() => { g.moving = false })
    },
    onPointerUp: end,
    onPointerCancel: end,
    onLostPointerCapture: end,
    onClickCapture(e: MouseEvent<HTMLElement>) {
      if (!swallow.current) return
      swallow.current = false
      e.stopPropagation()
      e.preventDefault()
    },
  }
}
