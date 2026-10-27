import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { PairDeviceDialog } from "../PairDeviceDialog"

type Handler = (msg: { type: string }) => void
const handlers = new Set<Handler>()
vi.mock("../../hooks/useWebSocket", () => ({
  useWebSocket: () => ({
    connected: true,
    send: vi.fn(),
    subscribe: (h: Handler) => {
      handlers.add(h)
      return () => handlers.delete(h)
    },
  }),
}))

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
}

const open = {
  state: "open",
  phrase: ["river", "candle", "otter", "mint"],
  expires_at: new Date(Date.now() + 5 * 60_000).toISOString(),
  attempts_left: 5,
  pair_uri: "legato://pair?phrase=river-candle-otter-mint&url=http%3A%2F%2F192.168.1.200%3A3080",
}
let current: Record<string, unknown>
let methods: string[]

beforeEach(() => {
  current = open
  methods = []
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    const method = init?.method ?? "GET"
    if (String(url).endsWith("/api/pairing")) methods.push(method)
    if (method === "DELETE") current = { state: "cancelled", attempts_left: 5 }
    return json(current)
  }))
})

afterEach(() => {
  cleanup()
  handlers.clear()
  vi.unstubAllGlobals()
})

function serverSays(status: Record<string, unknown>) {
  current = status
  act(() => handlers.forEach((h) => h({ type: "pairing_changed" })))
}

describe("PairDeviceDialog", () => {
  it("opens a request and shows its phrase", async () => {
    render(<PairDeviceDialog baseUrl="" onClose={() => {}} />)
    expect(await screen.findByText("river")).toBeTruthy()
    expect(screen.getByText("mint")).toBeTruthy()
    expect(screen.getByText(/Expires in [45]:\d\d/)).toBeTruthy()
    expect(methods[0]).toBe("POST")
  })

  it("shows the QR code of the phrase blurred until it is clicked", async () => {
    render(<PairDeviceDialog baseUrl="" onClose={() => {}} />)
    await screen.findByText("river")
    expect(screen.queryByRole("img", { name: /QR code/ })).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Show QR code" }))
    const code = screen.getByRole("img", { name: /QR code/ })
    expect(code.querySelector("path")?.getAttribute("d")).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Show QR code" })).toBeNull()
  })

  it("has no QR code when the server names no address a phone could use", async () => {
    current = { ...open, pair_uri: undefined }
    render(<PairDeviceDialog baseUrl="" onClose={() => {}} />)
    await screen.findByText("river")
    expect(screen.queryByRole("button", { name: "Show QR code" })).toBeNull()
  })

  it("says which device paired when it does", async () => {
    render(<PairDeviceDialog baseUrl="" onClose={() => {}} />)
    await screen.findByText("river")
    serverSays({ state: "paired", device: "Pixel 4 XL", attempts_left: 4 })
    expect(await screen.findByText(/Paired with Pixel 4 XL/)).toBeTruthy()
    expect(screen.queryByText("river")).toBeNull()
  })

  it("offers a new phrase when this one can no longer be used", async () => {
    render(<PairDeviceDialog baseUrl="" onClose={() => {}} />)
    await screen.findByText("river")
    serverSays({ state: "exhausted", attempts_left: 0 })
    expect(await screen.findByText(/Too many wrong phrases/)).toBeTruthy()
    current = { ...open, phrase: ["acid", "acorn", "acre", "acts"] }
    fireEvent.click(screen.getByRole("button", { name: "New phrase" }))
    expect(await screen.findByText("acorn")).toBeTruthy()
  })

  it("cancels the request when closed before a device paired", async () => {
    const onClose = vi.fn()
    render(<PairDeviceDialog baseUrl="" onClose={onClose} />)
    await screen.findByText("river")
    fireEvent.click(screen.getByRole("button", { name: "Close" }))
    await waitFor(() => expect(methods).toContain("DELETE"))
    expect(onClose).toHaveBeenCalled()
  })

  it("says so when the server cannot pair by phrase", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => json({ error: "this server does not pair by phrase" }, 404)))
    render(<PairDeviceDialog baseUrl="" onClose={() => {}} />)
    expect(await screen.findByText(/cannot pair by phrase/)).toBeTruthy()
  })
})
