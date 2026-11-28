import { useEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type PointerEvent as ReactPointerEvent } from "react"

export const MIN_WIDTH_REM = 20
export const MAX_WIDTH_REM = 64
const DEFAULT_WIDTH_REM = 34
const WIDTH_KEY = "legato:artifact-panel-width"

function storedWidth(): number {
  try {
    const stored = Number(localStorage.getItem(WIDTH_KEY))
    if (Number.isFinite(stored) && stored >= MIN_WIDTH_REM && stored <= MAX_WIDTH_REM) return stored
  } catch {
    // Storage can be unavailable; the default width is fine.
  }
  return DEFAULT_WIDTH_REM
}

function clampWidth(value: number): number {
  return Math.min(MAX_WIDTH_REM, Math.max(MIN_WIDTH_REM, value))
}

// useArtifactPanelWidth is the width of the artifact panel's column on a
// wide screen, remembered between visits, and the handlers of its resize
// handle: dragged with a pointer, or stepped with the arrow keys.
export function useArtifactPanelWidth(mobile: boolean) {
  const [widthRem, setWidthRem] = useState(storedWidth)

  useEffect(() => {
    try {
      localStorage.setItem(WIDTH_KEY, String(widthRem))
    } catch {
      // Not remembered; nothing else depends on it.
    }
  }, [widthRem])

  const stopResize = useRef<(() => void) | null>(null)
  useEffect(() => () => stopResize.current?.(), [mobile])

  function resizeFromPointer(event: ReactPointerEvent<HTMLDivElement>) {
    event.preventDefault()
    stopResize.current?.()
    const handle = event.currentTarget
    const pointerId = event.pointerId
    handle.setPointerCapture(pointerId)
    const startX = event.clientX
    const startWidth = widthRem
    const rootSize = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16
    const move = (moveEvent: PointerEvent) => {
      if (moveEvent.pointerId === pointerId) setWidthRem(clampWidth(startWidth + (startX - moveEvent.clientX) / rootSize))
    }
    const stop = () => {
      window.removeEventListener("pointermove", move)
      window.removeEventListener("pointerup", stop)
      window.removeEventListener("pointercancel", stop)
      window.removeEventListener("blur", stop)
      handle.removeEventListener("lostpointercapture", stop)
      if (handle.hasPointerCapture(pointerId)) handle.releasePointerCapture(pointerId)
      stopResize.current = null
    }
    stopResize.current = stop
    window.addEventListener("pointercancel", stop)
    window.addEventListener("blur", stop)
    handle.addEventListener("lostpointercapture", stop)
    window.addEventListener("pointermove", move)
    window.addEventListener("pointerup", stop)
  }

  function resizeFromKeyboard(event: ReactKeyboardEvent<HTMLDivElement>) {
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return
    event.preventDefault()
    setWidthRem((width) => clampWidth(width + (event.key === "ArrowLeft" ? 2 : -2)))
  }

  return { widthRem, resizeFromPointer, resizeFromKeyboard }
}
