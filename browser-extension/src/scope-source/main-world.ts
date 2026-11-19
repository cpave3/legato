import { createReactAdapter } from "./react-adapter"
import { PROBE_ATTRIBUTE, type ComponentReading, type FrameworkAdapter } from "./types"

// The page's half of a pin's source (see types.ts): finds the element the
// content script marked and asks each framework adapter about it. It runs in
// the page's MAIN world, so the service worker checks what it returns.
export function readProbedComponent(doc: Document, adapters?: FrameworkAdapter[]): ComponentReading | null {
  const element = doc.querySelector(`[${PROBE_ATTRIBUTE}]`)
  if (!element) return null
  const win = doc.defaultView
  for (const adapter of adapters ?? (win ? [createReactAdapter(win)] : [])) {
    try {
      const reading = adapter.read(element)
      if (reading) return reading
    } catch {
      // An adapter that cannot read the page gives way to the next.
    }
  }
  return null
}
