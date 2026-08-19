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
  setState: (state: LegatoFabState, message: string) => void
}

const stateColors: Record<LegatoFabState, string> = {
  idle: "#a1a1aa",
  loading: "#c4b5fd",
  success: "#67e8f9",
  error: "#fca5a5",
}

export function createLegatoFab(doc: Document, options: LegatoFabOptions): LegatoFab {
  const root = doc.createElement("div")
  root.id = options.id
  root.dataset.legatoFab = "true"
  root.style.cssText = [
    "all:initial", "position:fixed", "right:24px", "bottom:24px", "z-index:2147483647",
    "box-sizing:border-box", "display:flex", "flex-direction:column", "gap:12px",
    "width:min(360px,calc(100vw - 48px))", "padding:16px", "border:1px solid rgba(139,92,246,.55)",
    "border-radius:12px", "background:linear-gradient(145deg,#181827 0%,#11111b 60%,#171225 100%)",
    "color:#f4f4f5", "box-shadow:0 16px 40px rgba(0,0,0,.42),0 0 0 1px rgba(196,181,253,.06) inset",
    "font-family:Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,Segoe UI,sans-serif",
  ].join(";")

  const identity = doc.createElement("div")
  identity.style.cssText = "display:flex;align-items:center;gap:9px"
  const mark = doc.createElement("span")
  mark.setAttribute("aria-hidden", "true")
  mark.textContent = "L"
  mark.style.cssText = "display:grid;place-items:center;width:24px;height:24px;border-radius:7px;background:linear-gradient(135deg,#8b5cf6,#6366f1);color:#fff;font:800 13px/1 system-ui;box-shadow:0 0 14px rgba(139,92,246,.4)"
  const brand = doc.createElement("span")
  brand.textContent = "LEGATO"
  brand.style.cssText = "color:#c4b5fd;font:700 11px/1 system-ui;letter-spacing:.16em"
  identity.append(mark, brand)

  const copy = doc.createElement("div")
  copy.style.cssText = "display:flex;flex-direction:column;gap:3px;min-width:0"
  const title = doc.createElement("div")
  title.textContent = options.title
  title.style.cssText = "color:#fafafa;font:700 15px/1.35 system-ui"
  const subtitle = doc.createElement("div")
  subtitle.textContent = options.subtitle
  subtitle.style.cssText = "overflow:hidden;color:#a1a1aa;font:12px/1.4 ui-monospace,SFMono-Regular,Menlo,monospace;text-overflow:ellipsis;white-space:nowrap"
  copy.append(title, subtitle)

  const button = doc.createElement("button")
  button.type = "button"
  button.textContent = options.actionLabel
  button.style.cssText = "box-sizing:border-box;width:100%;min-height:38px;padding:8px 12px;border:1px solid rgba(167,139,250,.75);border-radius:8px;background:linear-gradient(180deg,#7c3aed,#6d28d9);color:#fff;font:700 13px/1.2 system-ui;cursor:pointer;box-shadow:0 5px 14px rgba(76,29,149,.35)"

  const status = doc.createElement("div")
  status.setAttribute("role", "status")
  status.setAttribute("aria-live", "polite")
  status.style.cssText = "display:none;color:#a1a1aa;font:12px/1.5 system-ui;white-space:pre-line;overflow-wrap:anywhere"

  const setState = (state: LegatoFabState, message: string): void => {
    status.dataset.state = state
    status.textContent = message
    status.style.display = message ? "block" : "none"
    status.style.color = stateColors[state]
    button.disabled = state === "loading"
    button.style.cursor = state === "loading" ? "wait" : "pointer"
    button.style.opacity = state === "loading" ? ".72" : "1"
  }

  root.append(identity, copy, button, status)
  return { root, title, subtitle, button, status, setState }
}
