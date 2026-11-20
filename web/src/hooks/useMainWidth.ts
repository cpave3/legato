import { createContext, useContext, useEffect, useState } from "react"

// MainWidthContext is the width of the layout's main area: the window less
// the navigation and the needs-you rail. Pages lay out by it, not by the
// window. Undefined outside the layout.
export const MainWidthContext = createContext<number | undefined>(undefined)

// useMainWidth is the width a page has: the main area's, or the window's
// outside the layout (tests of a page alone).
export function useMainWidth(): number {
  const main = useContext(MainWidthContext)
  const [windowWidth, setWindowWidth] = useState(() => (typeof window === "undefined" ? 0 : window.innerWidth))
  useEffect(() => {
    if (main !== undefined) return
    const update = () => setWindowWidth(window.innerWidth)
    window.addEventListener("resize", update)
    return () => window.removeEventListener("resize", update)
  }, [main])
  return main ?? windowWidth
}
