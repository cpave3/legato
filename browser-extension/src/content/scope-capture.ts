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

// CSS.escape, with the specification's algorithm where it is missing.
export function cssEscape(value: string): string {
  const native = (globalThis as { CSS?: { escape?: (value: string) => string } }).CSS?.escape
  if (native) return native(value)
  let out = ""
  for (let index = 0; index < value.length; index++) {
    const code = value.charCodeAt(index)
    const char = value[index]
    if (code === 0) out += "\uFFFD"
    else if ((code >= 1 && code <= 0x1f) || code === 0x7f || (index === 0 && code >= 48 && code <= 57)
      || (index === 1 && code >= 48 && code <= 57 && value.charCodeAt(0) === 45)) out += `\\${code.toString(16)} `
    else if (index === 0 && char === "-" && value.length === 1) out += "\\-"
    else if (code >= 0x80 || char === "-" || char === "_" || /[0-9A-Za-z]/.test(char)) out += char
    else out += `\\${char}`
  }
  return out
}

const quote = (value: string): string => `"${value.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`

// The element's ARIA role: its role attribute, else the implicit role of the
// common elements.
export function roleOf(element: Element): string | null {
  const explicit = element.getAttribute("role")?.trim().split(/\s+/)[0]
  if (explicit) return explicit
  const tag = element.tagName.toLowerCase()
  switch (tag) {
    case "button": case "summary": return "button"
    case "a": case "area": return element.hasAttribute("href") ? "link" : null
    case "h1": case "h2": case "h3": case "h4": case "h5": case "h6": return "heading"
    case "select": return element.hasAttribute("multiple") || Number(element.getAttribute("size")) > 1 ? "listbox" : "combobox"
    case "textarea": return "textbox"
    case "img": return element.getAttribute("alt") === "" ? "presentation" : "img"
    case "nav": return "navigation"
    case "main": return "main"
    case "ul": case "ol": return "list"
    case "li": return "listitem"
    case "table": return "table"
    case "dialog": return "dialog"
    case "option": return "option"
    case "progress": return "progressbar"
    case "input": {
      const type = (element.getAttribute("type") ?? "text").toLowerCase()
      if (["button", "submit", "reset", "image"].includes(type)) return "button"
      if (type === "checkbox") return "checkbox"
      if (type === "radio") return "radio"
      if (type === "range") return "slider"
      if (type === "search") return "searchbox"
      if (type === "number") return "spinbutton"
      if (["text", "email", "tel", "url", ""].includes(type)) return "textbox"
      return null
    }
    default: return null
  }
}

const collapse = (text: string | null | undefined): string => (text ?? "").replace(/\s+/g, " ").trim()

// A simple accessible name: aria-label, aria-labelledby, a form control's
// label, alt, then the element's own text, then its title.
export function accessibleName(element: Element): string {
  const doc = element.ownerDocument
  const label = collapse(element.getAttribute("aria-label"))
  if (label) return label
  const labelledBy = element.getAttribute("aria-labelledby")
  if (labelledBy) {
    const text = collapse(labelledBy.split(/\s+/).map((id) => doc.getElementById(id)?.textContent ?? "").join(" "))
    if (text) return text
  }
  const tag = element.tagName.toLowerCase()
  if (tag === "input" || tag === "select" || tag === "textarea") {
    const forLabel = element.id ? doc.querySelector(`label[for=${quote(element.id)}]`) : null
    const wrapping = element.closest("label")
    const owner = forLabel ?? wrapping
    if (owner) {
      const copy = owner.cloneNode(true) as Element
      for (const control of copy.querySelectorAll("input, select, textarea")) control.remove()
      const text = collapse(copy.textContent)
      if (text) return text
    }
    if (tag === "input" && ["button", "submit", "reset"].includes((element.getAttribute("type") ?? "").toLowerCase())) {
      const value = collapse((element as HTMLInputElement).value)
      if (value) return value
    }
    return collapse(element.getAttribute("placeholder")) || collapse(element.getAttribute("title"))
  }
  if (tag === "img") return collapse(element.getAttribute("alt")) || collapse(element.getAttribute("title"))
  return collapse(element.textContent) || collapse(element.getAttribute("title"))
}

