import { describe, expect, it, vi } from "vitest"
import { createScopeLive, type LiveSocket } from "./scope-live"
import type { SentPin } from "./scope-types"

function fakeSocket(url: string) {
  const socket: LiveSocket & { url: string; closed: boolean } = {
    url, closed: false, onopen: null, onmessage: null, onclose: null,
    close: () => { socket.closed = true; socket.onclose?.() },
  }
  return socket
}

const pin = (id: string) => ({ id, state: "sent" }) as SentPin

function setup() {
  const sockets: ReturnType<typeof fakeSocket>[] = []
  const timers: Array<() => void> = []
  const deps = {
    target: vi.fn(async () => ({ baseUrl: "http://localhost:3080", token: "secret" })),
    connect: vi.fn((url: string) => { const socket = fakeSocket(url); sockets.push(socket); return socket }),
    fetchPins: vi.fn(async (origin: string) => [pin(`${origin}-1`)]),
    notify: vi.fn<(tabId: number, message: unknown) => Promise<unknown>>(async () => undefined),
    setTimeout: vi.fn((callback: () => void) => { timers.push(callback); return timers.length }),
    clearTimeout: vi.fn(),
  }
  return { deps, live: createScopeLive(deps), sockets, timers }
}

const settle = () => new Promise((resolve) => setTimeout(resolve, 0))

describe("following sent pins", () => {
  it("opens Legato's socket with the token when a tab watches, and refetches an origin's pins for its tabs on scope_changed", async () => {
    const { deps, live, sockets } = setup()
    live.watch(1, "http://localhost:5173")
    live.watch(2, "http://localhost:5173")
    live.watch(3, "https://other.test")
    await settle()
    expect(sockets).toHaveLength(1)
    expect(sockets[0].url).toBe("ws://localhost:3080/ws?token=secret")
    sockets[0].onopen?.()
    await settle()
    deps.notify.mockClear()
    deps.fetchPins.mockClear()
    sockets[0].onmessage?.({ data: JSON.stringify({ type: "scope_changed", origin: "http://localhost:5173" }) })
    await settle()
    expect(deps.fetchPins).toHaveBeenCalledTimes(1)
    expect(deps.fetchPins).toHaveBeenCalledWith("http://localhost:5173")
    expect(deps.notify.mock.calls).toEqual([
      [1, { type: "scopePinsChanged", origin: "http://localhost:5173", pins: [pin("http://localhost:5173-1")] }],
      [2, { type: "scopePinsChanged", origin: "http://localhost:5173", pins: [pin("http://localhost:5173-1")] }],
    ])
  })

  it("ignores other messages and origins no tab watches", async () => {
    const { deps, live, sockets } = setup()
    live.watch(1, "http://localhost:5173")
    await settle()
    deps.fetchPins.mockClear()
    sockets[0].onmessage?.({ data: JSON.stringify({ type: "scope_changed", origin: "https://nobody.test" }) })
    sockets[0].onmessage?.({ data: JSON.stringify({ type: "cards_changed" }) })
    sockets[0].onmessage?.({ data: "not json" })
    await settle()
    expect(deps.fetchPins).not.toHaveBeenCalled()
  })

  it("reconnects after a drop and refetches every watched origin, as changes may have been missed", async () => {
    const { deps, live, sockets, timers } = setup()
    live.watch(1, "http://localhost:5173")
    await settle()
    sockets[0].onopen?.()
    sockets[0].onclose?.()
    expect(timers).toHaveLength(1)
    timers[0]()
    await settle()
    expect(sockets).toHaveLength(2)
    deps.fetchPins.mockClear()
    sockets[1].onopen?.()
    await settle()
    expect(deps.fetchPins).toHaveBeenCalledWith("http://localhost:5173")
  })

  it("forgets a tab that closed or left, and closes the socket when no tab watches", async () => {
    const { deps, live, sockets, timers } = setup()
    live.watch(1, "http://localhost:5173")
    live.watch(2, "http://localhost:5173")
    await settle()
    deps.notify.mockImplementation(async (tabId: number) => { if (tabId === 2) throw new Error("No receiving end") })
    await live.refresh("http://localhost:5173")
    expect(live.watching()).toEqual([1])
    live.unwatch(1)
    expect(sockets[0].closed).toBe(true)
    expect(timers).toHaveLength(0)
  })
})
