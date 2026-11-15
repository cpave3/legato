import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { LiveTerminal } from "../LiveTerminal"
import { TerminalThemePicker } from "../settings/TerminalThemePicker"
import { TerminalPanel } from "../TerminalPanel"

// jsdom cannot draw a terminal: these stand in for xterm and record how it
// was made.
const { terminals, stream } = vi.hoisted(() => ({ terminals: [] as Array<{ theme?: { background?: string; green?: string } }>, stream: { connected: false, send: () => {}, subscribe: () => () => {} } }))
vi.mock("@xterm/xterm", () => ({
  Terminal: class {
    cols = 80
    rows = 24
    options: { theme?: { background?: string; green?: string } }
    constructor(options: { theme?: { background?: string; green?: string } }) { this.options = options; terminals.push(options) }
    loadAddon() {}
    open() {}
    write() {}
    reset() {}
    focus() {}
    dispose() {}
    onData() { return { dispose() {} } }
    onBinary() { return { dispose() {} } }
    onScroll() { return { dispose() {} } }
  },
}))
vi.mock("@xterm/addon-fit", () => ({ FitAddon: class { fit() {} } }))
vi.mock("@xterm/xterm/css/xterm.css", () => ({}))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useWebSocket", () => ({ useWebSocket: () => stream }))

class FakeSocket {
  static OPEN = 1
  static made: FakeSocket[] = []
  readyState = 0
  binaryType = ""
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  url: string
  constructor(url: string) {
    this.url = url
    FakeSocket.made.push(this)
  }
  send() {}
  close() {}
}

beforeEach(() => {
  terminals.length = 0
  FakeSocket.made = []
  localStorage.clear()
  vi.stubGlobal("WebSocket", FakeSocket)
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} })
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

it("draws in the terminal theme chosen in this browser", () => {
  localStorage.setItem("legato:terminal-theme", "night")
  render(<LiveTerminal agentId="abc" />)
  expect(terminals[0].theme?.background).toBe("#000000")
})

it("falls back to Legato's theme when the saved one is not offered", () => {
  localStorage.setItem("legato:terminal-theme", "neon")
  render(<LiveTerminal agentId="abc" />)
  expect(terminals[0].theme?.background).toBe("#0b0b10")
})

it("says it is reconnecting when the connection drops, and retries at once when asked", () => {
  render(<LiveTerminal agentId="abc" />)
  const socket = FakeSocket.made[0]
  act(() => {
    socket.readyState = 1
    socket.onopen?.()
  })
  expect(screen.queryByText(/reconnecting/i)).toBeNull()

  act(() => {
    socket.readyState = 3
    socket.onclose?.()
  })
  expect(screen.getByText(/reconnecting/i)).not.toBeNull()

  fireEvent.click(screen.getByRole("button", { name: "Retry now" }))
  expect(FakeSocket.made).toHaveLength(2)
})

it.each([
  { name: "Ocular Dark", id: "ocular-dark", green: "#A3C9A8" },
  { name: "Ocular Dark Vivid", id: "ocular-dark-vivid", green: "#61DD8A" },
])("updates mounted terminals to $name without reconnecting or losing their screens", ({ name, id, green }) => {
  render(<><TerminalThemePicker /><LiveTerminal agentId="abc" /><TerminalPanel agentId="def" /></>)
  fireEvent.click(screen.getByRole("radio", { name }))
  expect(terminals).toHaveLength(2)
  expect(FakeSocket.made).toHaveLength(1)
  expect(terminals[0].theme?.background).toBe("#20252B")
  expect(terminals[1].theme?.background).toBe("#20252B")
  expect(terminals[0].theme?.green).toBe(green)
  expect(terminals[1].theme?.green).toBe(green)
  expect(localStorage.getItem("legato:terminal-theme")).toBe(id)
  act(() => {
    localStorage.setItem("legato:terminal-theme", "ocular-light-vivid")
    window.dispatchEvent(new StorageEvent("storage", { key: "legato:terminal-theme" }))
  })
  expect(terminals).toHaveLength(2)
  expect(terminals[0].theme?.background).toBe("#F5F3ED")
  expect(terminals[1].theme?.background).toBe("#F5F3ED")
  expect(terminals[0].theme?.green).toBe("#167347")
  expect(terminals[1].theme?.green).toBe("#167347")
})