const ROLE_SELECTOR = /^role=([a-z-]+)\[name="((?:[^"\\]|\\.)*)"\]$/

function byRole(doc: Document, role: string, name: string): Element[] {
  return [...doc.body.querySelectorAll("*")].filter((element) => roleOf(element) === role && accessibleName(element) === name)
}

// The element a selector names, when it names exactly one. A selector is
// CSS, or role=<role>[name="<accessible name>"].
export function resolveSelector(doc: Document, selector: string): Element | null {
  const role = ROLE_SELECTOR.exec(selector)
  if (role) {
    const found = byRole(doc, role[1], role[2].replace(/\\(.)/g, "$1"))
    return found.length === 1 ? found[0] : null
  }
  let matches: NodeListOf<Element>
  try { matches = doc.querySelectorAll(selector) } catch { return null }
  return matches.length === 1 ? matches[0] : null
}

const TEST_ID_ATTRIBUTES = ["data-testid", "data-test-id", "data-test", "data-cy", "data-qa"]
const NAME_LIMIT = 80

// Ways to name the element, the stablest first: a test ID, its id, its role
// and accessible name, a class combination, and last its nth-child path.
// Each one matches the element alone.
export function selectorCandidates(element: Element): string[] {
  const doc = element.ownerDocument
  const tag = element.tagName.toLowerCase()
  const candidates: string[] = []
  const offer = (selector: string): boolean => {
    if (candidates.includes(selector) || resolveSelector(doc, selector) !== element) return false
    candidates.push(selector)
    return true
  }
  for (const attribute of TEST_ID_ATTRIBUTES) {
    const value = element.getAttribute(attribute)
    if (value) offer(`[${attribute}=${quote(value)}]`)
  }
  if (element.id) offer(`#${cssEscape(element.id)}`)
  const role = roleOf(element)
  const name = role ? accessibleName(element) : ""
  if (role && name && name.length <= NAME_LIMIT) offer(`role=${role}[name=${quote(name)}]`)
  const classes = [...element.classList].filter((name) => name.length <= 60).slice(0, 8)
  const combinations: string[][] = classes.map((name) => [name])
  for (let i = 0; i < classes.length; i++) for (let j = i + 1; j < classes.length; j++) combinations.push([classes[i], classes[j]])
  for (const combination of combinations) {
    if (offer(`${tag}${combination.map((name) => `.${cssEscape(name)}`).join("")}`)) break
  }
  offer(nthChildPath(element))
  return candidates
}

export function textSnippet(element: Element): string {
  const text = collapse((element as HTMLElement).innerText ?? element.textContent)
  return text.length > TEXT_LIMIT ? `${text.slice(0, TEXT_LIMIT - 1)}…` : text
}

export function captureTarget(element: Element): PinTarget {
  const win = element.ownerDocument.defaultView!
  const rect = element.getBoundingClientRect()
  return {
    selectors: selectorCandidates(element),
    text: textSnippet(element),
    box: {
      x: Math.round(rect.left + win.scrollX),
      y: Math.round(rect.top + win.scrollY),
      width: Math.round(rect.width),
      height: Math.round(rect.height),
    },
  }
}

const sameText = (a: string, b: string): boolean => a === b || (a.length >= 60 && b.length >= 60 && a.slice(0, 60) === b.slice(0, 60))

// The element a saved target names on this page: a selector's element that
// still has the pin's text; else the one element with that text; else the
// first selector that matches one element (its text changed).
export function locateTarget(doc: Document, target: PinTarget): Element | null {
  const found = target.selectors.map((selector) => resolveSelector(doc, selector)).filter((element): element is Element => element !== null)
  if (target.text) {
    const withText = found.find((element) => sameText(textSnippet(element), target.text))
    if (withText) return withText
    const byText = [...doc.body.querySelectorAll("*")].filter((element) => sameText(textSnippet(element), target.text))
    // The innermost: an element with the text contains its ancestors' text too.
    const innermost = byText.filter((element) => !byText.some((other) => other !== element && element.contains(other)))
    if (innermost.length === 1) return innermost[0]
  }
  return found[0] ?? null
}
