import { getToken } from "./auth"

/** The WebSocket address of an agent's live terminal on a Legato server. baseUrl is "" for this page's origin. */
export function terminalUrl(baseUrl: string, agentId: string, cols: number, rows: number): string {
  let wsBase: string
  let tokenKey: string | undefined
  if (!baseUrl) {
    wsBase = `${window.location.protocol === "https:" ? "wss:" : "ws:"}//${window.location.host}`
  } else {
    try {
      const parsed = new URL(baseUrl)
      wsBase = `${parsed.protocol === "https:" ? "wss:" : "ws:"}//${parsed.host}`
    } catch {
      wsBase = `ws://${baseUrl}`
    }
    tokenKey = baseUrl
  }
  const params = new URLSearchParams({ cols: String(cols), rows: String(rows) })
  const token = getToken(tokenKey)
  if (token) params.set("token", token)
  return `${wsBase}/api/agents/${encodeURIComponent(agentId)}/terminal?${params}`
}

/** The escape sequence xterm sends for one wheel notch (SGR mouse report), which tmux reads when mouse mode is on. */
export function wheelSequence(up: boolean): string {
  return `\x1b[<${up ? 64 : 65};1;1M`
}

/** Whole wheel notches in an accumulated touch drag, and the pixels left over. Dragging down scrolls up. */
export function touchWheel(accumulated: number, lineHeight: number): { notches: number; rest: number } {
  const notches = Math.trunc(accumulated / lineHeight)
  return { notches, rest: accumulated - notches * lineHeight }
}
