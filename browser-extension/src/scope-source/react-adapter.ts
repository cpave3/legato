import { getDisplayName, getFiberFromHostInstance, isCompositeFiber, isFiber, isHostFiber, type Fiber } from "./bippy"
import { parseStack } from "./stack"
import { MAX_FRAMES, MAX_NAME_LENGTH, MAX_OWNERS, MAX_URL_LENGTH, type ComponentReading, type FrameworkAdapter, type RawFrame } from "./types"

// React's adapter, which runs in the page. React keeps the fibre that
// rendered a DOM element on the element; in development the fibre also
// says where the element was made:
//
// - React 19: _debugStack, an Error captured when the JSX ran. Its first
//   frame in the project is the JSX's line in the code the browser ran,
//   which the source map turns into the source line.
// - React 18: _debugSource, the file, line and column the JSX transform
//   wrote into the element; already the source position.
//
// A production build keeps neither, and its names are usually minified: the
// name is all there is. The approach (fibre from the node, _debugStack's
// first user frame, _debugSource before React 19) is the one react-grab and
// LocatorJS take (both MIT); the code is Scope's own, over bippy's helpers.

const nameOf = (type: unknown): string => (getDisplayName(type) || "Anonymous").slice(0, MAX_NAME_LENGTH)

// A fibre's owner: the component whose render made it. React 19 may give a
// server component's info ({name}) in place of a fibre.
type Owner = { name: string; fiber: Fiber | null; next: unknown }

function ownerOf(value: unknown): Owner | null {
  if (!value || typeof value !== "object") return null
  if (isFiber(value)) {
    if (!isCompositeFiber(value)) return null
    return { name: nameOf(value.type), fiber: value, next: value._debugOwner }
  }
  const info = value as { name?: unknown; owner?: unknown }
  return typeof info.name === "string" ? { name: info.name.slice(0, MAX_NAME_LENGTH) || "Anonymous", fiber: null, next: info.owner } : null
}

// The components above a fibre, nearest first: its owners in development,
// else the components that contain it.
function components(host: Fiber): Owner[] {
  const found: Owner[] = []
  const limit = MAX_OWNERS + 1
  if (host._debugOwner) {
    let owner = ownerOf(host._debugOwner)
    while (owner && found.length < limit) {
      found.push(owner)
      owner = ownerOf(owner.next)
    }
    if (found.length) return found
  }
  for (let fiber = host.return; fiber && found.length < limit; fiber = fiber.return) {
    if (isCompositeFiber(fiber)) found.push({ name: nameOf(fiber.type), fiber: null, next: null })
  }
  return found
}

// React 18's _debugSource names a file on disk. The page's own list of the
// modules it loaded gives that file's URL: the longest loaded path the file
// name ends with. Failing that, the file itself.
function moduleUrlFor(fileName: string, win: Window): string | null {
  const normalized = fileName.replace(/\\/g, "/")
  const origin = win.location.origin
  const loaded = new Set<string>()
  const add = (href: string | undefined): void => {
    try {
      const url = new URL(href ?? "", win.location.href)
      if (url.origin === origin) loaded.add(url.pathname)
    } catch { /* not a URL */ }
  }
  try {
    for (const entry of win.performance.getEntriesByType("resource")) add(entry.name)
  } catch { /* no resource timing */ }
  for (const script of Array.from(win.document.scripts)) add(script.src)
  let best = ""
  for (const path of loaded) {
    if (path.length > best.length && path.length > 1 && normalized.endsWith(path)) best = path
  }
  if (best) return `${origin}${best}`
  if (normalized.startsWith("/")) return `file://${normalized}`
  if (/^[A-Za-z]:\//.test(normalized)) return `file:///${normalized}`
  return null
}

function sourceFrame(source: unknown, win: Window): RawFrame | null {
  if (!source || typeof source !== "object") return null
  const { fileName, lineNumber, columnNumber } = source as { fileName?: unknown; lineNumber?: unknown; columnNumber?: unknown }
  if (typeof fileName !== "string" || typeof lineNumber !== "number" || fileName.length > MAX_URL_LENGTH) return null
  const url = moduleUrlFor(fileName, win)
  if (!url) return null
  return { url, line: lineNumber, column: typeof columnNumber === "number" ? columnNumber : 0, original: true }
}

function stackFrames(stack: unknown): RawFrame[] {
  if (!stack || typeof stack !== "object") return []
  const text = (stack as { stack?: unknown }).stack
  return typeof text === "string" ? parseStack(text) : []
}

// Where a fibre's element was written.
function framesOf(fiber: Fiber, win: Window): RawFrame[] {
  const fromSource = sourceFrame(fiber._debugSource, win)
  return fromSource ? [fromSource] : stackFrames(fiber._debugStack)
}

export function createReactAdapter(win: Window): FrameworkAdapter {
  return {
    framework: "react",
    read(element: Element): ComponentReading | null {
      const fiber = getFiberFromHostInstance(element)
      if (!fiber || !isFiber(fiber) || !isHostFiber(fiber)) return null
      const above = components(fiber)
      // The element's own frames first, then where each owner was written:
      // an element a dependency's component drew is placed where the
      // project used that component.
      const frames = framesOf(fiber, win)
      for (const owner of above) {
        if (frames.length >= MAX_FRAMES) break
        if (owner.fiber) frames.push(...framesOf(owner.fiber, win))
      }
      const [component, ...owners] = above
      return {
        framework: "react",
        ...(component ? { name: component.name } : {}),
        owners: owners.slice(0, MAX_OWNERS).map((owner) => owner.name),
        frames: frames.slice(0, MAX_FRAMES),
      }
    },
  }
}
