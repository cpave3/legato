import "@testing-library/jest-dom/vitest"
import type { ConfigSetting, ConfigList } from "../../lib/configSettings"
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter, useNavigate } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { QueryClientProvider } from "@tanstack/react-query"
import { apiFetch } from "../../lib/api"
import { createQueryClient, queryKeys } from "../../lib/query"
import { SettingsPage } from "../Settings"
import { fileOnlyPlace, findSection, listPlace, settingsSections } from "../../lib/settingsCatalog"
// A copy of the Go settings registry (config.Settings and friends), kept current by
// go test ./config -run TestTheWebSettingsRegistryFixtureIsCurrent -update.
import registryFixture from "./fixtures/settings-registry.json"

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
    renderPage("/settings#system")
    expect(screen.queryByRole("button", { name: /rebuild and hot reload/i })).toBeNull()
  })

  it("starts a full build and confirms it with a toast", async () => {
    useSettings.mockReturnValue(settings(true))
    let resolveBuild!: (response: Response) => void
    mockedApiFetch.mockReturnValue(new Promise((resolve) => { resolveBuild = resolve }))
    renderPage("/settings#system")

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
    renderPage("/settings#system")

    fireEvent.click(screen.getByRole("button", { name: /rebuild and hot reload/i }))

    await waitFor(() => expect(addToast).toHaveBeenCalledWith("a hot reload build is already running", "error"))
    expect((screen.getByRole("button", { name: /rebuild and hot reload/i }) as HTMLButtonElement).disabled).toBe(false)
  })

  it("surfaces a network failure and allows another attempt", async () => {
    useSettings.mockReturnValue(settings(true))
    mockedApiFetch.mockRejectedValue(new Error("network unavailable"))
    renderPage("/settings#system")

    fireEvent.click(screen.getByRole("button", { name: /rebuild and hot reload/i }))

    await waitFor(() => expect(addToast).toHaveBeenCalledWith("network unavailable", "error"))
    expect((screen.getByRole("button", { name: /rebuild and hot reload/i }) as HTMLButtonElement).disabled).toBe(false)
  })
})

type Registry = { settings: ConfigSetting[]; file_only: string[]; lists: ConfigList[]; entry_lists: string[] }
const registry = registryFixture as unknown as Registry

// The registry as a server serves it, every value its kind's zero, with
// overrides by key.
function serveRegistry(overrides: Record<string, Partial<ConfigSetting>> = {}, extra: (path: string, init?: RequestInit) => Response | undefined = () => undefined) {
  const values = registry.settings.map((s) => ({ ...s, value: s.kind === "bool" ? false : s.kind === "strings" ? [] : s.kind === "int" ? 0 : "", source: "default", pinned: false, ...overrides[s.key] }))
  mockedApiFetch.mockImplementation(async (_base, path, init) => {
    const answer = extra(path, init)
    if (answer) return answer
    if (path === "/api/config/settings") return Response.json({ settings: values, file_only: registry.file_only, file_only_values: { "web.port": "3080" } })
    if (path === "/api/config/lists") return Response.json({ lists: registry.lists.map((list) => ({ ...list, entries: [] })) })
    if (path === "/api/config/move") return Response.json({ settings: [], entries: [] })
    return new Response("{}", { status: 404 })
  })
  return values
}

function wideScreen(wide: boolean) {
  vi.stubGlobal("matchMedia", vi.fn((query: string) => ({ matches: wide && query === "(min-width: 1024px)", addEventListener: vi.fn(), removeEventListener: vi.fn() })))
}

const nav = () => screen.getByRole("navigation", { name: "Settings sections" })
const openSection = (name: string | RegExp) => fireEvent.click(within(nav()).getByRole("link", { name }))
const loaded = () => waitFor(() => {
  expect(screen.queryByText("Loading server settings…")).toBeNull()
  expect(screen.queryByText("Loading configuration lists…")).toBeNull()
})

