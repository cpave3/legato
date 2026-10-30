import { describe, expect, it, vi, beforeEach, afterEach } from "vitest"
import { renderHook, act, cleanup } from "@testing-library/react"
import { useWebSocketProvider } from "../useWebSocket"

// FakeWebSocket records each socket the hook opens; a test opens or closes
// it as the network would.
class FakeWebSocket {
  static OPEN = 1
  static CONNECTING = 0
  static CLOSED = 3
  static made: FakeWebSocket[] = []
  readyState = FakeWebSocket.CONNECTING
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  url: string
  constructor(url: string) {
    this.url = url
    FakeWebSocket.made.push(this)
  }
  open() {
    this.readyState = FakeWebSocket.OPEN
    this.onopen?.()
  }
  drop() {
    this.readyState = FakeWebSocket.CLOSED
    this.onclose?.()
  }
  send() {}
  close() {
    this.readyState = FakeWebSocket.CLOSED
  }
}

const latest = () => FakeWebSocket.made[FakeWebSocket.made.length - 1]

describe("useWebSocketProvider", () => {
  beforeEach(() => {
    vi.useFakeTimers()
    FakeWebSocket.made = []
    vi.stubGlobal("WebSocket", FakeWebSocket)
    vi.stubGlobal("fetch", vi.fn(async () => ({ ok: false })))
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  it("reconnects at once when the page comes back into view, without waiting out its backoff", () => {
    renderHook(() => useWebSocketProvider("ws://localhost/ws"))
    act(() => latest().open())
    // Several failures in the background push the next try far out.
    act(() => latest().drop())
    act(() => { vi.advanceTimersByTime(1000) })
    act(() => latest().drop())
    act(() => { vi.advanceTimersByTime(2000) })
    act(() => latest().drop())
    const before = FakeWebSocket.made.length

    act(() => {
      Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" })
      document.dispatchEvent(new Event("visibilitychange"))
    })

    expect(FakeWebSocket.made.length).toBe(before + 1)
  })

  it("reconnects at once when the mobile app returns to the foreground, and leaves an open socket alone", () => {
    renderHook(() => useWebSocketProvider("ws://localhost/ws"))
    act(() => latest().open())
    act(() => { window.dispatchEvent(new Event("legato:shell-return")) })
    expect(FakeWebSocket.made.length).toBe(1)

    act(() => latest().drop())
    act(() => { window.dispatchEvent(new Event("legato:shell-return")) })
    expect(FakeWebSocket.made.length).toBe(2)
  })

  it("counts the connection as lost only after ten seconds without one", () => {
    const { result } = renderHook(() => useWebSocketProvider("ws://localhost/ws"))
    act(() => latest().open())

    // A drop that reconnects in time is never lost.
    act(() => latest().drop())
    act(() => { vi.advanceTimersByTime(1000) })
    act(() => latest().open())
    act(() => { vi.advanceTimersByTime(15000) })
    expect(result.current.lost).toBe(false)

    // One that does not is lost at ten seconds, and found again on connect.
    act(() => latest().drop())
    act(() => { vi.advanceTimersByTime(9900) })
    expect(result.current.lost).toBe(false)
    act(() => { vi.advanceTimersByTime(200) })
    expect(result.current.lost).toBe(true)
    act(() => { result.current.reconnect() })
    act(() => latest().open())
    expect(result.current.lost).toBe(false)
  })
})
