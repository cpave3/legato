import { useEffect, useState } from "react"

function matches(query: string): boolean {
  return typeof window !== "undefined" && typeof window.matchMedia === "function" && window.matchMedia(query).matches
}

// Whether a CSS media query matches, kept current as the window changes.
// Without matchMedia (tests, very old browsers) it never matches.
export function useMediaQuery(query: string): boolean {
  const [value, setValue] = useState(() => matches(query))

  useEffect(() => {
    if (typeof window.matchMedia !== "function") return
    const list = window.matchMedia(query)
    const update = () => setValue(list.matches)
    update()
    list.addEventListener?.("change", update)
    return () => list.removeEventListener?.("change", update)
  }, [query])

  return value
}
