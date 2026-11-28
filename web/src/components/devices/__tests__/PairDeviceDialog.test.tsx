import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { PairDeviceDialog } from "../PairDeviceDialog"

type Handler = (msg: { type: string }) => void
const handlers = new Set<Handler>()
vi.mock("../../../hooks/useWebSocket", () => ({
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

  it("shows the request's purpose when a main instance's request replaced a phone's", async () => {
    render(<PairDeviceDialog baseUrl="" onClose={() => {}} />)
    await screen.findByText("river")
    serverSays({ ...open, purpose: "main", pair_uri: undefined, address: "https://vm-1.lan:3080", phrase: ["acid", "acorn", "acre", "acts"] })
    expect(await screen.findByText(/now pairs a main instance/)).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Show QR code" })).toBeNull()
  })

  it("pairs a main instance: the purpose, this server's address and no QR code", async () => {
    current = { ...open, purpose: "main", pair_uri: undefined, address: "https://vm-1.lan:3080" }
    render(<PairDeviceDialog baseUrl="" purpose="main" onClose={() => {}} />)
    expect(await screen.findByText("river")).toBeTruthy()
    expect(screen.getByRole("heading", { name: "Pair a main instance" })).toBeTruthy()
    expect(screen.getByText("https://vm-1.lan:3080")).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Show QR code" })).toBeNull()
    serverSays({ state: "paired", purpose: "main", device: "workstation", attempts_left: 5 })
    expect(await screen.findByText(/Paired with main instance workstation/)).toBeTruthy()
  })
})

it("traps pairing focus and Escape cancels its open phrase", async () => {
  const close = vi.fn()
  render(<PairDeviceDialog baseUrl="" onClose={close} />)
  await screen.findByText("river")
  screen.getByRole("button", {name:"Done"}).focus()
  fireEvent.keyDown(document.activeElement!, {key:"Tab"})
  expect(document.activeElement).toBe(screen.getByRole("button", {name:"Close"}))
  fireEvent.keyDown(document.activeElement!, {key:"Escape"})
  expect(close).toHaveBeenCalledTimes(1)
  await waitFor(() => expect(methods).toContain("DELETE"))
})

describe("PairDeviceDialog states", () => {
  it("asks for a phrase first, and opens a phone's request with no body", async () => {
    let finish!: () => void
    const fetchMock = vi.fn<(url: string, init?: RequestInit) => Promise<Response>>(() => new Promise<Response>((resolve) => { finish = () => resolve(json(open)) }))
    vi.stubGlobal("fetch", fetchMock)
    render(<PairDeviceDialog baseUrl="" onClose={() => {}} />)
    expect(screen.getByText("Asking the server for a phrase…")).toBeTruthy()
    expect(screen.getByRole("heading", { name: "Pair a device" })).toBeTruthy()
    expect(screen.getByText("In the Legato app, add a server, choose this one and type the words.")).toBeTruthy()
    expect(fetchMock.mock.calls[0][1]?.method).toBe("POST")
    expect(fetchMock.mock.calls[0][1]?.body).toBeUndefined()
    finish()
    expect(await screen.findByText("Waiting for a device…")).toBeTruthy()
    expect(screen.getByText("Scan it in the Legato app, or type the words.")).toBeTruthy()
    expect(screen.queryByText(/tries left/)).toBeNull()
    expect(screen.queryByRole("button", { name: "New phrase" })).toBeNull()
  })

  it("pairs another Reeve: its purpose in the request, its words and its outcome", async () => {
    current = { ...open, purpose: "reeve", pair_uri: undefined, address: "https://vm-2.lan:3080", attempts_left: 3 }
    render(<PairDeviceDialog baseUrl="" purpose="reeve" onClose={() => {}} />)
    expect(await screen.findByText("Waiting for the other instance…")).toBeTruthy()
    expect(screen.getByRole("heading", { name: "Pair another Reeve" })).toBeTruthy()
    expect(screen.getByText(/Settings › Reeve › Reeve pairings/)).toBeTruthy()
    expect(screen.getByText("https://vm-2.lan:3080")).toBeTruthy()
    expect(screen.getByText("3 tries left")).toBeTruthy()
    const post = vi.mocked(fetch).mock.calls[0][1]
    expect(post).toEqual({ method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ purpose: "reeve" }) })
    serverSays({ state: "paired", purpose: "reeve", attempts_left: 3 })
    expect(await screen.findByText("Paired with the Reeve on the other instance.")).toBeTruthy()
    serverSays({ state: "paired", purpose: "reeve", device: "erebus", attempts_left: 3 })
    expect(await screen.findByText("Paired with the Reeve on erebus.")).toBeTruthy()
  })

  it("tells a main instance where to type the phrase and waits for one", async () => {
    current = { ...open, purpose: "main", pair_uri: undefined }
    render(<PairDeviceDialog baseUrl="" purpose="main" onClose={() => {}} />)
    expect(await screen.findByText("Waiting for a main instance…")).toBeTruthy()
    expect(screen.getByText(/Settings › Connections › Workers › Add a worker/)).toBeTruthy()
    expect(screen.queryByText("Address")).toBeNull()
  })

  it("names a device that paired without a name", async () => {
    render(<PairDeviceDialog baseUrl="" onClose={() => {}} />)
    await screen.findByText("river")
    serverSays({ state: "paired", attempts_left: 5 })
    expect(await screen.findByText("Paired with a device.")).toBeTruthy()
  })

  it("says why a phrase ended", async () => {
    render(<PairDeviceDialog baseUrl="" onClose={() => {}} />)
    await screen.findByText("river")
    serverSays({ state: "expired", attempts_left: 5 })
    expect(await screen.findByText("The phrase expired before a device used it.")).toBeTruthy()
    serverSays({ state: "cancelled", attempts_left: 5 })
    expect(await screen.findByText("This phrase was cancelled.")).toBeTruthy()
    expect(screen.getByRole("button", { name: "New phrase" })).toBeTruthy()
  })

  it("reads the request again when its countdown runs out", async () => {
    current = { ...open, expires_at: new Date(Date.now() - 1000).toISOString() }
    render(<PairDeviceDialog baseUrl="" onClose={() => {}} />)
    await waitFor(() => expect(methods).toContain("GET"))
    expect(screen.getByText("Expires in 0:00")).toBeTruthy()
  })

  it("shows the server's refusal, or its silence", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => json({ error: "busy pairing" }, 409)))
    render(<PairDeviceDialog baseUrl="" onClose={() => {}} />)
    expect((await screen.findByRole("alert")).textContent).toBe("busy pairing")
    expect(screen.queryByText(/Asking the server/)).toBeNull()
    cleanup()
    vi.stubGlobal("fetch", vi.fn(async () => new Response("nope", { status: 500 })))
    render(<PairDeviceDialog baseUrl="" onClose={() => {}} />)
    expect((await screen.findByRole("alert")).textContent).toBe("The server answered 500.")
    cleanup()
    vi.stubGlobal("fetch", vi.fn(async () => { throw new Error("down") }))
    render(<PairDeviceDialog baseUrl="" onClose={() => {}} />)
    expect((await screen.findByRole("alert")).textContent).toBe("The server did not answer.")
  })

  it("closes without cancelling once paired, from Done or the backdrop", async () => {
    const onClose = vi.fn()
    const view = render(<PairDeviceDialog baseUrl="" onClose={onClose} />)
    await screen.findByText("river")
    serverSays({ state: "paired", device: "Pixel", attempts_left: 5 })
    await screen.findByText(/Paired with Pixel/)
    fireEvent.click(screen.getByRole("button", { name: "Done" }))
    fireEvent.click(view.container.firstChild as HTMLElement)
    fireEvent.click(screen.getByRole("dialog"))
    expect(onClose).toHaveBeenCalledTimes(2)
    expect(methods).not.toContain("DELETE")
  })
})
