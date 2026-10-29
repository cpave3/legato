import { LEGATO_MONO, LEGATO_SANS, loadLegatoFonts } from "./fonts"

export type LegatoFabState = "idle" | "loading" | "success" | "error"

type LegatoFabOptions = {
  id: string
  title: string
  subtitle: string
  actionLabel: string
}

export type LegatoFab = {
  root: HTMLDivElement
  title: HTMLDivElement
  subtitle: HTMLDivElement
  button: HTMLButtonElement
  status: HTMLDivElement
  actions: HTMLDivElement
  addSecondaryAction: (label: string) => HTMLButtonElement
  setEmphasis: (button: HTMLButtonElement) => void
  setPrimaryVisible: (visible: boolean) => void
  setState: (state: LegatoFabState, message: string) => void
}

// The web app's new look (the lw- colours of web/src/index.css): one ground,
// one accent, colours named for what they are for.
const lw = {
  ground: "#111114",
  surface: "#17171b",
  raised: "#1d1d22",
  line: "#26262d",
  lineStrong: "#31313a",
  text: "#e6e5eb",
  text2: "#9a99a5",
  faint: "#66656f",
  accent: "#6d5ef6",
  accentRing: "#4f46b8",
  accentText: "#c8c0ff",
  live: "#3ecf8e",
  error: "#f0646b",
}
const sans = `${LEGATO_SANS}, "Geist Sans", Geist, Inter, ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif`
const mono = `${LEGATO_MONO}, "Geist Mono", ui-monospace, SFMono-Regular, Menlo, monospace`

const stateColors: Record<LegatoFabState, string> = {
  idle: lw.text2,
  loading: lw.accentText,
  success: lw.live,
  error: lw.error,
}

