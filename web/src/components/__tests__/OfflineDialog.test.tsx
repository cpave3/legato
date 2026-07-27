import { describe, expect, it, vi, beforeEach, afterEach } from "vitest"
import { render, screen, fireEvent, cleanup, act } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { OfflineDialog } from "../OfflineDialog"

const mockSetActiveServer = vi.fn()

let mockConnected = false
let mockServers = [
  { name: "Remote A", url: "https://remote-a:3080" },
  { name: "Remote B", url: "https://remote-b:3080" },
]
let mockBaseUrl = "https://remote-a:3080"
let mockIsRemote = true

vi.mock("../../hooks/useWebSocket", () => ({
  useWebSocket: () => ({ get connected() { return mockConnected } }),
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
  })

  it("shows the dialog after the delay", async () => {
    renderDialog(<OfflineDialog delayMs={100} />)
    expect(screen.queryByText(/Connection Lost/i)).toBeNull()

    await act(async () => { vi.advanceTimersByTime(150) })

    expect(screen.queryByText(/Connection Lost/i)).toBeTruthy()
  })

  it("does NOT show when connected", async () => {
    mockConnected = true
    renderDialog(<OfflineDialog delayMs={100} />)
    await act(async () => { vi.advanceTimersByTime(150) })

    expect(screen.queryByText(/Connection Lost/i)).toBeNull()
  })

  it("lists servers and calls setActiveServer on remote click", async () => {
    renderDialog(<OfflineDialog delayMs={0} />)
    await act(async () => { vi.advanceTimersByTime(0) })

    const remoteBBtn = screen.getByRole("button", { name: /Remote B/i })
    expect(remoteBBtn).toBeTruthy()

    fireEvent.click(remoteBBtn)
    expect(mockSetActiveServer).toHaveBeenCalledWith("https://remote-b:3080")
  })

  it("calls setActiveServer('') when Local is clicked", async () => {
    renderDialog(<OfflineDialog delayMs={0} />)
    await act(async () => { vi.advanceTimersByTime(0) })

    const localBtn = screen.getByRole("button", { name: /Local/i })
    fireEvent.click(localBtn)
    expect(mockSetActiveServer).toHaveBeenCalledWith("")
  })

  it("shows active styling on the currently selected server", async () => {
    renderDialog(<OfflineDialog delayMs={0} />)
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
    renderDialog(<OfflineDialog delayMs={0} />)
    await act(async () => { vi.advanceTimersByTime(0) })

    expect(screen.getByRole("link", { name: /Go to Settings/i })).toBeTruthy()
    expect(screen.queryByRole("button", { name: /Local/i })).toBeNull()
  })

  it("shows TLS hint for remote servers", async () => {
    renderDialog(<OfflineDialog delayMs={0} />)
    await act(async () => { vi.advanceTimersByTime(0) })

    expect(screen.getByText(/TLS certificate is trusted/i)).toBeTruthy()
  })

  it("renders a dialog without replacing the underlying page", async () => {
    renderDialog(<><p>Cached plan queue</p><OfflineDialog delayMs={0} /></>)
    await act(async () => { vi.advanceTimersByTime(0) })

    expect(screen.getByRole("dialog", { name: /Connection Lost/i })).toBeTruthy()
    expect(screen.getByText("Cached plan queue")).toBeTruthy()
  })

  it("closes automatically when the connection recovers", async () => {
    const { rerenderDialog } = renderDialog(<OfflineDialog delayMs={0} />)
    await act(async () => { vi.advanceTimersByTime(0) })
    expect(screen.getByRole("dialog")).toBeTruthy()

    mockConnected = true
    rerenderDialog(<OfflineDialog delayMs={0} />)

    expect(screen.queryByRole("dialog")).toBeNull()
  })

  it("cancels the delayed dialog when the connection recovers", async () => {
    const { rerenderDialog } = renderDialog(<OfflineDialog delayMs={100} />)

    mockConnected = true
    rerenderDialog(<OfflineDialog delayMs={100} />)
    await act(async () => { vi.advanceTimersByTime(150) })

    expect(screen.queryByRole("dialog")).toBeNull()
  })

  it("can be dismissed and reappears after a later disconnect", async () => {
    const { rerenderDialog } = renderDialog(<OfflineDialog delayMs={0} />)
    await act(async () => { vi.advanceTimersByTime(0) })

    fireEvent.click(screen.getByRole("button", { name: /Dismiss connection warning/i }))
    expect(screen.queryByRole("dialog")).toBeNull()

    mockConnected = true
    rerenderDialog(<OfflineDialog delayMs={0} />)
    mockConnected = false
    rerenderDialog(<OfflineDialog delayMs={0} />)
    await act(async () => { vi.advanceTimersByTime(0) })

    expect(screen.getByRole("dialog", { name: /Connection Lost/i })).toBeTruthy()
  })

  it("dismisses with Escape and restores focus", async () => {
    renderDialog(<><button type="button">Open menu</button><OfflineDialog delayMs={0} /></>)
    const opener = screen.getByRole("button", { name: "Open menu" })
    opener.focus()
    await act(async () => { vi.advanceTimersByTime(0) })

    expect(screen.getByRole("button", { name: /Dismiss connection warning/i })).toBe(document.activeElement)
    fireEvent.keyDown(document, { key: "Escape" })

    expect(screen.queryByRole("dialog")).toBeNull()
    expect(opener).toBe(document.activeElement)
  })

  it("has a Retry button that triggers a page reload", async () => {
    const reloadSpy = vi.fn()
    vi.stubGlobal("location", { ...window.location, reload: reloadSpy })

    renderDialog(<OfflineDialog delayMs={0} />)
    await act(async () => { vi.advanceTimersByTime(0) })

    fireEvent.click(screen.getByRole("button", { name: /Retry/i }))
    expect(reloadSpy).toHaveBeenCalled()

    vi.unstubAllGlobals()
  })
})
