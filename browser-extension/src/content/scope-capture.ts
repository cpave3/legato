import type { PinPage, PinTarget } from "../scope-types"

const TEXT_LIMIT = 200

export function capturePage(win: Window): PinPage {
  return {
    url: win.location.href,
    title: win.document.title,
    viewport: { width: win.innerWidth, height: win.innerHeight },
    scroll: { x: Math.round(win.scrollX), y: Math.round(win.scrollY) },
    dpr: win.devicePixelRatio || 1,
  }
}

// The element's place from body down, each step its tag and nth-child: it
// always matches the element alone, though any change to the page above it
// moves it.
export function nthChildPath(element: Element): string {
  const steps: string[] = []
  let current: Element | null = element
  while (current && current.parentElement && current !== current.ownerDocument.body) {
    const index = [...current.parentElement.children].indexOf(current) + 1
    steps.unshift(`${current.tagName.toLowerCase()}:nth-child(${index})`)
    current = current.parentElement
  }
  steps.unshift(current === element.ownerDocument.body ? "body" : (current?.tagName.toLowerCase() ?? "html"))
  return steps.join(" > ")
}

// The element a selector names, when it names exactly one.
export function resolveSelector(doc: Document, selector: string): Element | null {
  let matches: NodeListOf<Element>
  try { matches = doc.querySelectorAll(selector) } catch { return null }
  return matches.length === 1 ? matches[0] : null
}

export function textSnippet(element: Element): string {
  const text = ((element as HTMLElement).innerText ?? element.textContent ?? "").replace(/\s+/g, " ").trim()
  return text.length > TEXT_LIMIT ? `${text.slice(0, TEXT_LIMIT - 1)}…` : text
}

export function captureTarget(element: Element): PinTarget {
  const win = element.ownerDocument.defaultView!
  const rect = element.getBoundingClientRect()
  return {
    selectors: [nthChildPath(element)],
    text: textSnippet(element),
    box: {
      x: Math.round(rect.left + win.scrollX),
      y: Math.round(rect.top + win.scrollY),
      width: Math.round(rect.width),
      height: Math.round(rect.height),
    },
  }
}

// The element a saved target names on this page: the first selector that
// matches one element.
export function locateTarget(doc: Document, target: PinTarget): Element | null {
  for (const selector of target.selectors) {
    const found = resolveSelector(doc, selector)
    if (found) return found
  }
  return null
}
