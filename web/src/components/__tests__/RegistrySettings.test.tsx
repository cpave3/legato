import { describe, it, expect, vi, afterEach, beforeEach } from "vitest"
import { render, screen, cleanup, fireEvent, waitFor, within } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { ReactNode } from "react"

const mockSet = vi.fn()
const mockReset = vi.fn()
const mockMovable = vi.fn()
const mockMove = vi.fn()
vi.mock("../../lib/configSettings", async (importActual) => ({
  ...(await importActual<typeof import("../../lib/configSettings")>()),
  setConfigSetting: (...a: unknown[]) => mockSet(...a),
  fetchMovable: (...a: unknown[]) => mockMovable(...a),
  moveSettings: (...a: unknown[]) => mockMove(...a),
  resetConfigSetting: (...a: unknown[]) => mockReset(...a),
}))

import { FileOnlyRow, RegistrySettings } from "../settings/RegistrySettings"
import { ShowKeysContext } from "../settings/showKeys"
import type { ConfigSetting } from "../../lib/configSettings"

const setting = (over: Partial<ConfigSetting>): ConfigSetting => ({
  key: "agents.reviews", title: "Review tour at the end", kind: "bool", class: "change", mode: "restart",
  value: true, default: true, source: "default", pinned: false, ...over,
})

const workers = (over: Partial<ConfigSetting> = {}) => setting({
  key: "swarm.max_concurrent_agents", title: "Workers at once", help: "Live workers in one swarm.", kind: "int",
  value: 0, default: 0, default_label: "4", ...over,
})

function wrap(children: ReactNode, showKeys = false) {
  return <QueryClientProvider client={new QueryClient()}><ShowKeysContext.Provider value={showKeys}>{children}</ShowKeysContext.Provider></QueryClientProvider>
}

const rows = (settings: ConfigSetting[], showMode?: boolean) => <RegistrySettings baseUrl="http://legato" prefixes={settings.map((s) => s.key)} settings={settings} showMode={showMode} />

