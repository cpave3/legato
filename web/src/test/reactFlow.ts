// The shims React Flow's testing guide gives for jsdom, which measures
// nothing: a ResizeObserver that reports at once, DOMMatrixReadOnly for the
// viewport's zoom, element sizes from their inline style, and getBBox for
// edge labels. Call it at the top of a test file that renders a graph.
class ResizeObserver {
  callback: ResizeObserverCallback
  constructor(callback: ResizeObserverCallback) { this.callback = callback }
  observe(target: Element) { this.callback([{ target } as ResizeObserverEntry], this) }
  unobserve() {}
  disconnect() {}
}

class DOMMatrixReadOnly {
  m22: number
  constructor(transform?: string) {
    // The guide's pattern reads one character, so scale(0.9) read as 1 and an
    // animated move of the view never settled.
    const scale = transform?.match(/scale\(([\d.]+)\)/)?.[1]
    this.m22 = scale !== undefined ? +scale : 1
  }
}

let installed = false

export function mockReactFlow() {
  if (installed) return
  installed = true
  globalThis.ResizeObserver = ResizeObserver as unknown as typeof globalThis.ResizeObserver
  globalThis.DOMMatrixReadOnly = DOMMatrixReadOnly as unknown as typeof globalThis.DOMMatrixReadOnly
  Object.defineProperties(HTMLElement.prototype, {
    offsetHeight: { configurable: true, get() { return parseFloat((this as HTMLElement).style.height) || 1 } },
    offsetWidth: { configurable: true, get() { return parseFloat((this as HTMLElement).style.width) || 1 } },
  })
  ;(SVGElement.prototype as unknown as { getBBox: () => DOMRect }).getBBox = () => ({ x: 0, y: 0, width: 0, height: 0 }) as DOMRect
}
