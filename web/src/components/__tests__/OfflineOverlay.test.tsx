import { describe, expect, it, vi, beforeEach, afterEach } from "vitest"
import { render, screen, fireEvent, cleanup, act } from "@testing-library/react"
import { OfflineOverlay } from "../OfflineOverlay"

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

describe("OfflineOverlay", () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    resetMocks()
  })

  afterEach(() => {
    cleanup()
    vi.useRealTimers()
    mockSetActiveServer.mockClear()
  })

  it("shows the banner after the delay", async () => {
    render(<OfflineOverlay delayMs={100} />)
    expect(screen.queryByText(/Connection Lost/i)).toBeNull()

    await act(async () => { vi.advanceTimersByTime(150) })

    expect(screen.queryByText(/Connection Lost/i)).toBeTruthy()
  })

  it("does NOT show when connected", async () => {
    mockConnected = true
    render(<OfflineOverlay delayMs={100} />)
    await act(async () => { vi.advanceTimersByTime(150) })

    expect(screen.queryByText(/Connection Lost/i)).toBeNull()
  })

  it("lists servers and calls setActiveServer on remote click", async () => {
    render(<OfflineOverlay delayMs={0} />)
    await act(async () => { vi.advanceTimersByTime(0) })

    const remoteBBtn = screen.getByRole("button", { name: /Remote B/i })
    expect(remoteBBtn).toBeTruthy()

    fireEvent.click(remoteBBtn)
    expect(mockSetActiveServer).toHaveBeenCalledWith("https://remote-b:3080")
  })

  it("calls setActiveServer('') when Local is clicked", async () => {
    render(<OfflineOverlay delayMs={0} />)
    await act(async () => { vi.advanceTimersByTime(0) })

    const localBtn = screen.getByRole("button", { name: /Local/i })
    fireEvent.click(localBtn)
    expect(mockSetActiveServer).toHaveBeenCalledWith("")
  })

  it("shows active styling on the currently selected server", async () => {
    render(<OfflineOverlay delayMs={0} />)
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
    render(<OfflineOverlay delayMs={0} />)
    await act(async () => { vi.advanceTimersByTime(0) })

    expect(screen.getByRole("link", { name: /Go to Settings/i })).toBeTruthy()
    expect(screen.queryByRole("button", { name: /Local/i })).toBeNull()
  })

  it("shows TLS hint for remote servers", async () => {
    render(<OfflineOverlay delayMs={0} />)
    await act(async () => { vi.advanceTimersByTime(0) })

    expect(screen.getByText(/TLS certificate is trusted/i)).toBeTruthy()
  })

  it("is sticky, not full-screen blocking", async () => {
    render(<OfflineOverlay delayMs={0} />)
    await act(async () => { vi.advanceTimersByTime(0) })

    const banner = screen.getByRole("alert")
    expect(banner.classList.contains("sticky")).toBe(true)
    expect(banner.classList.contains("inset-0")).toBe(false)
    expect(banner.classList.contains("fixed")).toBe(false)
  })

  it("has a Retry button that triggers a page reload", async () => {
    const reloadSpy = vi.fn()
    vi.stubGlobal("location", { ...window.location, reload: reloadSpy })

    render(<OfflineOverlay delayMs={0} />)
    await act(async () => { vi.advanceTimersByTime(0) })

    fireEvent.click(screen.getByRole("button", { name: /Retry/i }))
    expect(reloadSpy).toHaveBeenCalled()

    vi.unstubAllGlobals()
  })
})