describe("A setting row", () => {
  beforeEach(() => {
    mockMovable.mockReset(); mockMovable.mockResolvedValue({ settings: [], entries: [] })
    mockMove.mockReset(); mockMove.mockResolvedValue(undefined)
    mockSet.mockReset(); mockSet.mockResolvedValue({})
    mockReset.mockReset(); mockReset.mockResolvedValue({})
  })
  afterEach(() => cleanup())

  it("shows a default with no chip, its value greyed and its label as the placeholder of an unset number", () => {
    render(wrap(rows([workers()])))
    const row = within(screen.getByTestId("setting-swarm.max_concurrent_agents"))
    const input = row.getByRole("textbox", { name: "Workers at once" }) as HTMLInputElement
    expect(input.value).toBe("")
    expect(input.placeholder).toBe("4")
    expect(row.getByText("Live workers in one swarm.")).toBeTruthy()
    expect(row.queryByText("Set here")).toBeNull()
    expect(row.queryByText(/config.yaml/)).toBeNull()
    expect(row.queryByRole("button", { name: /^(Save|Cancel|Apply)$/ })).toBeNull()
  })

  it("puts a unit beside a number, and shows bytes as KB or MB", () => {
    render(wrap(rows([
      setting({ key: "swarm.brief_kickoff_delay_ms", title: "Kickoff delay", kind: "int", unit: "ms", value: 250, default: 0, default_label: "250", source: "database" }),
      setting({ key: "jira.attachment_max_size_bytes", title: "Largest attachment", kind: "int", unit: "bytes", value: 26214400, default: 26214400 }),
    ])))
    expect(within(screen.getByTestId("setting-swarm.brief_kickoff_delay_ms")).getByText("ms")).toBeTruthy()
    const bytes = within(screen.getByTestId("setting-jira.attachment_max_size_bytes"))
    expect((bytes.getByRole("textbox") as HTMLInputElement).value).toBe("25")
    expect(bytes.getByText("MB")).toBeTruthy()
  })

  it("saves a number in bytes as bytes", async () => {
    render(wrap(rows([setting({ key: "jira.attachment_max_size_bytes", title: "Largest attachment", kind: "int", unit: "bytes", value: 26214400, default: 26214400 })])))
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "10" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(mockSet).toHaveBeenCalledWith("http://legato", "jira.attachment_max_size_bytes", 10485760))
  })

  it("shows Save and Cancel only once a typed value changes; Enter saves and Esc cancels", async () => {
    render(wrap(rows([setting({ key: "editor", title: "TUI editor", kind: "string", value: "vim", default: "", source: "database" })])))
    const input = screen.getByRole("textbox", { name: "TUI editor" }) as HTMLInputElement
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull()
    fireEvent.change(input, { target: { value: "nano" } })
    expect(screen.getByRole("button", { name: "Save" })).toBeTruthy()
    fireEvent.blur(input)
    expect(mockSet).not.toHaveBeenCalled()
    fireEvent.keyDown(input, { key: "Escape" })
    expect(input.value).toBe("vim")
    expect(screen.queryByRole("button", { name: "Cancel" })).toBeNull()
    fireEvent.change(input, { target: { value: "hx" } })
    fireEvent.keyDown(input, { key: "Enter" })
    await waitFor(() => expect(mockSet).toHaveBeenCalledWith("http://legato", "editor", "hx"))
  })

  it("saves a switch and a menu at once", async () => {
    render(wrap(rows([setting({}), setting({ key: "icons", title: "TUI icons", kind: "enum", enum: ["unicode", "nerdfonts"], value: "unicode", default: "unicode" })])))
    fireEvent.click(screen.getByRole("button", { name: "Review tour at the end" }))
    await waitFor(() => expect(mockSet).toHaveBeenCalledWith("http://legato", "agents.reviews", false))
    fireEvent.change(screen.getByRole("combobox", { name: "TUI icons" }), { target: { value: "nerdfonts" } })
    await waitFor(() => expect(mockSet).toHaveBeenCalledWith("http://legato", "icons", "nerdfonts"))
  })

  it("says Set here with Reset to the default, and resets", async () => {
    render(wrap(rows([workers({ value: 6, source: "database" })])))
    const row = within(screen.getByTestId("setting-swarm.max_concurrent_agents"))
    expect(row.getByText("Set here")).toBeTruthy()
    fireEvent.click(row.getByRole("button", { name: /^Reset to 4/ }))
    await waitFor(() => expect(mockReset).toHaveBeenCalledWith("http://legato", "swarm.max_concurrent_agents"))
  })

  it("marks a value that waits for a restart, and after saving says what Legato uses until then", async () => {
    mockSet.mockResolvedValue(workers({ value: 6, source: "database", pending_restart: true, started_value: 0 }))
    const { rerender } = render(wrap(rows([workers()])))
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "6" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(await screen.findByText("Saved. Legato uses 4 until it restarts.")).toBeTruthy()
    rerender(wrap(rows([workers({ value: 6, source: "database", pending_restart: true, started_value: 0 })])))
    expect(screen.getByText("Restart to apply")).toBeTruthy()
  })

  it("says Saved for a live setting, which fades", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    mockSet.mockResolvedValue(setting({ key: "web.rail.done", mode: "live", value: false }))
    render(wrap(rows([setting({ key: "web.rail.done", title: "Finished work", mode: "live" })])))
    fireEvent.click(screen.getByRole("button", { name: "Finished work" }))
    expect(await screen.findByText("Saved.")).toBeTruthy()
    vi.advanceTimersByTime(5000)
    await waitFor(() => expect(screen.queryByText("Saved.")).toBeNull())
    vi.useRealTimers()
  })

  it("keeps the draft and shows the server's reason when a value is refused", async () => {
    mockSet.mockRejectedValue(new Error("must be 1 or more"))
    render(wrap(rows([workers()])))
    const input = screen.getByRole("textbox") as HTMLInputElement
    fireEvent.change(input, { target: { value: "-1" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect((await screen.findByRole("alert")).textContent).toBe("must be 1 or more")
    expect(input.value).toBe("-1")
    expect(screen.getByRole("button", { name: "Save" })).toBeTruthy()
  })

  it("offers Use or Keep when the value changed elsewhere while you edited", async () => {
    const client = new QueryClient()
    const view = (value: number) => <QueryClientProvider client={client}>{rows([workers({ value, source: "database" })])}</QueryClientProvider>
    const { rerender } = render(view(6))
    const input = screen.getByRole("textbox") as HTMLInputElement
    fireEvent.change(input, { target: { value: "7" } })
    rerender(view(8))
    expect(screen.getByText("Changed to 8 while you edited.")).toBeTruthy()
    expect(input.value).toBe("7")
    expect((screen.getByRole("button", { name: "Save" }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.click(screen.getByRole("button", { name: "Keep 7" }))
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(mockSet).toHaveBeenCalledWith("http://legato", "swarm.max_concurrent_agents", 7))
    fireEvent.change(input, { target: { value: "9" } })
    rerender(view(5))
    fireEvent.click(screen.getByRole("button", { name: "Use 5" }))
    expect(input.value).toBe("5")
  })

  it("accepts the saved value as clean and follows later refreshes", async () => {
    const client = new QueryClient()
    const view = (value: number) => <QueryClientProvider client={client}>{rows([workers({ value, source: "database" })])}</QueryClientProvider>
    const { rerender } = render(view(1))
    mockSet.mockImplementation(async () => {
      rerender(view(2))
      return workers({ value: 2, source: "database" })
    })
    const input = screen.getByRole("textbox") as HTMLInputElement
    fireEvent.change(input, { target: { value: "02" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(input.value).toBe("2"))
    expect(screen.queryByRole("button", { name: /^Keep/ })).toBeNull()
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull()
    rerender(view(3))
    expect(input.value).toBe("3")
  })

  it("names a menu's empty choice by its default label, else None, and greys a default", () => {
    render(wrap(rows([
      setting({ key: "a.mode", title: "Mode", kind: "enum", enum: ["", "fast"], value: "", default: "", default_label: "Automatic" }),
      setting({ key: "b.mode", title: "Other mode", kind: "enum", enum: ["", "slow"], value: "slow", default: "", source: "database" }),
    ])))
    const mode = screen.getByRole("combobox", { name: "Mode" })
    expect(within(mode).getAllByRole("option").map((o) => o.textContent)).toEqual(["Automatic", "fast"])
    expect(mode.className).toContain("text-lw-text-2")
    const other = screen.getByRole("combobox", { name: "Other mode" })
    expect(within(other).getAllByRole("option").map((o) => o.textContent)).toEqual(["None", "slow"])
    expect(other.className).toContain("text-lw-text")
    expect(other.className).not.toContain("text-lw-text-2")
  })

  it("draws a number narrow and numeric, and greys it only while it is the default and untouched", () => {
    render(wrap(rows([workers()])))
    const input = screen.getByRole("textbox") as HTMLInputElement
    expect(input.className).toContain("w-24 text-right")
    expect(input.getAttribute("inputmode")).toBe("numeric")
    expect(input.className).toContain("text-lw-text-2")
    fireEvent.change(input, { target: { value: "3" } })
    expect(input.className).not.toContain("text-lw-text-2")
    fireEvent.keyDown(input, { key: "Escape" })
    fireEvent.keyDown(input, { key: "Escape" })
    expect(input.value).toBe("")
  })

  it("names the default when the value changed back to it while you edited", () => {
    const client = new QueryClient()
    const view = (value: number, source: ConfigSetting["source"]) => <QueryClientProvider client={client}>{rows([workers({ value, source })])}</QueryClientProvider>
    const { rerender } = render(view(6, "database"))
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "7" } })
    rerender(view(0, "default"))
    expect(screen.getByText("Changed to 4 while you edited.")).toBeTruthy()
    expect(screen.getByRole("button", { name: "Use 4" })).toBeTruthy()
  })

  it("saves once while a save is on its way", async () => {
    let finish!: (value: unknown) => void
    mockSet.mockImplementation(() => new Promise(resolve => { finish = resolve }))
    render(wrap(rows([workers()])))
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "5" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(screen.getByText("Saving…")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(mockSet).toHaveBeenCalledTimes(1)
    finish(workers({ value: 5 }))
    await waitFor(() => expect(screen.queryByText("Saving…")).toBeNull())
  })

  it("shows a pinned value read-only, with a menu to move it out of config.yaml or copy its key", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } })
    mockMovable.mockResolvedValue({ settings: ["agents.default_tool"], entries: [] })
    render(wrap(rows([setting({ key: "agents.default_tool", title: "Default agent", kind: "enum", enum: ["", "claude-code", "codex"], value: "claude-code", default: "", source: "config.yaml", pinned: true })])))
    const row = within(screen.getByTestId("setting-agents.default_tool"))
    expect((row.getByRole("combobox", { name: "Default agent" }) as HTMLSelectElement).disabled).toBe(true)
    expect(row.getByLabelText("Pinned by config.yaml")).toBeTruthy()
    expect(row.queryByRole("button", { name: /^Reset/ })).toBeNull()
    fireEvent.click(row.getByRole("button", { name: "More for Default agent" }))
    fireEvent.click(await row.findByRole("menuitem", { name: /Copy key/ }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith("agents.default_tool"))
    fireEvent.click(row.getByRole("button", { name: "More for Default agent" }))
    fireEvent.click(await row.findByRole("menuitem", { name: /Move out of config.yaml/ }))
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Cancel" }))
    expect(mockMove).not.toHaveBeenCalled()
    fireEvent.click(row.getByRole("button", { name: "More for Default agent" }))
    fireEvent.click(await row.findByRole("menuitem", { name: /Move out of config.yaml/ }))
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Move and restart" }))
    await waitFor(() => expect(mockMove).toHaveBeenCalledWith("http://legato", { settings: ["agents.default_tool"], entries: [] }))
    expect(await row.findByText("Restarting…")).toBeTruthy()
  })

  it("offers no move for a pinned setting the server cannot move", async () => {
    render(wrap(rows([setting({ key: "icons", title: "TUI icons", kind: "enum", enum: ["unicode"], value: "unicode", source: "config.yaml", pinned: true })])))
    fireEvent.click(screen.getByRole("button", { name: "More for TUI icons" }))
    await waitFor(() => expect(mockMovable).toHaveBeenCalled())
    expect(screen.queryByRole("menuitem", { name: /Move out/ })).toBeNull()
    expect(screen.getByRole("menuitem", { name: /Copy key/ })).toBeTruthy()
  })

  it("marks one of the Reeve's own rules Signed, and says a change waits for the signature", async () => {
    mockSet.mockResolvedValue({ status: "needs_signature", approval_id: "ap-1" })
    render(wrap(rows([setting({ key: "reeve.levels.risky", title: "Approval for a risky change", kind: "enum", enum: ["", "approve", "signed"], value: "", default_label: "default", own_rule: true })])))
    expect(screen.getByText("Signed")).toBeTruthy()
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "approve" } })
    expect(await screen.findByText(/It changes once you approve/)).toBeTruthy()
  })

  it("shows its key on hover and focus, and on every row with Show config keys", () => {
    const { unmount } = render(wrap(rows([workers()])))
    const row = screen.getByTestId("setting-swarm.max_concurrent_agents")
    expect(within(row).queryByText(/swarm.max_concurrent_agents/)).toBeNull()
    fireEvent.mouseEnter(row)
    expect(within(row).getByRole("button", { name: "Copy key swarm.max_concurrent_agents" })).toBeTruthy()
    fireEvent.mouseLeave(row)
    expect(within(row).queryByRole("button", { name: /Copy key/ })).toBeNull()
    fireEvent.focus(within(row).getByRole("textbox"))
    expect(within(row).getByRole("button", { name: /Copy key/ })).toBeTruthy()
    unmount()
    render(wrap(rows([workers()]), true))
    expect(screen.getByRole("button", { name: "Copy key swarm.max_concurrent_agents" })).toBeTruthy()
  })

  it("says when it applies on the row when its group does not share one", () => {
    render(wrap(rows([setting({ mode: "new_agents" })], true)))
    expect(screen.getByText("Applies to new agents")).toBeTruthy()
  })

  it("gives each setting an anchor a link can open it at", () => {
    render(wrap(rows([setting({})])))
    expect(document.getElementById("setting-agents.reviews")).not.toBeNull()
  })

  it("leaves out settings a card of their own shows", () => {
    render(wrap(<RegistrySettings baseUrl="" prefixes={["web"]} exclude={["web.tint"]} settings={[setting({ key: "web.tint", title: "Tint", kind: "enum", enum: ["", "red"], value: "" })]} />))
    expect(screen.queryByTestId("setting-web.tint")).toBeNull()
  })
})

