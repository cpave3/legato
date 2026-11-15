import "@testing-library/jest-dom/vitest"
import { execFileSync } from "node:child_process"
import type { ConfigSetting, ConfigList } from "../../lib/configSettings"
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter, useNavigate } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { QueryClientProvider } from "@tanstack/react-query"
import { apiFetch } from "../../lib/api"
import { createQueryClient, queryKeys } from "../../lib/query"
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

function HistoryBack() {
  const navigate = useNavigate()
  return <button onClick={() => navigate(-1)}>Browser Back</button>
}

function renderPage(entry = "/settings", client = createQueryClient()) {
  return render(<QueryClientProvider client={client}><MemoryRouter initialEntries={[entry]}><SettingsPage /></MemoryRouter></QueryClientProvider>)
}

describe("SettingsPage hot reload build", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    useSettings.mockReturnValue(settings(false))
  })

  afterEach(() => cleanup())

  it("stores the animated agent activity preference in this browser", () => {
    renderPage("/settings#preferences")

    const toggle = screen.getByRole("button", { name: /animated agent activity/i })
    expect(toggle.getAttribute("aria-pressed")).toBe("true")

    fireEvent.click(toggle)

    expect(toggle.getAttribute("aria-pressed")).toBe("false")
    expect(localStorage.getItem("legato:animated-agent-activity")).toBe("false")
  })

  it("stores the terminal theme in this browser", () => {
    renderPage("/settings#preferences")

    const themes = screen.getByRole("radiogroup", { name: "Terminal theme" })
    expect(within(themes).getByRole("radio", { name: "Follow app" }).getAttribute("aria-checked")).toBe("true")

    fireEvent.click(within(themes).getByRole("radio", { name: "Night" }))

    expect(within(themes).getByRole("radio", { name: "Night" }).getAttribute("aria-checked")).toBe("true")
    expect(within(themes).getByRole("radio", { name: "Legato" }).getAttribute("aria-checked")).toBe("false")
    expect(localStorage.getItem("legato:terminal-theme")).toBe("night")
  })

  it("offers app themes in Appearance and applies the selected palette", () => {
    renderPage("/settings#preferences")
    const themes = screen.getByRole("radiogroup", { name: "App theme" })
    fireEvent.click(within(themes).getByRole("radio", { name: "Ocular Light" }))
    expect(localStorage.getItem("legato:app-theme")).toBe("ocular-light")
    expect(document.documentElement.style.getPropertyValue("--color-lw-text")).toBe("#252B33")
  })

  it("hides the rebuild action when the server is not running under hot reload", () => {
    renderPage("/settings#server")
    expect(screen.queryByRole("button", { name: /rebuild and hot reload/i })).toBeNull()
  })

  it("starts a full build and confirms it with a toast", async () => {
    useSettings.mockReturnValue(settings(true))
    let resolveBuild!: (response: Response) => void
    mockedApiFetch.mockReturnValue(new Promise((resolve) => { resolveBuild = resolve }))
    renderPage("/settings#server")

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
    renderPage("/settings#server")

    fireEvent.click(screen.getByRole("button", { name: /rebuild and hot reload/i }))

    await waitFor(() => expect(addToast).toHaveBeenCalledWith("a hot reload build is already running", "error"))
    expect((screen.getByRole("button", { name: /rebuild and hot reload/i }) as HTMLButtonElement).disabled).toBe(false)
  })

  it("surfaces a network failure and allows another attempt", async () => {
    useSettings.mockReturnValue(settings(true))
    mockedApiFetch.mockRejectedValue(new Error("network unavailable"))
    renderPage("/settings#server")

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

  it("opens one task category and keeps search and server identity visible", () => {
    renderPage()
    expect(screen.getByRole("searchbox", { name: "Search settings" })).toBeVisible()
    expect(screen.getByText("Server: https://remote.example")).toBeVisible()
    const nav = screen.getByRole("navigation", { name: "Settings sections" })
    expect(within(nav).getAllByRole("link").map((link) => link.textContent)).toEqual([
      "Preferences", "Agents and workspaces", "Reeve", "Security", "Integrations", "Connections and handoff", "Server and maintenance",
    ])
    fireEvent.click(within(nav).getByRole("link", { name: "Security" }))
    expect(screen.getByRole("region", { name: "Security" })).toBeVisible()
    expect(screen.queryByRole("region", { name: "Preferences" })).toBeNull()
    fireEvent.click(screen.getByRole("link", { name: "Back to Settings" }))
    expect(screen.queryByRole("region", { name: "Security" })).toBeNull()
  })

  it("finds a server setting by task term and opens its only destination", async () => {
    mockedApiFetch.mockImplementation(async (_base, path) => path === "/api/config/settings"
      ? Response.json({ settings: [{ key: "jira.base_url", title: "Jira address", description: "Where tickets are imported", kind: "string", value: "", source: "default", pinned: false }], file_only: ["web.port"] })
      : Response.json({ lists: [] }))
    renderPage()
    fireEvent.change(screen.getByRole("searchbox"), { target: { value: "tickets" } })
    const result = await screen.findByRole("link", { name: /Jira address.*Integrations.*This server/ })
    fireEvent.click(result)
    expect(await screen.findByRole("textbox", { name: "Jira address" })).toBeVisible()
    expect(document.querySelectorAll('[id="setting-jira.base_url"]')).toHaveLength(1)
    expect(document.activeElement).toBe(document.getElementById("setting-jira.base_url"))
  })

  it("opens Preferences by default on desktop", () => {
    vi.stubGlobal("matchMedia", vi.fn((query: string) => ({ matches: query === "(min-width: 1024px)", addEventListener: vi.fn(), removeEventListener: vi.fn() })))
    renderPage()
    expect(screen.getByRole("region", { name: "Preferences" })).toBeVisible()
    vi.unstubAllGlobals()
  })

  it("resolves delayed list links and uses the same credential editor from MQTT", async () => {
    let release!: (response: Response) => void
    mockedApiFetch.mockImplementation(async (_base, path) => {
      if (path === "/api/config/settings") return Response.json({ settings: [] })
      if (path === "/api/config/lists") return new Promise<Response>((resolve) => { release = resolve })
      return new Response("{}", { status: 404 })
    })
    renderPage("/settings#mqtt")
    fireEvent.click(screen.getByRole("link", { name: "Edit MQTT credentials" }))
    expect(screen.getByText("Loading configuration lists…")).toBeVisible()
    await act(async () => release(Response.json({ lists: [{ key: "credentials", title: "Credentials", mode: "restart", entries: [] }] })))
    await waitFor(() => expect(document.activeElement).toBe(document.getElementById("list-credentials")))
    fireEvent.click(screen.getByRole("button", { name: "Add to credentials" }))
    expect(screen.getByRole("textbox", { name: "Secret reference" })).toBeVisible()
    expect(screen.getAllByRole("textbox", { name: "Credentials name" })).toHaveLength(1)
    expect(screen.getByRole("link", { name: "Edit credentials" })).toHaveAttribute("href", "/settings#list-credentials")
  })

  it("shows new keys in Advanced and file-only instructions without editable controls", async () => {
    mockedApiFetch.mockImplementation(async (_base, path) => path === "/api/config/settings"
      ? Response.json({ settings: [{ key: "future.option", title: "Future option", kind: "string", value: "hello", source: "default", pinned: false }], file_only: ["web.port"] })
      : Response.json({ lists: [], settings: [], entries: [] }))
    renderPage("/settings#setting-future.option")
    expect(await within(await screen.findByRole("region", { name: "Advanced" })).findByRole("textbox", { name: "Future option" })).toHaveValue("hello")
    fireEvent.change(screen.getByRole("searchbox"), { target: { value: "web.port" } })
    fireEvent.click(screen.getByRole("link", { name: /web.port.*File only/ }))
    const target = document.getElementById("setting-web.port")!
    expect(target).toHaveTextContent("Read-only here. Edit web.port")
    expect(within(target).queryByRole("textbox")).toBeNull()
  })

  it("reports an unavailable deep link after loading rather than silently opening another setting", async () => {
    mockedApiFetch.mockImplementation(async (_base, path) => path.startsWith("/api/config/") ? Response.json({ settings: [], lists: [], entries: [] }) : new Response("{}", { status: 404 }))
    renderPage("/settings#setting-no.longer.exists")
    expect(await screen.findByText(/destination is unavailable.*setting-no.longer.exists/)).toBeVisible()
  })

  it("adds a directory preset from its owning category", async () => {
    mockedApiFetch.mockImplementation(async (_base, path, init) => {
      if (path === "/api/directories/presets") return init?.method === "POST"
        ? Response.json({ name: "project", path: "/srv/project", display: "/srv/project", saved: true })
        : Response.json({ presets: [], default: { path: "/srv", display: "/srv" } })
      if (path === "/api/config/settings" || path === "/api/config/lists") return Response.json({ settings: [], lists: [] })
      return new Response("{}", { status: 404 })
    })
    renderPage("/settings#directory-presets")
    fireEvent.change(await screen.findByRole("textbox", { name: "Directory path" }), { target: { value: "/srv/project" } })
    fireEvent.click(screen.getByRole("button", { name: "Save directory" }))
    await waitFor(() => expect(mockedApiFetch).toHaveBeenCalledWith("https://remote.example", "/api/directories/presets", expect.objectContaining({ method: "POST", body: JSON.stringify({ path: "/srv/project" }) })))
    expect(await screen.findByText("Directory saved.")).toBeVisible()
  })

  it("preserves a registry draft through live updates and makes conflict and retry explicit", async () => {
    let value = "initial"
    let refuse = true
    const setting = () => ({ key: "jira.base_url", title: "Jira address", kind: "string", value, source: "database", pinned: false, mode: "restart", pending_restart: true })
    mockedApiFetch.mockImplementation(async (_base, path, init) => {
      if (path === "/api/config/settings") return Response.json({ settings: [setting()] })
      if (path === "/api/config/lists") return Response.json({ lists: [] })
      if (path === "/api/config/settings/jira.base_url" && init?.method === "PUT") {
        if (refuse) return Response.json({ error: "Invalid Jira address" }, { status: 400 })
        value = JSON.parse(String(init.body)).value
        return Response.json(setting())
      }
      return new Response("{}", { status: 404 })
    })
    const client = createQueryClient()
    renderPage("/settings#setting-jira.base_url", client)
    const input = await screen.findByRole("textbox", { name: "Jira address" })
    const row = within(input.closest("[data-testid]") as HTMLElement)
    fireEvent.change(input, { target: { value: "my edit" } })
    fireEvent.blur(input)
    expect(mockedApiFetch.mock.calls.filter(([, , init]) => init?.method === "PUT")).toHaveLength(0)
    value = "changed elsewhere"
    await act(async () => { await client.invalidateQueries({ queryKey: queryKeys.configSettings("https://remote.example"), exact: true }) })
    expect(await row.findByText("This value changed elsewhere.")).toBeVisible()
    expect(input).toHaveValue("my edit")
    expect(row.getByRole("button", { name: "Apply" })).toBeDisabled()
    fireEvent.click(row.getByRole("button", { name: "Keep my edit" }))
    fireEvent.click(row.getByRole("button", { name: "Apply" }))
    expect(await row.findByRole("alert")).toHaveTextContent("Invalid Jira address")
    expect(input).toHaveValue("my edit")
    refuse = false
    fireEvent.click(row.getByRole("button", { name: "Apply" }))
    expect(await row.findByText("Saved — applies at restart.")).toBeVisible()
    expect(value).toBe("my edit")
    fireEvent.change(input, { target: { value: "discard" } })
    fireEvent.click(row.getByRole("button", { name: "Cancel" }))
    expect(input).toHaveValue("my edit")
  })

  it("keeps a proposed list edit open and distinguishes signed approval from saved", async () => {
    mockedApiFetch.mockImplementation(async (_base, path, init) => {
      if (path === "/api/config/settings") return Response.json({ settings: [] })
      if (path === "/api/config/lists") return Response.json({ lists: [{ key: "reeve.rules", title: "Reeve rules", mode: "live", name_field: "pattern", entries: [] }] })
      if (init?.method === "PUT") return Response.json({ status: "needs_signature" }, { status: 202 })
      return new Response("{}", { status: 404 })
    })
    renderPage("/settings#list-reeve.rules")
    fireEvent.click(await screen.findByRole("button", { name: "Add to reeve rules" }))
    fireEvent.change(screen.getByRole("textbox", { name: "Reeve rules name" }), { target: { value: "config.*" } })
    fireEvent.change(screen.getByRole("textbox", { name: "Reeve rules fields" }), { target: { value: '{"level":"signed"}' } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(await screen.findByText(/Awaiting approval/)).toBeVisible()
    expect(screen.getByRole("textbox", { name: "Reeve rules fields" })).toHaveValue('{"level":"signed"}')
    expect(screen.queryByText(/^Saved/)).toBeNull()
  })

  it("gives every actual Go registry key and named list exactly one destination", async () => {
    const registry = JSON.parse(execFileSync("go", ["run", "./src/pages/__tests__/fixtures/settings-registry"], { encoding: "utf8" })) as { settings: ConfigSetting[]; file_only: string[]; lists: ConfigList[]; entry_lists: string[] }
    const values = registry.settings.map((s) => ({ ...s, value: s.kind === "bool" ? false : s.kind === "strings" ? [] : s.kind === "int" ? 0 : "", source: "default", pinned: false }))
    mockedApiFetch.mockImplementation(async (_base, path) => {
      if (path === "/api/config/settings") return Response.json({ settings: values, file_only: registry.file_only })
      if (path === "/api/config/lists") return Response.json({ lists: registry.lists.map((list) => ({ ...list, entries: [] })) })
      if (path === "/api/config/move") return Response.json({ settings: [], entries: [] })
      return new Response("{}", { status: 404 })
    })
    renderPage()
    await waitFor(() => expect(screen.queryByText("Loading server settings…")).toBeNull())
    const found: string[] = []
    for (const category of ["Preferences", "Agents and workspaces", "Reeve", "Security", "Integrations", "Connections and handoff", "Server and maintenance"]) {
      fireEvent.click(within(screen.getByRole("navigation", { name: "Settings sections" })).getByRole("link", { name: category }))
      await waitFor(() => expect(screen.queryByText("Loading configuration lists…")).toBeNull())
      found.push(...Array.from(document.querySelectorAll('[id^="setting-"], [id^="list-"]')).map((element) => element.id))
    }
    for (const key of [...registry.settings.map((s) => s.key), ...registry.file_only]) expect(found.filter((id) => id === `setting-${key}`), key).toHaveLength(1)
    for (const key of registry.entry_lists) expect(found.filter((id) => id === `list-${key}`), key).toHaveLength(1)
  }, 30000)

  it("puts existing controls in their task categories", async () => {
    renderPage()
    const open = (name: string) => fireEvent.click(within(screen.getByRole("navigation", { name: "Settings sections" })).getByRole("link", { name }))
    open("Integrations")
    expect(await screen.findByRole("region", { name: "Argus" })).toBeTruthy()
    open("Agents and workspaces")
    expect(document.getElementById("resources")).toBeTruthy()
    open("Security")
    expect(document.getElementById("devices")).toBeTruthy()
    expect(document.getElementById("credentials")).toBeTruthy()
    open("Connections and handoff")
    expect(document.getElementById("workers")).toBeTruthy()
    expect(document.getElementById("main-instances")).toBeTruthy()
    expect(screen.getByText("Servers")).toBeTruthy()
    expect(screen.getByText("Pair a phone")).toBeTruthy()
    expect(screen.getByText("TLS certificate")).toBeTruthy()
    open("Preferences")
    expect(screen.getByRole("button", { name: "Prompt detection" })).toBeTruthy()
    expect(screen.getByRole("combobox", { name: "Agent switch key" })).toBeTruthy()
    expect(screen.getByRole("button", { name: "Glitch effect" })).toBeTruthy()
  })

  it("adds Development under hot reload", () => {
    useSettings.mockReturnValue(settings(true))
    renderPage("/settings#development")
    expect(screen.getByRole("region", { name: "Development" })).toBeTruthy()
  })

  it.each([undefined, false, true])("links the old clones anchor to Remote with fresh clones %s", (enabled) => {
    useSettings.mockReturnValue({ data: { ...settings(false).data, fresh_clones_enabled: enabled } })
    renderPage("/settings#job-clones")
    const agents = screen.getByRole("region", { name: "Agents and workspaces" })
    const anchor = agents.querySelector("#job-clones")
    expect(anchor).toBeTruthy()
    expect(within(anchor as HTMLElement).getByRole("link", { name: "Open Remote" }).getAttribute("href")).toBe("/remote")
    expect(within(agents).queryByRole("button", { name: /^Remove .*\// })).toBeNull()
    expect(mockedApiFetch.mock.calls.some(([, path]) => path === "/api/job-clones")).toBe(false)
  })

  it("keeps category navigation in browser history", () => {
    render(<QueryClientProvider client={createQueryClient()}><MemoryRouter initialEntries={["/settings"]}><SettingsPage /><HistoryBack /></MemoryRouter></QueryClientProvider>)
    fireEvent.click(screen.getByRole("link", { name: "Security" }))
    fireEvent.click(screen.getByRole("link", { name: "Reeve" }))
    fireEvent.click(screen.getByRole("button", { name: "Browser Back" }))
    expect(screen.getByRole("region", { name: "Security" })).toBeVisible()
    expect(screen.queryByRole("region", { name: "Reeve" })).toBeNull()
  })

  // Every setting the voice sheet's Open settings can name
  // (internal/service/reeve_voice.go, cmd/legato/reeve_voice_runtime.go).
  it.each(["voice.enabled", "voice.whisper_url", "reeve.voice.enabled", "reeve.voice.command", "agents.sandbox.enabled"])("has a row to open at for %s", async (key) => {
    const setting = { key, title: key, kind: "string", class: "change", mode: "restart", value: "", source: "default", pinned: false }
    mockedApiFetch.mockImplementation(async (_base, path) => (path === "/api/config/settings"
      ? new Response(JSON.stringify({ settings: [setting] }), { status: 200 })
      : new Response("{}", { status: 404 })))
    renderPage(`/settings#setting-${key}`)
    await waitFor(() => expect(document.getElementById(`setting-${key}`)).toBeTruthy())
  })

  it("opens at the setting a link names", async () => {
    const scroll = vi.fn()
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value: scroll })
    const setting = { key: "voice.whisper_url", title: "Whisper server", kind: "string", class: "change", mode: "restart", value: "", source: "default", pinned: false }
    mockedApiFetch.mockImplementation(async (_base, path) => (path === "/api/config/settings"
      ? new Response(JSON.stringify({ settings: [setting] }), { status: 200 })
      : new Response("{}", { status: 404 })))
    render(<QueryClientProvider client={createQueryClient()}><MemoryRouter initialEntries={["/settings#setting-voice.whisper_url"]}><SettingsPage /></MemoryRouter></QueryClientProvider>)
    await waitFor(() => expect(scroll).toHaveBeenCalled())
    expect(scroll.mock.contexts.at(-1)).toBe(document.getElementById("setting-voice.whisper_url"))
  })

  it("marks what is kept in this browser only", () => {
    renderPage("/settings#appearance")
    expect(within(screen.getByText("App theme").parentElement!).getByText(/this browser/i)).toBeVisible()
    fireEvent.click(screen.getByRole("link", { name: "Server and maintenance" }))
    expect(within(screen.getByText("Tint").parentElement!).queryByText(/this browser/i)).toBeNull()
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


describe("SettingsPage MQTT", () => {
  const mqttSettings = () => [
    ["enabled", "MQTT publisher", false],
    ["broker", "MQTT broker", ""],
    ["credential", "MQTT credential", ""],
    ["discovery_prefix", "MQTT discovery prefix", "homeassistant"],
    ["base_topic", "MQTT base topic", ""],
    ["device_name", "MQTT device name", ""],
    ["include_reeve", "Include Reeve in MQTT counts", false],
    ["attention_details", "MQTT Attention details", false],
  ].map(([key, title, value]) => ({
    key: `integrations.mqtt.${key}`, title, value,
    kind: typeof value === "boolean" ? "bool" : "string",
    class: "risky", mode: "restart", source: "default", pinned: false,
    pending_restart: false,
  }))

  beforeEach(() => {
    vi.clearAllMocks()
    useSettings.mockReturnValue(settings(false))
  })
  afterEach(() => cleanup())

  it("opens MQTT under Integrations and saves through the registry with restart feedback", async () => {
    const values = mqttSettings()
    mockedApiFetch.mockImplementation(async (_base, path, init) => {
      if (path === "/api/config/settings") return Response.json({ settings: values })
      const item = values.find((s) => path === `/api/config/settings/${s.key}`)
      if (item && init?.method === "PUT") {
        Object.assign(item, { value: JSON.parse(String(init.body)).value, source: "database", pending_restart: true })
        return Response.json(item)
      }
      return new Response("{}", { status: 404 })
    })
    renderPage("/settings#mqtt")
    const integrations = within(screen.getByRole("region", { name: "Integrations" }))
    const mqtt = within(await integrations.findByRole("region", { name: "Home Assistant over MQTT" }))
    expect(await mqtt.findByRole("textbox", { name: "MQTT discovery prefix" })).toHaveValue("homeassistant")
    expect(mqtt.getAllByRole("textbox")).toHaveLength(5)
    expect(mqtt.getByRole("button", { name: "MQTT publisher" })).toHaveAttribute("aria-pressed", "false")
    const broker = mqtt.getByRole("textbox", { name: "MQTT broker" })
    fireEvent.change(broker, { target: { value: "mqtts://broker.example:8883" } })
    fireEvent.click(within(broker.closest("[data-testid]") as HTMLElement).getByRole("button", { name: /^Apply/ }))
    await waitFor(() => expect(mockedApiFetch).toHaveBeenCalledWith("https://remote.example", "/api/config/settings/integrations.mqtt.broker", expect.objectContaining({
      method: "PUT", body: JSON.stringify({ value: "mqtts://broker.example:8883" }),
    })))
    expect(await mqtt.findByText("applies at restart")).toBeTruthy()
    fireEvent.click(mqtt.getByRole("button", { name: "MQTT publisher" }))
    await waitFor(() => expect(mqtt.getByRole("button", { name: "MQTT publisher" })).toHaveAttribute("aria-pressed", "true"))
    expect(mqtt.getByText(/anonymous access/i)).toBeTruthy()
    expect(mockedApiFetch.mock.calls.some(([, path]) => path.includes("restart") || path.includes("hot-reload"))).toBe(false)
  })

  it("keeps file pins locked and surfaces broker validation and save refusals", async () => {
    const values = mqttSettings()
    Object.assign(values.find((s) => s.key.endsWith(".credential"))!, { value: "mqtt", pinned: true, source: "config.yaml" })
    mockedApiFetch.mockImplementation(async (_base, path, init) => {
      if (path === "/api/config/settings") return Response.json({ settings: values })
      if (init?.method === "PUT") return Response.json({ error: "integrations.mqtt.broker: want mqtt:// or mqtts:// without credentials" }, { status: 400 })
      return new Response("{}", { status: 404 })
    })
    renderPage("/settings#mqtt")
    const credential = await screen.findByRole("textbox", { name: "MQTT credential" })
    expect(credential).toBeDisabled()
    expect(credential).toHaveValue("mqtt")
    const broker = screen.getByRole("textbox", { name: "MQTT broker" })
    fireEvent.change(broker, { target: { value: "https://wrong" } })
    fireEvent.click(within(broker.closest("[data-testid]") as HTMLElement).getByRole("button", { name: /^Apply/ }))
    expect(await within(broker.closest("[data-testid]") as HTMLElement).findByRole("alert")).toHaveTextContent("want mqtt:// or mqtts://")
    expect(broker).toHaveValue("https://wrong")
    expect(screen.queryByText("applies at restart")).toBeNull()

    mockedApiFetch.mockImplementation(async (_base, path, init) => {
      if (path === "/api/config/settings") return Response.json({ settings: values })
      if (init?.method === "PUT") return Response.json({ error: "MQTT broker is pinned by config.yaml" }, { status: 409 })
      return new Response("{}", { status: 404 })
    })
    fireEvent.change(broker, { target: { value: "mqtts://broker.example" } })
    fireEvent.click(within(broker.closest("[data-testid]") as HTMLElement).getByRole("button", { name: /^Apply/ }))
    expect(await within(broker.closest("[data-testid]") as HTMLElement).findByRole("alert")).toHaveTextContent("pinned by config.yaml")
  })

  it("shows registry load failures and retries from the MQTT section", async () => {
    let refused = true
    mockedApiFetch.mockImplementation(async (_base, path) => {
      if (path === "/api/config/settings") return refused
        ? Response.json({ error: "Settings access denied" }, { status: 403 })
        : Response.json({ settings: mqttSettings() })
      return new Response("{}", { status: 404 })
    })
    renderPage("/settings#mqtt")
    const mqtt = within(screen.getByRole("region", { name: "Home Assistant over MQTT" }))
    expect(await screen.findByText("Could not load settings: Settings access denied")).toHaveAttribute("role", "alert")
    refused = false
    fireEvent.click(screen.getByRole("button", { name: "Retry settings" }))
    expect(await screen.findByRole("textbox", { name: "MQTT broker" })).toBeTruthy()
    expect(mqtt.queryByRole("alert")).toBeNull()
  })

})