describe("SettingsPage layout", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    useSettings.mockReturnValue(settings(false))
    mockedApiFetch.mockResolvedValue(new Response("{}", { status: 404 }))
    wideScreen(true)
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it("lists 16 sections in six groups, Macros as a link to its page", () => {
    renderPage()
    const groups = within(nav()).getAllByRole("group")
    expect(groups.map((g) => g.getAttribute("aria-label"))).toEqual(["You", "Agents", "Features", "Services", "Access", "System"])
    const titles = within(nav()).getAllByRole("link").map((link) => link.textContent?.replace(/(On|Off)$/, ""))
    expect(titles).toEqual(expect.arrayContaining(["Appearance", "Attention", "Input and terminal", "Agents", "Repositories", "Sandbox", "Macros", "Reeve", "Swarms", "Review passes", "Learn", "Board and tickets", "Integrations", "Security", "Connections", "System"]))
    expect(within(nav()).getByRole("link", { name: "Macros" })).toHaveAttribute("href", "/macros")
  })

  it("opens Appearance by default on a wide screen and lists the open section's groups in the nav", async () => {
    serveRegistry()
    renderPage()
    expect(screen.getByRole("region", { name: "Appearance" })).toBeVisible()
    await loaded()
    openSection(/^Swarms/)
    const groups = within(nav()).getByRole("navigation", { name: "Swarms groups" })
    expect(within(groups).getAllByRole("link").map((link) => link.textContent)).toEqual(["Conductor and workers", "Limits", "Behaviour", "Advanced"])
    fireEvent.click(within(groups).getByRole("link", { name: "Limits" }))
    await waitFor(() => expect(document.activeElement).toBe(document.getElementById("swarms--limits")))
    expect(within(screen.getByRole("region", { name: "Limits" })).getByRole("textbox", { name: "Workers at once" })).toBeTruthy()
  })

  it("gives every registry key, file-only key and list exactly one row, in the section it names", async () => {
    serveRegistry()
    renderPage()
    await loaded()
    const found = new Map<string, string[]>()
    for (const section of settingsSections.filter((s) => !s.href)) {
      openSection(new RegExp(`^${section.title}`))
      await loaded()
      for (const element of Array.from(document.querySelectorAll('[id^="setting-"], [id^="list-"]'))) found.set(element.id, [...(found.get(element.id) ?? []), section.id])
    }
    for (const s of registry.settings) expect(found.get(`setting-${s.key}`), s.key).toEqual([s.section])
    for (const key of registry.file_only) expect(found.get(`setting-${key}`), key).toEqual([fileOnlyPlace(key).section])
    for (const key of registry.entry_lists.filter((k) => k !== "macros")) expect(found.get(`list-${key}`), key).toEqual([listPlace(key).section])
  })

  // Every old fragment the code base links to: Attention items, setup's
  // checklist, the docs, the voice sheet and the Reeve.
  it.each([
    ["workers", "connections", "workers"],
    ["mqtt", "integrations", "mqtt"],
    ["list-credentials", "security", "list-credentials"],
    ["setting-swarm.enabled", "swarms", "setting-swarm.enabled"],
    ["setting-agents.sandbox.enabled", "sandbox", "setting-agents.sandbox.enabled"],
    ["setting-review_passes.enabled", "review-passes", "setting-review_passes.enabled"],
    ["setting-learning.enabled", "learn", "setting-learning.enabled"],
    ["setting-voice.whisper_url", "input", "setting-voice.whisper_url"],
    ["setting-reeve.voice.enabled", "reeve", "setting-reeve.voice.enabled"],
    ["setting-web.port", "connections", "setting-web.port"],
    ["list-reeve.rules", "reeve", "list-reeve.rules"],
    ["preferences", "appearance", "section-appearance"],
    ["appearance", "appearance", "section-appearance"],
    ["server", "system", "section-system"],
    ["integrations", "integrations", "section-integrations"],
    ["reeve", "reeve", "section-reeve"],
    ["devices", "security", "devices"],
    ["credentials", "security", "credentials"],
    ["resources", "agents", "list-resources"],
    ["needs-you-rail", "attention", "needs-you-rail"],
    ["job-clones", "repositories", "job-clones"],
    ["directory-presets", "repositories", "directory-presets"],
    ["updates", "system", "updates"],
    ["move", "system", "move"],
    ["burnlite", "board", "section-board"],
    ["integration-token-overview", "security", "section-security"],
  ])("opens the old link #%s at its new home in %s", async (fragment, section, element) => {
    serveRegistry({}, (path) => path === "/api/integrations" ? Response.json({ tokens: [], argus: { configured: false, connection: { state: "none", replies: {} }, open_cases: 0 } }) : undefined)
    renderPage(`/settings#${fragment}`)
    await loaded()
    expect(await screen.findByRole("region", { name: findSection(section)!.title })).toBeTruthy()
    expect(document.getElementById(`section-${section}`)).toBeTruthy()
    if (element !== `section-${section}`) await waitFor(() => expect(document.getElementById(element)).toBeTruthy())
  })

  it("opens a group by its fragment", async () => {
    serveRegistry()
    renderPage("/settings#learn/semantic-search")
    await loaded()
    await waitFor(() => expect(document.activeElement).toBe(document.getElementById("learn--semantic-search")))
  })

  it("shows On or Off for features, a dot on Security with no approval device, and a count on System while changes wait", async () => {
    serveRegistry({ "swarm.enabled": { value: true }, "agents.sandbox.enabled": { value: false }, "jira.base_url": { pending_restart: true, source: "database" }, "swarm.max_concurrent_agents": { pending_restart: true, source: "database" } },
      (path) => path === "/api/devices" ? Response.json({ devices: [], enrolled: false, server_id: "s" }) : undefined)
    renderPage()
    await loaded()
    expect(within(nav()).getByRole("link", { name: /^Swarms/ })).toHaveTextContent("On")
    expect(within(nav()).getByRole("link", { name: /^Sandbox/ })).toHaveTextContent("Off")
    expect(within(nav()).getByRole("link", { name: /^Learn/ })).toHaveTextContent("Off")
    expect(await within(nav()).findByRole("img", { name: "No approval device" })).toBeTruthy()
    expect(within(nav()).getByRole("link", { name: /^System/ })).toHaveTextContent("2")
  })

  it("marks Security only while no approval device is enrolled", async () => {
    serveRegistry({}, (path) => path === "/api/devices" ? Response.json({ devices: [{ id: "d", name: "phone" }], enrolled: true, server_id: "s" }) : undefined)
    renderPage()
    await loaded()
    await waitFor(() => expect(mockedApiFetch.mock.calls.some(([, path]) => path === "/api/devices")).toBe(true))
    expect(within(nav()).queryByRole("img", { name: "No approval device" })).toBeNull()
    expect(within(nav()).getByRole("link", { name: /^System/ }).textContent).toBe("System")
  })

  it("on a phone, lists the sections with their state, then opens each as a page with Back and its group links", async () => {
    wideScreen(false)
    serveRegistry({ "review_passes.enabled": { value: true } }, (path) => path === "/api/devices" ? Response.json({ devices: [], enrolled: false, server_id: "s" }) : undefined)
    renderPage()
    await loaded()
    expect(screen.queryByRole("region", { name: "Appearance" })).toBeNull()
    expect(within(nav()).getByRole("link", { name: /^Review passes/ })).toHaveTextContent("On")
    expect(await within(nav()).findByText("No approval device")).toBeTruthy()
    openSection(/^Reeve/)
    expect(screen.getByRole("region", { name: "Reeve" })).toBeTruthy()
    const groups = screen.getByRole("navigation", { name: "Reeve groups" })
    expect(within(groups).getByRole("link", { name: "Voice" })).toHaveAttribute("href", "/settings#reeve/voice")
    fireEvent.click(screen.getByRole("link", { name: "Back to Settings" }))
    expect(screen.queryByRole("region", { name: "Reeve" })).toBeNull()
  })

  it("keeps the server in use in the header, and its link opens Connections", async () => {
    serveRegistry()
    renderPage()
    const server = screen.getByRole("link", { name: "Server: https://remote.example" })
    expect(screen.getByRole("searchbox", { name: "Search settings" })).toBeVisible()
    fireEvent.click(server)
    expect(await screen.findByRole("region", { name: "Connections" })).toBeTruthy()
    expect(document.getElementById("servers")).toBeTruthy()
  })

  it("finds a setting by a word of its help and opens its only home", async () => {
    serveRegistry()
    renderPage()
    await loaded()
    fireEvent.change(screen.getByRole("searchbox"), { target: { value: "atlassian" } })
    const result = await screen.findByRole("link", { name: /^Address.*Board and tickets › Jira/ })
    fireEvent.click(result)
    expect(await within(await screen.findByRole("region", { name: "Jira" })).findByRole("textbox", { name: "Address" })).toBeVisible()
    expect(document.querySelectorAll('[id="setting-jira.base_url"]')).toHaveLength(1)
    await waitFor(() => expect(document.activeElement).toBe(document.getElementById("setting-jira.base_url")))
  })

  it("puts a setting of a section this web UI does not know under System, never nowhere", async () => {
    mockedApiFetch.mockImplementation(async (_base, path) => path === "/api/config/settings"
      ? Response.json({ settings: [{ key: "future.option", title: "Future option", kind: "string", value: "hello", source: "default", pinned: false }], file_only: ["web.port"] })
      : Response.json({ lists: [], settings: [], entries: [] }))
    renderPage("/settings#setting-future.option")
    expect(await within(await screen.findByRole("region", { name: "Other settings" })).findByRole("textbox", { name: "Future option" })).toHaveValue("hello")
    fireEvent.change(screen.getByRole("searchbox"), { target: { value: "web.port" } })
    fireEvent.click(screen.getByRole("link", { name: /web.port.*config.yaml only/ }))
    const target = await waitFor(() => { const el = document.getElementById("setting-web.port"); expect(el).toBeTruthy(); return el! })
    expect(target).toHaveTextContent("Read-only here. Change it in config.yaml")
    expect(within(target).queryByRole("textbox")).toBeNull()
  })

  it("reports an unavailable deep link after loading rather than silently opening another setting", async () => {
    mockedApiFetch.mockImplementation(async (_base, path) => path.startsWith("/api/config/") ? Response.json({ settings: [], lists: [], entries: [] }) : new Response("{}", { status: 404 }))
    renderPage("/settings#setting-no.longer.exists")
    expect(await screen.findByText(/destination is unavailable.*setting-no.longer.exists/)).toBeVisible()
  })

  it("resolves delayed list links and uses the same credential editor from MQTT", async () => {
    let release!: (response: Response) => void
    mockedApiFetch.mockImplementation(async (_base, path) => {
      if (path === "/api/config/settings") return Response.json({ settings: [] })
      if (path === "/api/config/lists") return new Promise<Response>((resolve) => { release = resolve })
      return new Response("{}", { status: 404 })
    })
    renderPage("/settings#mqtt")
    fireEvent.click(await screen.findByRole("link", { name: "Edit MQTT credentials" }))
    expect(screen.getByText("Loading configuration lists…")).toBeVisible()
    await act(async () => release(Response.json({ lists: [{ key: "credentials", title: "Credentials", mode: "restart", entries: [] }] })))
    await waitFor(() => expect(document.activeElement).toBe(document.getElementById("list-credentials")))
    fireEvent.click(screen.getByRole("button", { name: "Add to credentials" }))
    expect(screen.getByRole("textbox", { name: "Secret reference" })).toBeVisible()
    expect(screen.getAllByRole("textbox", { name: "Credentials name" })).toHaveLength(1)
  })

  it("adds a directory preset in Repositories", async () => {
    mockedApiFetch.mockImplementation(async (_base, path, init) => {
      if (path === "/api/directories/presets") return init?.method === "POST"
        ? Response.json({ name: "project", path: "/srv/project", display: "/srv/project", saved: true })
        : Response.json({ presets: [], default: { path: "/srv", display: "/srv" } })
      if (path === "/api/config/settings" || path === "/api/config/lists") return Response.json({ settings: [], lists: [] })
      return new Response("{}", { status: 404 })
    })
    renderPage("/settings#directory-presets")
    expect(screen.getByRole("region", { name: "Repositories" })).toBeTruthy()
    fireEvent.change(await screen.findByRole("textbox", { name: "Directory path" }), { target: { value: "/srv/project" } })
    fireEvent.click(screen.getByRole("button", { name: "Save directory" }))
    await waitFor(() => expect(mockedApiFetch).toHaveBeenCalledWith("https://remote.example", "/api/directories/presets", expect.objectContaining({ method: "POST", body: JSON.stringify({ path: "/srv/project" }) })))
    expect(await screen.findByText("Directory saved.")).toBeVisible()
  })

  it("preserves a registry draft through live updates and makes conflict and retry explicit", async () => {
    let value = "initial"
    let refuse = true
    const setting = () => ({ key: "jira.base_url", title: "Jira address", section: "board", group: "Jira", kind: "string", value, source: "database", pinned: false, mode: "restart", pending_restart: true })
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
    expect(await row.findByText("Changed to changed elsewhere while you edited.")).toBeVisible()
    expect(input).toHaveValue("my edit")
    expect(row.getByRole("button", { name: "Save" })).toBeDisabled()
    fireEvent.click(row.getByRole("button", { name: "Keep my edit" }))
    fireEvent.click(row.getByRole("button", { name: "Save" }))
    expect(await row.findByRole("alert")).toHaveTextContent("Invalid Jira address")
    expect(input).toHaveValue("my edit")
    refuse = false
    fireEvent.click(row.getByRole("button", { name: "Save" }))
    expect(await row.findByText("Saved. It applies when Legato restarts.")).toBeVisible()
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

  it("puts the cards in their sections", async () => {
    serveRegistry({}, (path) => path === "/api/integrations" ? Response.json({ tokens: [], argus: { configured: false, connection: { state: "none", replies: {} }, open_cases: 0 } }) : undefined)
    renderPage()
    await loaded()
    openSection("Integrations")
    expect(await screen.findByRole("region", { name: "Argus" })).toBeTruthy()
    openSection("Agents")
    expect(document.getElementById("resources")).toBeTruthy()
    expect(document.getElementById("skills")).toBeTruthy()
    openSection(/^Security/)
    expect(document.getElementById("devices")).toBeTruthy()
    expect(document.getElementById("credentials")).toBeTruthy()
    openSection("Connections")
    expect(document.getElementById("workers")).toBeTruthy()
    expect(document.getElementById("main-instances")).toBeTruthy()
    expect(screen.getByText("Servers")).toBeTruthy()
    expect(screen.getByText("Pair a phone")).toBeTruthy()
    expect(screen.getByText("TLS certificate")).toBeTruthy()
    openSection("Input and terminal")
    expect(screen.getByRole("button", { name: "Prompt detection" })).toBeTruthy()
    expect(screen.getByRole("combobox", { name: "Agent switch key" })).toBeTruthy()
    openSection("Appearance")
    expect(screen.getByRole("button", { name: "Glitch effect" })).toBeTruthy()
    expect(document.getElementById("setting-web.tint")).toBeTruthy()
    openSection("Attention")
    expect(document.getElementById("browser-notifications")).toBeTruthy()
    expect(document.getElementById("needs-you-rail")).toBeTruthy()
    openSection("Board and tickets")
    expect(document.getElementById("list-board.columns")).toBeTruthy()
    expect(document.getElementById("list-workspaces")).toBeTruthy()
  })

  it("says once in a group's heading when its changes apply, and on each row when they differ", async () => {
    serveRegistry({ "swarm.max_steps_per_plan": { mode: "live" } })
    renderPage("/settings#swarms")
    await loaded()
    expect(screen.getByTestId("swarms--conductor-and-workers-applies")).toHaveTextContent("Applies at restart")
    expect(screen.queryByTestId("swarms--limits-applies")).toBeNull()
    const limits = within(screen.getByRole("region", { name: "Limits" }))
    expect(limits.getByText("Applies now")).toBeTruthy()
    expect(limits.getAllByText("Applies at restart")).toHaveLength(2)
  })

  it("shows every row's key with Show config keys, and remembers it in this browser", async () => {
    serveRegistry()
    renderPage("/settings#swarms")
    await loaded()
    expect(screen.queryByRole("button", { name: "Copy key swarm.strict_scope" })).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Show config keys" }))
    expect(screen.getByRole("button", { name: "Copy key swarm.strict_scope" })).toBeTruthy()
    expect(localStorage.getItem("legato:settings-show-keys")).toBe("true")
  })

  it("shows a file-only setting in its topic with its value", async () => {
    serveRegistry()
    renderPage("/settings#connections/network")
    await loaded()
    const row = within(await waitFor(() => { const el = document.getElementById("setting-web.port"); expect(el).toBeTruthy(); return el! }))
    expect(row.getByText("Web port")).toBeTruthy()
    expect(row.getByText("3080")).toBeTruthy()
    expect(row.getByText("config.yaml only")).toBeTruthy()
  })

  it("sends the old Macros link to the Macros page", () => {
    renderPage("/settings#list-macros")
    expect(within(screen.getByRole("region", { name: "Macros" })).getByRole("link", { name: "Open Macros" })).toHaveAttribute("href", "/macros")
    expect(screen.queryByRole("button", { name: "Add to macros" })).toBeNull()
  })

  it("lists only the groups a section draws: no Development link without hot reload", async () => {
    serveRegistry()
    renderPage("/settings#system")
    await loaded()
    const links = within(screen.getByRole("navigation", { name: "System groups" })).getAllByRole("link").map((l) => l.textContent)
    expect(links).not.toContain("Development")
    expect(links).toContain("Restart")
  })

  it("adds Development to System under hot reload", () => {
    useSettings.mockReturnValue(settings(true))
    renderPage("/settings#development")
    expect(screen.getByRole("region", { name: "Development" })).toBeTruthy()
  })

  it.each([undefined, false, true])("links the old clones anchor to Remote with fresh clones %s", (enabled) => {
    useSettings.mockReturnValue({ data: { ...settings(false).data, fresh_clones_enabled: enabled } })
    renderPage("/settings#job-clones")
    const section = screen.getByRole("region", { name: "Repositories" })
    const anchor = section.querySelector("#job-clones")
    expect(anchor).toBeTruthy()
    expect(within(anchor as HTMLElement).getByRole("link", { name: "Open Remote" }).getAttribute("href")).toBe("/remote")
    expect(mockedApiFetch.mock.calls.some(([, path]) => path === "/api/job-clones")).toBe(false)
  })

  it("keeps section navigation in browser history", () => {
    render(<QueryClientProvider client={createQueryClient()}><MemoryRouter initialEntries={["/settings"]}><SettingsPage /><HistoryBack /></MemoryRouter></QueryClientProvider>)
    openSection(/^Security/)
    openSection("Reeve")
    fireEvent.click(screen.getByRole("button", { name: "Browser Back" }))
    expect(screen.getByRole("region", { name: "Security" })).toBeVisible()
    expect(screen.queryByRole("region", { name: "Reeve" })).toBeNull()
  })

  it("opens at the setting a link names", async () => {
    const scroll = vi.fn()
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value: scroll })
    serveRegistry()
    renderPage("/settings#setting-voice.whisper_url")
    await waitFor(() => expect(scroll).toHaveBeenCalled())
    expect(scroll.mock.contexts.at(-1)).toBe(document.getElementById("setting-voice.whisper_url"))
  })

  it("marks what is kept in this browser only", () => {
    renderPage("/settings#appearance")
    expect(within(screen.getByText("App theme").parentElement!).getByText(/this browser/i)).toBeVisible()
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
    ["theme_control", "Allow MQTT theme control", false],
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
    expect(mqtt.getByText(/every broker-authorized writer/i)).toBeTruthy()
    fireEvent.click(mqtt.getByRole("button", { name: "Allow MQTT theme control" }))
    await waitFor(() => expect(mqtt.getByRole("button", { name: "Allow MQTT theme control" })).toHaveAttribute("aria-pressed", "true"))
    expect(mqtt.getByRole("button", { name: "MQTT publisher" })).toHaveAttribute("aria-pressed", "false")
    const broker = mqtt.getByRole("textbox", { name: "MQTT broker" })
    fireEvent.change(broker, { target: { value: "mqtts://broker.example:8883" } })
    fireEvent.click(within(broker.closest("[data-testid]") as HTMLElement).getByRole("button", { name: "Save" }))
    await waitFor(() => expect(mockedApiFetch).toHaveBeenCalledWith("https://remote.example", "/api/config/settings/integrations.mqtt.broker", expect.objectContaining({
      method: "PUT", body: JSON.stringify({ value: "mqtts://broker.example:8883" }),
    })))
    expect(await mqtt.findByText("Restart to apply")).toBeTruthy()
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
    fireEvent.click(within(broker.closest("[data-testid]") as HTMLElement).getByRole("button", { name: "Save" }))
    expect(await within(broker.closest("[data-testid]") as HTMLElement).findByRole("alert")).toHaveTextContent("want mqtt:// or mqtts://")
    expect(broker).toHaveValue("https://wrong")
    expect(screen.queryByText("Restart to apply")).toBeNull()

    mockedApiFetch.mockImplementation(async (_base, path, init) => {
      if (path === "/api/config/settings") return Response.json({ settings: values })
      if (init?.method === "PUT") return Response.json({ error: "MQTT broker is pinned by config.yaml" }, { status: 409 })
      return new Response("{}", { status: 404 })
    })
    fireEvent.change(broker, { target: { value: "mqtts://broker.example" } })
    fireEvent.click(within(broker.closest("[data-testid]") as HTMLElement).getByRole("button", { name: "Save" }))
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

describe("SettingsPage server theme", () => {
  afterEach(() => cleanup())
  it("mounts the server preference editor separately from the browser picker", async () => {
    useSettings.mockReturnValue(settings(false))
    localStorage.setItem("legato:app-theme", "dracula")
    mockedApiFetch.mockImplementation(async (_base, path, init) => {
      if (path === "/api/settings/theme") return Response.json({ id: init?.method === "PUT" ? "ocular-dark" : "", revision: init?.method === "PUT" ? 1 : 0 })
      return new Response("{}", { status: 404 })
    })
    renderPage("/settings#appearance")
    const preference = await screen.findByRole("combobox", { name: "Preferred theme" })
    expect(screen.getByRole("radiogroup", { name: "App theme" })).toBeTruthy()
    fireEvent.change(preference, { target: { value: "ocular-dark" } })
    await waitFor(() => expect(mockedApiFetch).toHaveBeenCalledWith("https://remote.example", "/api/settings/theme", expect.objectContaining({ method: "PUT", body: JSON.stringify({ id: "ocular-dark", expected_revision: 0 }) })))
    expect(localStorage.getItem("legato:app-theme")).toBe("dracula")
    localStorage.clear()
  })
})