describe("A secret setting's row", () => {
  afterEach(() => cleanup())
  const token = (over: Partial<ConfigSetting>) => setting({ key: "jira.api_token", title: "API token", kind: "string", secret: true, value: "legato://jira-api-token", source: "database", ...over })

  it("says Stored, and offers the command that replaces it", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } })
    render(wrap(rows([token({ secret_status: "stored" })])))
    expect(screen.getByText("Stored")).toBeTruthy()
    expect(screen.queryByText("legato secret add jira-api-token")).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Replace" }))
    fireEvent.click(screen.getByRole("button", { name: "Copy legato secret add jira-api-token" }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith("legato secret add jira-api-token"))
  })

  it("says Not found, with the command that stores it", () => {
    render(wrap(rows([token({ secret_status: "missing" })])))
    expect(screen.getByText("Not found")).toBeTruthy()
    expect(screen.getByText("legato secret add jira-api-token")).toBeTruthy()
  })

  it("names no command for a 1Password secret Legato cannot read", () => {
    render(wrap(rows([token({ value: "op://Work/Jira/token", secret_status: "missing" })])))
    expect(screen.getByText("Not found")).toBeTruthy()
    expect(screen.queryByText(/legato secret add/)).toBeNull()
  })
})

describe("A file-only row", () => {
  afterEach(() => cleanup())
  it("shows its value read-only, says config.yaml only, and has no control", () => {
    render(wrap(<FileOnlyRow keyName="web.port" title="Web port" value="3080" />))
    const row = within(screen.getByTestId("setting-web.port"))
    expect(row.getByText("Web port")).toBeTruthy()
    expect(row.getByText("config.yaml only")).toBeTruthy()
    expect(row.getByText("3080")).toBeTruthy()
    expect(row.queryByRole("textbox")).toBeNull()
  })
})
