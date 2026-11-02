import { useEffect } from "react"
import { tintColor } from "../lib/tints"

// TintBand is the server's tint as a band across the top of the window, and
// as the browser's theme colour (a phone's address bar, an installed app's
// title bar). With no tint there is no band and the theme colour is the
// page's own.
export function TintBand({ tint }: { tint?: string }) {
  const color = tintColor(tint)
  useEffect(() => {
    const meta = document.querySelector('meta[name="theme-color"]')
    if (!meta || !color) return
    const own = meta.getAttribute("content") ?? ""
    meta.setAttribute("content", color)
    return () => meta.setAttribute("content", own)
  }, [color])
  if (!color) return null
  return (
    <div
      data-testid="tint-band"
      data-tint={tint}
      aria-hidden="true"
      className="pointer-events-none fixed inset-x-0 top-0 z-[80] h-[3px]"
      style={{ backgroundColor: color }}
    />
  )
}
