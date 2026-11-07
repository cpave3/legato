import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { LiveTerminal } from "../LiveTerminal"

// jsdom cannot draw a terminal: these stand in for xterm and record how it
// was made.
const { terminals } = vi.hoisted(() => ({ terminals: [] as Array<{ theme?: { background?: string } }> }))
vi.mock("@xterm/xterm", () => ({
  Terminal: class {
    cols = 80
    rows = 24
    constructor(options: { theme?: { background?: string } }) { terminals.push(options) }
    loadAddon() {}
    open() {}
    write() {}
    reset() {}
    focus() {}
    dispose() {}
    onData() { return { dispose() {} } }
    onBinary() { return { dispose() {} } }
  },
}))
vi.mock("@xterm/addon-fit", () => ({ FitAddon: class { fit() {} } }))
vi.mock("@xterm/xterm/css/xterm.css", () => ({}))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))

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
