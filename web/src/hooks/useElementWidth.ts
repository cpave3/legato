import { useLayoutEffect, useState, type RefObject } from "react"

// The width of an element, kept current as it changes. Where there is no
// ResizeObserver (tests, very old browsers) the window's width stands in.
export function useElementWidth(ref: RefObject<HTMLElement | null>): number {
  const [width, setWidth] = useState(() => (typeof window === "undefined" ? 0 : window.innerWidth))

  useLayoutEffect(() => {
    const element = ref.current
    if (typeof ResizeObserver === "undefined" || !element) {
      const update = () => setWidth(window.innerWidth)
      update()
      window.addEventListener("resize", update)
      return () => window.removeEventListener("resize", update)
    }
    setWidth(element.getBoundingClientRect().width)
    const observer = new ResizeObserver(([entry]) => setWidth(entry.contentRect.width))
    observer.observe(element)
    return () => observer.disconnect()
  }, [ref])

  return width
}
