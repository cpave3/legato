import type { PinStyles } from "../scope-types"

// The computed properties that explain most visual bugs: box, layout,
// colour and type.
export const STYLE_PROPERTIES = [
  "display", "position", "top", "right", "bottom", "left", "z-index",
  "width", "height", "min-width", "max-width", "min-height", "max-height", "box-sizing",
  "margin", "padding", "border", "border-radius", "box-shadow", "outline",
  "overflow", "opacity", "visibility", "transform",
  "color", "background-color", "background-image",
  "font-family", "font-size", "font-weight", "line-height", "letter-spacing",
  "text-align", "text-decoration", "white-space", "text-overflow",
  "flex", "align-self",
] as const

// The parent's layout: how it places the pinned element.
export const PARENT_LAYOUT_PROPERTIES = [
  "display", "position", "flex-direction", "flex-wrap", "justify-content", "align-items", "align-content",
  "gap", "row-gap", "column-gap", "grid-template-columns", "grid-template-rows", "grid-auto-flow",
  "overflow", "width", "height", "padding",
] as const

// The parent's display and position are kept even when they are the
// default: they say what kind of layout the element is in.
const ALWAYS_PARENT = new Set(["display", "position"])

type StyleLike = { getPropertyValue: (property: string) => string }

export type StyleReader = {
  computed: (element: Element) => StyleLike
  // The browser's default style of a fresh element of the tag ("svg:<tag>"
  // for SVG), or null when it cannot be read.
  defaults: (tag: string) => StyleLike | null
}

function tagKey(element: Element): string {
  return element.namespaceURI === "http://www.w3.org/2000/svg" ? `svg:${element.localName}` : element.localName
}

function differing(element: Element, reader: StyleReader, properties: readonly string[], always: Set<string> = new Set()): Record<string, string> {
  const computed = reader.computed(element)
  const defaults = reader.defaults(tagKey(element))
  const out: Record<string, string> = {}
  for (const property of properties) {
    const value = computed.getPropertyValue(property).trim()
    if (!value) continue
    if (defaults && !always.has(property) && defaults.getPropertyValue(property).trim() === value) continue
    out[property] = value
  }
  return out
}

export function captureStyles(element: Element, reader: StyleReader): PinStyles {
  const parent = element.parentElement
  return {
    computed: differing(element, reader, STYLE_PROPERTIES),
    classes: [...element.classList],
    inline: element.getAttribute("style")?.trim() ? (element as HTMLElement).style?.cssText ?? element.getAttribute("style")! : "",
    parent: parent ? differing(parent, reader, PARENT_LAYOUT_PROPERTIES, ALWAYS_PARENT) : {},
  }
}

// Reads the page's computed styles, and the browser's defaults from fresh
// elements in an empty iframe of its own, where no site CSS reaches. The
// iframe is put in the given container (the overlay's shadow root, so the
// page's observers do not see it) and removed by dispose.
export function createStyleReader(doc: Document, container: Node): StyleReader & { dispose: () => void } {
  const win = doc.defaultView!
  let frame: HTMLIFrameElement | null = null
  const cache = new Map<string, Record<string, string> | null>()
  const all = [...new Set<string>([...STYLE_PROPERTIES, ...PARENT_LAYOUT_PROPERTIES])]
  const open = (): Document | null => {
    if (!frame) {
      frame = doc.createElement("iframe")
      frame.setAttribute("aria-hidden", "true")
      frame.tabIndex = -1
      frame.style.cssText = "position:fixed;left:-10000px;top:0;width:1024px;height:768px;visibility:hidden;border:0;pointer-events:none"
      container.appendChild(frame)
      const frameDoc = frame.contentDocument
      if (!frameDoc) return null
      frameDoc.open()
      frameDoc.write("<!doctype html><html><head></head><body></body></html>")
      frameDoc.close()
    }
    return frame.contentDocument
  }
  return {
    computed: (element) => win.getComputedStyle(element),
    defaults: (tag) => {
      if (!cache.has(tag)) {
        try {
          const frameDoc = open()
          const frameWin = frameDoc?.defaultView
          if (!frameDoc || !frameWin) throw new Error("no frame")
          const fresh = tag.startsWith("svg:")
            ? frameDoc.createElementNS("http://www.w3.org/2000/svg", tag.slice(4))
            : frameDoc.createElement(tag)
          const host = tag.startsWith("svg:") ? frameDoc.createElementNS("http://www.w3.org/2000/svg", "svg") : frameDoc.body
          if (host !== frameDoc.body) frameDoc.body.append(host)
          host.append(fresh)
          const style = frameWin.getComputedStyle(fresh)
          cache.set(tag, Object.fromEntries(all.map((property) => [property, style.getPropertyValue(property).trim()])))
          fresh.remove()
          if (host !== frameDoc.body) host.remove()
        } catch {
          cache.set(tag, null)
        }
      }
      const values = cache.get(tag)
      return values ? { getPropertyValue: (property) => values[property] ?? "" } : null
    },
    dispose: () => {
      frame?.remove()
      frame = null
    },
  }
}
