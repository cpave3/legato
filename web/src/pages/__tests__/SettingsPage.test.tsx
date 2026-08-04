import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { apiFetch } from "../../lib/api"
import { SettingsPage } from "../Settings"

const addToast = vi.fn()
const useSettings = vi.fn()

vi.mock("../../hooks/useServer", () => ({
  useServer: () => ({
    baseUrl: "https://remote.example",
    servers: [],
    addServer: vi.fn(),
    removeServer: vi.fn(),
    setActiveServer: vi.fn(),
  }),
}))
vi.mock("../../hooks/useSettings", () => ({ useSettings: () => useSettings() }))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast }) }))
vi.mock("../../lib/api", () => ({ apiFetch: vi.fn() }))

const mockedApiFetch = vi.mocked(apiFetch)

function settings(hotReloadEnabled: boolean) {
  return {
    data: {
      ca_cert_available: false,
      ntfy_configured: false,
      voice_enabled: false,
      voice_auto_send: false,
      worktrees_enabled: false,
      hot_reload_enabled: hotReloadEnabled,
      working_dir: "/projects/legato",
      repositories: [],
    },
  }
}

function renderPage() {
  return render(<MemoryRouter><SettingsPage /></MemoryRouter>)
}

describe("SettingsPage hot reload build", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    useSettings.mockReturnValue(settings(false))
  })

  afterEach(() => cleanup())

  it("hides the rebuild action when the server is not running under hot reload", () => {
    renderPage()
    expect(screen.queryByRole("button", { name: /rebuild and hot reload/i })).toBeNull()
  })

  it("starts a full build and confirms it with a toast", async () => {
    useSettings.mockReturnValue(settings(true))
    let resolveBuild!: (response: Response) => void
    mockedApiFetch.mockReturnValue(new Promise((resolve) => { resolveBuild = resolve }))
    renderPage()

    const button = screen.getByRole("button", { name: /rebuild and hot reload/i })
    fireEvent.click(button)
    expect(mockedApiFetch).toHaveBeenCalledWith("https://remote.example", "/api/hot-reload/build", { method: "POST" })
    expect((button as HTMLButtonElement).disabled).toBe(true)
    fireEvent.click(button)
    expect(mockedApiFetch).toHaveBeenCalledOnce()

    resolveBuild({ ok: true, status: 202 } as Response)
    await waitFor(() => expect(addToast).toHaveBeenCalledWith("Full rebuild started", "success"))
    expect((button as HTMLButtonElement).disabled).toBe(false)
  })

  it("surfaces a rejected build request", async () => {
    useSettings.mockReturnValue(settings(true))
    mockedApiFetch.mockResolvedValue({ ok: false, status: 409, text: async () => "a hot reload build is already running" } as Response)
    renderPage()

    fireEvent.click(screen.getByRole("button", { name: /rebuild and hot reload/i }))

    await waitFor(() => expect(addToast).toHaveBeenCalledWith("a hot reload build is already running", "error"))
    expect((screen.getByRole("button", { name: /rebuild and hot reload/i }) as HTMLButtonElement).disabled).toBe(false)
  })

  it("surfaces a network failure and allows another attempt", async () => {
    useSettings.mockReturnValue(settings(true))
    mockedApiFetch.mockRejectedValue(new Error("network unavailable"))
    renderPage()

    fireEvent.click(screen.getByRole("button", { name: /rebuild and hot reload/i }))

    await waitFor(() => expect(addToast).toHaveBeenCalledWith("network unavailable", "error"))
    expect((screen.getByRole("button", { name: /rebuild and hot reload/i }) as HTMLButtonElement).disabled).toBe(false)
  })
})
