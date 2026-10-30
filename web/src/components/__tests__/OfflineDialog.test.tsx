import { describe, expect, it, vi, beforeEach, afterEach } from "vitest"
import { render, screen, fireEvent, cleanup, act } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { OfflineDialog } from "../OfflineDialog"

const mockSetActiveServer = vi.fn()
const mockReconnect = vi.fn()

let mockConnected = false
// The hook's own judgement: no connection for its lost window.
let mockLost = true
let mockGeneration = 1
let mockServers = [
  { name: "Remote A", url: "https://remote-a:3080" },
  { name: "Remote B", url: "https://remote-b:3080" },
]
let mockBaseUrl = "https://remote-a:3080"
let mockIsRemote = true

vi.mock("../../hooks/useWebSocket", () => ({
  useWebSocket: () => ({
    get connected() { return mockConnected },
    get lost() { return mockLost },
    get connectionGeneration() { return mockGeneration },
    reconnect: mockReconnect,
  }),
}))

vi.mock("../../hooks/useServer", () => ({
  useServer: () => ({
    get isRemote() { return mockIsRemote },
    get activeServerName() { return "Remote A" },
    get baseUrl() { return mockBaseUrl },
    get servers() { return mockServers },
    setActiveServer: mockSetActiveServer,
  }),
}))

function resetMocks() {
  mockConnected = false
  mockLost = true
  mockGeneration = 1
  mockServers = [
    { name: "Remote A", url: "https://remote-a:3080" },
    { name: "Remote B", url: "https://remote-b:3080" },
  ]
  mockBaseUrl = "https://remote-a:3080"
  mockIsRemote = true
}

function renderDialog(ui: React.ReactNode) {
  const result = render(<MemoryRouter>{ui}</MemoryRouter>)
  return {
    ...result,
    rerenderDialog: (next: React.ReactNode) => result.rerender(<MemoryRouter>{next}</MemoryRouter>),
  }
}

