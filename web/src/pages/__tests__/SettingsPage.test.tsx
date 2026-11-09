import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { QueryClientProvider } from "@tanstack/react-query"
import { apiFetch } from "../../lib/api"
import { createQueryClient } from "../../lib/query"
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
vi.mock("../../lib/integrations", () => ({
  fetchIntegrations: () => Promise.resolve({ tokens: [], argus: { configured: false, connection: { state: "none", replies: {} }, open_cases: 0 } }),
}))

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
  return render(<QueryClientProvider client={createQueryClient()}><MemoryRouter><SettingsPage /></MemoryRouter></QueryClientProvider>)
}

describe("SettingsPage hot reload build", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    useSettings.mockReturnValue(settings(false))
  })

  afterEach(() => cleanup())

  it("stores the animated agent activity preference in this browser", () => {
    renderPage()

    const toggle = screen.getByRole("button", { name: /animated agent activity/i })
    expect(toggle.getAttribute("aria-pressed")).toBe("true")

    fireEvent.click(toggle)

    expect(toggle.getAttribute("aria-pressed")).toBe("false")
    expect(localStorage.getItem("legato:animated-agent-activity")).toBe("false")
  })

  it("stores the terminal theme in this browser", () => {
    renderPage()

    const themes = screen.getByRole("radiogroup", { name: "Terminal theme" })
    expect(within(themes).getByRole("radio", { name: "Legato" }).getAttribute("aria-checked")).toBe("true")

    fireEvent.click(within(themes).getByRole("radio", { name: "Night" }))

    expect(within(themes).getByRole("radio", { name: "Night" }).getAttribute("aria-checked")).toBe("true")
    expect(within(themes).getByRole("radio", { name: "Legato" }).getAttribute("aria-checked")).toBe("false")
    expect(localStorage.getItem("legato:terminal-theme")).toBe("night")
  })

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
    // The page asks for other things too (its resources); only one build may start.
    expect(mockedApiFetch.mock.calls.filter(([, path]) => path === "/api/hot-reload/build")).toHaveLength(1)

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

describe("SettingsPage layout", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    useSettings.mockReturnValue(settings(false))
    mockedApiFetch.mockResolvedValue(new Response("{}", { status: 404 }))
  })

  afterEach(() => cleanup())

  it("groups settings by what they are about, in order", () => {
    renderPage()
    const groups = screen.getAllByRole("heading", { level: 2 }).map((h) => h.textContent)
    expect(groups).toEqual(["Agents", "Reeve", "Security", "Integrations", "Handoff", "Connection", "Appearance", "Lists", "Updates"])
  })

  it("puts each setting in its group", async () => {
    renderPage()
    const group = (name: string) => screen.getByRole("region", { name })
    expect(await within(group("Integrations")).findByRole("region", { name: "Argus" })).toBeTruthy()
    expect(within(group("Integrations")).getByRole("region", { name: "Integration tokens" })).toBeTruthy()
    expect(within(group("Agents")).getByText("Prompt detection")).toBeTruthy()
    expect(within(group("Agents")).getByText("Agent switch key")).toBeTruthy()
    expect(group("Agents").querySelector("#resources")).toBeTruthy()
    expect(group("Security").querySelector("#devices")).toBeTruthy()
    expect(group("Security").querySelector("#credentials")).toBeTruthy()
    expect(group("Handoff").querySelector("#workers")).toBeTruthy()
    expect(group("Handoff").querySelector("#main-instances")).toBeTruthy()
    expect(within(group("Connection")).getByText("Servers")).toBeTruthy()
    expect(within(group("Connection")).getByText("Pair a phone")).toBeTruthy()
    expect(within(group("Connection")).getByText("TLS certificate")).toBeTruthy()
    expect(within(group("Appearance")).getByText("Glitch effect")).toBeTruthy()
  })

  it("adds Development under hot reload", () => {
    useSettings.mockReturnValue(settings(true))
    renderPage()
    expect(screen.getByRole("region", { name: "Development" })).toBeTruthy()
  })

  it.each([undefined, false, true])("links the old clones anchor to Remote with fresh clones %s", (enabled) => {
    useSettings.mockReturnValue({ data: { ...settings(false).data, fresh_clones_enabled: enabled } })
    renderPage()
    const agents = screen.getByRole("region", { name: "Agents" })
    const anchor = agents.querySelector("#job-clones")
    expect(anchor).toBeTruthy()
    expect(within(anchor as HTMLElement).getByRole("link", { name: "Open Remote" }).getAttribute("href")).toBe("/remote")
    expect(within(agents).queryByRole("button", { name: /^Remove .*\// })).toBeNull()
    expect(mockedApiFetch.mock.calls.some(([, path]) => path === "/api/job-clones")).toBe(false)
  })

  it("lists the groups and goes to one", () => {
    const scroll = vi.fn()
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value: scroll })
    renderPage()
    const nav = screen.getAllByRole("navigation", { name: "Settings sections" })[0]
    const links = within(nav).getAllByRole("button").map((b) => b.textContent)
    expect(links).toEqual(["Agents", "Reeve", "Security", "Integrations", "Handoff", "Connection", "Appearance", "Lists", "Updates"])
    fireEvent.click(within(nav).getByRole("button", { name: "Security" }))
    expect(scroll).toHaveBeenCalled()
  })

  it("marks what is kept in this browser only", () => {
    renderPage()
    const appearance = screen.getByRole("region", { name: "Appearance" })
    expect(within(appearance).getAllByText(/this browser/i).length).toBeGreaterThan(0)
  })
})


describe("SettingsPage version", () => {
  it("shows the version of the current server in the sections menu", async () => {
    useSettings.mockReturnValue({ ...settings(false), refetch: vi.fn() })
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ version: "v9.9.9" }), { status: 200 })))
    renderPage()
    expect((await screen.findByTestId("settings-version")).textContent).toContain("v9.9.9")
    vi.unstubAllGlobals()
  })
})
