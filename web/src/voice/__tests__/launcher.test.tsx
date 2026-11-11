import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import "@testing-library/jest-dom/vitest"
import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom"
import { VoiceLauncher } from "../VoiceLauncher"
import { VOICE_HASH } from "../status"
import type { AudioPort, SocketLike } from "../session"

function status(over: Record<string, unknown> = {}) {
  return { instance: { id: "srv-1", name: "tessitura", role: "main" }, ready: true, reason: "", setting: "", ...over }
}

function mockStatus(body: unknown) {
  vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } })))
}

class Socket implements SocketLike {
  static last: Socket | null = null
  binaryType = ""
  readyState = 0
  sent: string[] = []
  onopen: (() => void) | null = null
  onmessage: ((ev: { data: unknown }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  url: string
  constructor(url: string) { this.url = url; Socket.last = this; setTimeout(() => { this.readyState = 1; this.onopen?.() }, 0) }
  send(data: string | ArrayBuffer) { if (typeof data === "string") this.sent.push(data) }
  close() { this.readyState = 3 }
}

const stopCapture = vi.fn()
const audio: AudioPort = { canPlay: true, startCapture: async () => {}, stopCapture, play: async () => {}, stopPlayback: () => {} }

function Where() {
  const location = useLocation()
  return <div data-testid="where">{location.pathname + location.search}</div>
}

function open(props: Partial<Parameters<typeof VoiceLauncher>[0]> = {}) {
  window.location.hash = VOICE_HASH
  return render(<MemoryRouter><VoiceLauncher baseUrl="" audio={audio} socket={(url) => new Socket(url)} {...props} /></MemoryRouter>)
}

describe("the voice sheet in the web UI", () => {
  beforeEach(() => { localStorage.clear() })
  afterEach(() => { vi.unstubAllGlobals(); history.replaceState(null, "", "/") })

  it("stays closed without #reeve-voice", () => {
    history.replaceState(null, "", "/")
    render(<MemoryRouter><VoiceLauncher baseUrl="" audio={audio} /></MemoryRouter>)
    expect(screen.queryByTestId("voice-sheet")).toBeNull()
  })

  it("says why the voice is not ready, and records nothing", async () => {
    mockStatus(status({ ready: false, reason: "The Reeve's voice is off on tessitura", setting: "reeve.voice.enabled" }))
    Socket.last = null
    open()
    expect(await screen.findByText("The Reeve's voice is off on tessitura")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Open settings" })).toBeInTheDocument()
    expect(Socket.last).toBeNull()
  })

  it("on a worker's page says the voice is on the main", async () => {
    mockStatus(status({ instance: { id: "srv-2", name: "vm-1", role: "worker" } }))
    open()
    expect(await screen.findByText("Voice is on the main")).toBeInTheDocument()
  })

  it("says so when the page has no microphone", async () => {
    mockStatus(status())
    open({ audio: null })
    expect(await screen.findByText(/No microphone here/)).toBeInTheDocument()
  })

  it("when ready, opens a session with the token, says hello and listens", async () => {
    mockStatus(status())
    localStorage.setItem("legato-auth-token", "tok")
    open()
    await waitFor(() => expect(Socket.last?.sent.map((s) => JSON.parse(s).type)).toEqual(["hello", "listen"]))
    expect(Socket.last?.url).toMatch(/\/api\/reeve\/voice\/session/)
    Socket.last?.onmessage?.({ data: JSON.stringify({ type: "state", state: "listening" }) })
    Socket.last?.onmessage?.({ data: JSON.stringify({ type: "partial", text: "turn the" }) })
    expect(await screen.findByText("turn the")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Send now" }))
    expect(JSON.parse(Socket.last!.sent.at(-1)!)).toEqual({ type: "send" })
  })

  it("opened before the person touched the page, waits for Talk", async () => {
    mockStatus(status())
    vi.stubGlobal("navigator", { ...navigator, userActivation: { hasBeenActive: false, isActive: false } })
    Socket.last = null
    open()
    const talk = await screen.findByRole("button", { name: "Talk" })
    expect(Socket.last).toBeNull()
    fireEvent.click(talk)
    await waitFor(() => expect(Socket.last?.sent.map((s) => JSON.parse(s).type)).toEqual(["hello", "listen"]))
  })

  it("is a modal dialog: focus moves in, and Escape closes it", async () => {
    mockStatus(status())
    open()
    const sheet = await screen.findByRole("dialog", { name: "Talk to the Reeve" })
    expect(sheet).toHaveAttribute("aria-modal", "true")
    await waitFor(() => expect(sheet.contains(document.activeElement)).toBe(true))
    fireEvent.keyDown(document, { key: "Escape" })
    await waitFor(() => expect(screen.queryByTestId("voice-sheet")).toBeNull())
  })

  it("keeps the router's history state when it closes", async () => {
    mockStatus(status())
    open()
    history.replaceState({ idx: 3, key: "k" }, "", window.location.pathname + VOICE_HASH)
    fireEvent.click(await screen.findByRole("button", { name: /Cancel|Close/ }))
    await waitFor(() => expect(screen.queryByTestId("voice-sheet")).toBeNull())
    expect(history.state).toEqual({ idx: 3, key: "k" })
  })

  it("ends the session when the sheet is closed by its fragment going (Back)", async () => {
    mockStatus(status())
    stopCapture.mockClear()
    open()
    await waitFor(() => expect(Socket.last?.sent.map((m) => JSON.parse(m).type)).toEqual(["hello", "listen"]))
    history.back()
    window.location.hash = ""
    window.dispatchEvent(new HashChangeEvent("hashchange"))
    await waitFor(() => expect(screen.queryByTestId("voice-sheet")).toBeNull())
    expect(Socket.last?.sent.map((m) => JSON.parse(m).type)).toContain("bye")
    expect(stopCapture).toHaveBeenCalled()
  })

  it("starts nothing when closed while it was still checking the server", async () => {
    let answer!: (r: Response) => void
    vi.stubGlobal("fetch", vi.fn(() => new Promise<Response>((r) => { answer = r })))
    Socket.last = null
    open()
    fireEvent.click(await screen.findByRole("button", { name: "Close" }))
    answer(new Response(JSON.stringify(status()), { status: 200 }))
    await new Promise((r) => setTimeout(r, 20))
    expect(Socket.last).toBeNull()
  })

  it("Open conversation opens the Reeve's own conversation", async () => {
    vi.stubGlobal("fetch", vi.fn(async (url: string) => {
      const body = String(url).endsWith("/api/reeve") ? { current: { task_id: "reeve-1", state: "running" }, history: [] } : status()
      return new Response(JSON.stringify(body), { status: 200 })
    }))
    window.location.hash = VOICE_HASH
    render(<MemoryRouter><Routes><Route path="*" element={<><Where /><VoiceLauncher baseUrl="" audio={audio} socket={(url) => new Socket(url)} /></>} /></Routes></MemoryRouter>)
    await waitFor(() => expect(Socket.last?.sent.length).toBe(2))
    Socket.last?.onmessage?.({ data: JSON.stringify({ type: "written" }) })
    Socket.last?.onmessage?.({ data: JSON.stringify({ type: "state", state: "idle" }) })
    fireEvent.click(await screen.findByRole("button", { name: "Open conversation" }))
    await waitFor(() => expect(screen.getByTestId("where")).toHaveTextContent("/agents?task=reeve-1"))
  })
})