describe("OfflineDialog", () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    resetMocks()
  })

  afterEach(() => {
    cleanup()
    vi.useRealTimers()
    mockSetActiveServer.mockClear()
    mockReconnect.mockClear()
  })

  it("shows Reconnecting over the page while it reconnects, and no dialog", async () => {
    mockLost = false
    renderDialog(<><p>The open agent</p><OfflineDialog overlayDelayMs={100} /></>)
    expect(screen.queryByRole("status")).toBeNull()

    await act(async () => { vi.advanceTimersByTime(150) })

    expect(screen.getByRole("status").textContent).toMatch(/Reconnecting/)
    expect(screen.queryByRole("dialog")).toBeNull()
    expect(screen.getByText("The open agent")).toBeTruthy()
  })

  it("says Connecting before the page has ever connected", async () => {
    mockLost = false
    mockGeneration = 0
    renderDialog(<OfflineDialog overlayDelayMs={0} />)
    await act(async () => { vi.advanceTimersByTime(0) })

    expect(screen.getByRole("status").textContent).toMatch(/^Connecting/)
  })

  it("shows the Connection Lost dialog only once the connection is lost", async () => {
    mockLost = false
    const { rerenderDialog } = renderDialog(<OfflineDialog overlayDelayMs={0} />)
    await act(async () => { vi.advanceTimersByTime(0) })
    expect(screen.queryByText(/Connection Lost/i)).toBeNull()

    mockLost = true
    rerenderDialog(<OfflineDialog overlayDelayMs={0} />)

    expect(screen.getByRole("dialog", { name: /Connection Lost/i })).toBeTruthy()
    expect(screen.queryByRole("status")).toBeNull()
  })

  it("shows nothing when connected", async () => {
    mockConnected = true
    mockLost = false
    renderDialog(<OfflineDialog overlayDelayMs={0} />)
    await act(async () => { vi.advanceTimersByTime(150) })

    expect(screen.queryByText(/Connection Lost/i)).toBeNull()
    expect(screen.queryByRole("status")).toBeNull()
  })

  it("lists servers and calls setActiveServer on remote click", async () => {
    renderDialog(<OfflineDialog />)
    await act(async () => { vi.advanceTimersByTime(0) })

    const remoteBBtn = screen.getByRole("button", { name: /Remote B/i })
    expect(remoteBBtn).toBeTruthy()

    fireEvent.click(remoteBBtn)
    expect(mockSetActiveServer).toHaveBeenCalledWith("https://remote-b:3080")
  })

  it("calls setActiveServer('') when Local is clicked", async () => {
    renderDialog(<OfflineDialog />)
    await act(async () => { vi.advanceTimersByTime(0) })

    const localBtn = screen.getByRole("button", { name: /Local/i })
    fireEvent.click(localBtn)
    expect(mockSetActiveServer).toHaveBeenCalledWith("")
  })

  it("shows active styling on the currently selected server", async () => {
    renderDialog(<OfflineDialog />)
    await act(async () => { vi.advanceTimersByTime(0) })

    // Remote A is active: should have amber highlight classes
    const remoteABtn = screen.getByRole("button", { name: /^Remote A$/i })
    expect(remoteABtn.classList.contains("bg-amber-500/20")).toBe(true)

    // Remote B is inactive: should not have amber highlight
    const remoteBBtn = screen.getByRole("button", { name: /^Remote B$/i })
    expect(remoteBBtn.classList.contains("bg-amber-500/20")).toBe(false)
  })

  it("shows a Go to Settings link when no remote servers exist", async () => {
    mockServers = []
    renderDialog(<OfflineDialog />)
    await act(async () => { vi.advanceTimersByTime(0) })

    expect(screen.getByRole("link", { name: /Go to Settings/i })).toBeTruthy()
    expect(screen.queryByRole("button", { name: /Local/i })).toBeNull()
  })

  it("shows TLS hint for remote servers", async () => {
    renderDialog(<OfflineDialog />)
    await act(async () => { vi.advanceTimersByTime(0) })

    expect(screen.getByText(/TLS certificate is trusted/i)).toBeTruthy()
  })

  it("renders a dialog without replacing the underlying page", async () => {
    renderDialog(<><p>Cached plan queue</p><OfflineDialog /></>)
    await act(async () => { vi.advanceTimersByTime(0) })

    expect(screen.getByRole("dialog", { name: /Connection Lost/i })).toBeTruthy()
    expect(screen.getByText("Cached plan queue")).toBeTruthy()
  })

  it("closes automatically when the connection recovers", async () => {
    const { rerenderDialog } = renderDialog(<OfflineDialog />)
    await act(async () => { vi.advanceTimersByTime(0) })
    expect(screen.getByRole("dialog")).toBeTruthy()

    mockConnected = true
    mockLost = false
    rerenderDialog(<OfflineDialog />)

    expect(screen.queryByRole("dialog")).toBeNull()
  })

  it("cancels the Reconnecting notice when the connection recovers in time", async () => {
    mockLost = false
    const { rerenderDialog } = renderDialog(<OfflineDialog overlayDelayMs={100} />)

    mockConnected = true
    rerenderDialog(<OfflineDialog overlayDelayMs={100} />)
    await act(async () => { vi.advanceTimersByTime(150) })

    expect(screen.queryByRole("status")).toBeNull()
  })

  it("can be dismissed and reappears after a later disconnect", async () => {
    const { rerenderDialog } = renderDialog(<OfflineDialog />)
    await act(async () => { vi.advanceTimersByTime(0) })

    fireEvent.click(screen.getByRole("button", { name: /Dismiss connection warning/i }))
    expect(screen.queryByRole("dialog")).toBeNull()

    mockConnected = true
    mockLost = false
    rerenderDialog(<OfflineDialog />)
    mockConnected = false
    mockLost = true
    rerenderDialog(<OfflineDialog />)
    await act(async () => { vi.advanceTimersByTime(0) })

    expect(screen.getByRole("dialog", { name: /Connection Lost/i })).toBeTruthy()
  })

  it("dismisses with Escape and restores focus", async () => {
    // Focus is somewhere on the page when the connection is lost.
    mockLost = false
    mockConnected = true
    const { rerenderDialog } = renderDialog(<><button type="button">Open menu</button><OfflineDialog /></>)
    const opener = screen.getByRole("button", { name: "Open menu" })
    opener.focus()
    mockConnected = false
    mockLost = true
    rerenderDialog(<><button type="button">Open menu</button><OfflineDialog /></>)
    await act(async () => { vi.advanceTimersByTime(0) })

    expect(screen.getByRole("button", { name: /Dismiss connection warning/i })).toBe(document.activeElement)
    fireEvent.keyDown(document, { key: "Escape" })

    expect(screen.queryByRole("dialog")).toBeNull()
    expect(opener).toBe(document.activeElement)
  })

  it("has a Retry button that reconnects in place, without reloading the page", async () => {
    const reloadSpy = vi.fn()
    vi.stubGlobal("location", { ...window.location, reload: reloadSpy })

    renderDialog(<OfflineDialog />)
    await act(async () => { vi.advanceTimersByTime(0) })

    fireEvent.click(screen.getByRole("button", { name: /Retry/i }))
    expect(mockReconnect).toHaveBeenCalled()
    expect(reloadSpy).not.toHaveBeenCalled()

    vi.unstubAllGlobals()
  })
})