export function createLegatoFab(doc: Document, options: LegatoFabOptions): LegatoFab {
  loadLegatoFonts(doc)
  const root = doc.createElement("div")
  root.id = options.id
  root.dataset.legatoFab = "true"
  root.dataset.expanded = "false"
  root.tabIndex = 0
  root.setAttribute("aria-label", `Open Legato actions: ${options.title}`)
  root.setAttribute("aria-expanded", "false")
  root.style.cssText = [
    "all:initial", "position:fixed", "right:24px", "bottom:24px", "z-index:2147483647",
    "box-sizing:border-box", "display:flex", "flex-direction:column", "gap:0", "overflow:hidden",
    "width:48px", "max-width:calc(100vw - 48px)", "max-height:48px", "padding:11px",
    `border:1px solid ${lw.lineStrong}`, "border-radius:50%", `background:${lw.surface}`,
    `color:${lw.text}`, "box-shadow:0 12px 32px rgba(0,0,0,.5)",
    `font-family:${sans}`, "-webkit-font-smoothing:antialiased",
    "cursor:pointer", "transition:width .18s ease,max-height .18s ease,padding .18s ease,border-radius .18s ease,gap .18s ease",
  ].join(";")

  const identity = doc.createElement("div")
  identity.style.cssText = "display:flex;align-items:center;gap:8px;min-height:24px;flex:none"
  const mark = doc.createElement("span")
  mark.setAttribute("aria-hidden", "true")
  mark.textContent = "♩"
  mark.style.cssText = `display:grid;place-items:center;flex:none;width:24px;height:24px;border-radius:6px;background:${lw.accent};color:#fff;font:600 15px/1 ${sans}`
  const brand = doc.createElement("span")
  brand.textContent = "legato"
  brand.style.cssText = `display:none;color:${lw.text2};font:500 12px/1 ${mono};letter-spacing:.02em`
  identity.append(mark, brand)

  const copy = doc.createElement("div")
  copy.style.cssText = "display:none;flex-direction:column;gap:4px;min-width:0"
  const title = doc.createElement("div")
  title.textContent = options.title
  title.style.cssText = `color:${lw.text};font:600 14px/1.35 ${sans}`
  const subtitle = doc.createElement("div")
  subtitle.textContent = options.subtitle
  subtitle.style.cssText = `overflow:hidden;color:${lw.text2};font:400 12px/1.4 ${mono};text-overflow:ellipsis;white-space:nowrap`
  copy.append(title, subtitle)

  const actionButtons: HTMLButtonElement[] = []
  const makeAction = (label: string): HTMLButtonElement => {
    const action = doc.createElement("button")
    action.type = "button"
    action.textContent = label
    action.style.cssText = [
      "box-sizing:border-box", "width:100%", "min-height:32px", "padding:6px 14px", "border-radius:6px",
      `font:500 13px/1.2 ${sans}`, "cursor:pointer", "transition:background-color .12s ease,filter .12s ease",
    ].join(";")
    action.addEventListener("mouseenter", () => {
      if (action.dataset.emphasis === "accent") action.style.filter = "brightness(1.1)"
      else action.style.backgroundColor = lw.raised
    })
    action.addEventListener("mouseleave", () => {
      action.style.filter = ""
      if (action.dataset.emphasis !== "accent") action.style.backgroundColor = "transparent"
    })
    actionButtons.push(action)
    return action
  }
  // One action carries the accent: the main one, or the one that waits for
  // confirmation. The others are quiet outlined buttons.
  const setEmphasis = (target: HTMLButtonElement): void => {
    for (const action of actionButtons) {
      const accent = action === target
      action.dataset.emphasis = accent ? "accent" : "quiet"
      action.style.backgroundColor = accent ? lw.accent : "transparent"
      action.style.border = `1px solid ${accent ? "transparent" : lw.lineStrong}`
      action.style.color = accent ? "#fff" : lw.text
      action.style.filter = ""
    }
  }

  const button = makeAction(options.actionLabel)
  const actions = doc.createElement("div")
  actions.style.cssText = "display:none;flex-direction:column;gap:6px;align-items:stretch"
  actions.append(button)
  setEmphasis(button)

  const addSecondaryAction = (label: string): HTMLButtonElement => {
    const secondary = makeAction(label)
    secondary.dataset.emphasis = "quiet"
    secondary.style.backgroundColor = "transparent"
    secondary.style.border = `1px solid ${lw.lineStrong}`
    secondary.style.color = lw.text
    actions.append(secondary)
    return secondary
  }

  const status = doc.createElement("div")
  status.setAttribute("role", "status")
  status.setAttribute("aria-live", "polite")
  status.style.cssText = `display:none;padding:8px 10px;border:1px solid ${lw.line};border-radius:6px;background:${lw.ground};color:${lw.text2};font:400 12px/1.5 ${sans};white-space:pre-line;overflow-wrap:anywhere`

  const setExpanded = (expanded: boolean): void => {
    root.dataset.expanded = String(expanded)
    root.setAttribute("aria-expanded", String(expanded))
    root.style.width = expanded ? "min(340px, calc(100vw - 48px))" : "48px"
    root.style.maxHeight = expanded ? "min(560px, calc(100vh - 48px))" : "48px"
    root.style.padding = expanded ? "14px" : "11px"
    root.style.borderRadius = expanded ? "12px" : "50%"
    root.style.gap = expanded ? "12px" : "0"
    root.style.borderColor = expanded ? lw.line : lw.lineStrong
    root.style.cursor = expanded ? "default" : "pointer"
    root.style.overflow = expanded ? "auto" : "hidden"
    brand.style.display = expanded ? "inline" : "none"
    copy.style.display = expanded ? "flex" : "none"
    actions.style.display = expanded ? "flex" : "none"
    status.style.display = expanded && status.textContent ? "block" : "none"
  }
  let handlingActionClick = false
  actions.addEventListener("click", () => {
    handlingActionClick = true
    setExpanded(true)
    queueMicrotask(() => { handlingActionClick = false })
  }, { capture: true })
  root.addEventListener("mouseenter", () => setExpanded(true))
  root.addEventListener("mouseleave", () => {
    if (!handlingActionClick && !root.contains(doc.activeElement)) setExpanded(false)
  })
  root.addEventListener("focusin", () => setExpanded(true))
  root.addEventListener("focusout", (event) => {
    if (!handlingActionClick && !root.contains(event.relatedTarget as Node | null)) setExpanded(false)
  })
  root.addEventListener("keydown", (event) => {
    if (event.key !== "Escape") return
    event.preventDefault()
    if (doc.activeElement instanceof HTMLElement) doc.activeElement.blur()
    setExpanded(false)
  })

  const setPrimaryVisible = (visible: boolean): void => {
    button.style.display = visible ? "block" : "none"
  }

  const setState = (state: LegatoFabState, message: string): void => {
    status.dataset.state = state
    status.textContent = message
    status.style.display = message && root.dataset.expanded === "true" ? "block" : "none"
    status.style.color = stateColors[state]
    button.disabled = state === "loading"
    button.style.cursor = state === "loading" ? "wait" : "pointer"
    button.style.opacity = state === "loading" ? ".6" : "1"
  }

  root.append(identity, copy, actions, status)
  return { root, title, subtitle, button, status, actions, addSecondaryAction, setEmphasis, setPrimaryVisible, setState }
}
