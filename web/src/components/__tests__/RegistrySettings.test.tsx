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

import { RegistrySettings } from "../settings/RegistrySettings"
import type { ConfigSetting } from "../../lib/configSettings"

const setting = (over: Partial<ConfigSetting>): ConfigSetting => ({
  key: "agents.reviews", title: "Reviews", kind: "bool", class: "change", mode: "restart",
  value: true, source: "default", pinned: false, ...over,
})

function wrap(children: ReactNode) {
  return <QueryClientProvider client={new QueryClient()}>{children}</QueryClientProvider>
}

describe("RegistrySettings", () => {
  beforeEach(() => {
    mockMovable.mockReset(); mockMovable.mockResolvedValue({ settings: [], entries: [] })
    mockMove.mockReset(); mockMove.mockResolvedValue(undefined)
    mockSet.mockReset(); mockSet.mockResolvedValue({})
    mockReset.mockReset(); mockReset.mockResolvedValue({})
  })
  afterEach(() => cleanup())

  it("keeps text local until Apply and discards it with Cancel", async () => {
    render(wrap(<RegistrySettings baseUrl="" prefixes={["theme"]} settings={[setting({ key: "theme", title: "Theme", kind: "string", value: "dark" })]} />))
    const input = screen.getByRole("textbox", { name: "Theme" })
    fireEvent.change(input, { target: { value: "light" } })
    fireEvent.blur(input)
    expect(mockSet).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    expect((input as HTMLInputElement).value).toBe("dark")
    fireEvent.change(input, { target: { value: "light" } })
    fireEvent.click(screen.getByRole("button", { name: "Apply" }))
    await waitFor(() => expect(mockSet).toHaveBeenCalledWith("", "theme", "light"))
  })

  it("preserves dirty text on refresh and resolves external changes explicitly", async () => {
    const client = new QueryClient()
    const view = (value: string) => <QueryClientProvider client={client}><RegistrySettings baseUrl="" prefixes={["theme"]} settings={[setting({ key: "theme", title: "Theme", kind: "string", value })]} /></QueryClientProvider>
    const { rerender } = render(view("dark"))
    const input = screen.getByRole("textbox", { name: "Theme" }) as HTMLInputElement
    fireEvent.change(input, { target: { value: "mine" } })
    rerender(view("dark"))
    expect(input.value).toBe("mine")
    rerender(view("external"))
    expect(input.value).toBe("mine")
    expect((screen.getByRole("button", { name: "Apply" }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.click(screen.getByRole("button", { name: "Keep my edit" }))
    fireEvent.click(screen.getByRole("button", { name: "Apply" }))
    await waitFor(() => expect(mockSet).toHaveBeenCalledWith("", "theme", "mine"))
    rerender(view("another"))
    fireEvent.click(screen.getByRole("button", { name: "Reload value" }))
    expect(input.value).toBe("another")
    rerender(view("latest"))
    expect(input.value).toBe("latest")
  })

  it("shows saving and saved restart feedback without allowing duplicate submissions", async () => {
    let finish!: (value: unknown) => void
    mockSet.mockImplementation(() => new Promise(resolve => { finish = resolve }))
    render(wrap(<RegistrySettings baseUrl="" prefixes={["theme"]} settings={[setting({ key: "theme", title: "Theme", kind: "string", value: "dark" })]} />))
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "light" } })
    fireEvent.click(screen.getByRole("button", { name: "Apply" }))
    expect(screen.getByRole("status").textContent).toBe("Saving…")
    fireEvent.click(screen.getByRole("button", { name: "Apply" }))
    expect(mockSet).toHaveBeenCalledTimes(1)
    finish(setting({ value: "light", pending_restart: true }))
    await waitFor(() => expect(screen.getByRole("status").textContent).toBe("Saved — applies at restart."))
  })

  it("keeps the toggle mounted and disabled while its save is pending", () => {
    mockSet.mockReturnValue(new Promise(() => {}))
    render(wrap(<RegistrySettings baseUrl="" prefixes={["agents"]} settings={[setting({})]} />))
    const toggle = screen.getByRole("button", { name: "Reviews" })
    toggle.focus()
    fireEvent.click(toggle)
    expect(screen.getByRole("button", { name: "Reviews" })).toBe(toggle)
    expect((toggle as HTMLButtonElement).disabled).toBe(true)
  })

  it("offers only server-eligible moves and requires confirmation", async () => {
    mockMovable.mockResolvedValue({ settings: ["theme"], entries: [] })
    render(wrap(<RegistrySettings baseUrl="http://legato" prefixes={["theme", "icons"]} settings={[
      setting({ key: "theme", title: "Theme", kind: "string", value: "dark", pinned: true }),
      setting({ key: "icons", title: "Icons", kind: "string", value: "unicode", pinned: true }),
    ]} />))
    const move = await within(screen.getByTestId("setting-theme")).findByRole("button", { name: /Move to the database/ })
    await waitFor(() => expect(mockMovable).toHaveBeenCalledWith("http://legato"))
    expect(within(screen.getByTestId("setting-icons")).queryByRole("button", { name: /Move to the database/ })).toBeNull()
    fireEvent.click(move)
    expect(mockMove).not.toHaveBeenCalled()
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Cancel" }))
    expect(mockMove).not.toHaveBeenCalled()
    await waitFor(() => expect((move as HTMLButtonElement).disabled).toBe(false))
    fireEvent.click(move)
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Move and restart" }))
    await waitFor(() => expect(mockMove).toHaveBeenCalledWith("http://legato", { settings: ["theme"], entries: [] }))
    await waitFor(() => expect(screen.getByRole("status").textContent).toContain("Restarting"))
  })

  it("accepts the saved canonical value as clean and follows later refreshes", async () => {
    const client = new QueryClient()
    const view = (value: number) => <QueryClientProvider client={client}><RegistrySettings baseUrl="" prefixes={["limit"]} settings={[setting({ key: "limit", title: "Limit", kind: "int", value })]} /></QueryClientProvider>
    const { rerender } = render(view(1))
    mockSet.mockImplementation(async () => {
      rerender(view(2))
      return setting({ key: "limit", kind: "int", value: 2 })
    })
    const input = screen.getByRole("textbox") as HTMLInputElement
    fireEvent.change(input, { target: { value: "02" } })
    fireEvent.click(screen.getByRole("button", { name: "Apply" }))
    await waitFor(() => expect(input.value).toBe("2"))
    expect(screen.queryByRole("button", { name: "Keep my edit" })).toBeNull()
    expect((screen.getByRole("button", { name: "Apply" }) as HTMLButtonElement).disabled).toBe(true)
    rerender(view(3))
    expect(input.value).toBe("3")
  })

  it("marks a successful save clean even before refreshed props arrive", async () => {
    mockSet.mockResolvedValue(setting({ key: "theme", kind: "string", value: "light" }))
    render(wrap(<RegistrySettings baseUrl="" prefixes={["theme"]} settings={[setting({ key: "theme", title: "Theme", kind: "string", value: "dark" })]} />))
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "light" } })
    fireEvent.click(screen.getByRole("button", { name: "Apply" }))
    await waitFor(() => expect(screen.getByRole("status").textContent).toBe("Saved."))
    expect((screen.getByRole("textbox") as HTMLInputElement).value).toBe("light")
    expect((screen.getByRole("button", { name: "Apply" }) as HTMLButtonElement).disabled).toBe(true)
  })

  it("says where each value comes from", () => {
    render(wrap(<RegistrySettings baseUrl="" prefixes={["agents", "icons", "theme"]} settings={[
      setting({}),
      setting({ key: "icons", title: "Icons", kind: "enum", enum: ["unicode", "nerdfonts"], value: "nerdfonts", source: "config.yaml", pinned: true }),
      setting({ key: "theme", title: "Theme", kind: "string", value: "dark", source: "database", pending_restart: true }),
    ]} />))

    expect(screen.getByTestId("source-agents.reviews").textContent).toContain("default")
    expect(screen.getByTestId("source-icons").textContent).toContain("config.yaml")
    expect(screen.getByTestId("source-theme").textContent).toContain("set here")
    expect(screen.getByTestId("source-theme").textContent).toContain("applies at restart")
  })

  it("gives each setting an anchor a link can open it at", () => {
    render(wrap(<RegistrySettings baseUrl="" prefixes={["agents"]} settings={[setting({})]} />))
    expect(document.getElementById("setting-agents.reviews")).not.toBeNull()
  })

  it("locks a pinned setting", () => {
    render(wrap(<RegistrySettings baseUrl="" prefixes={["icons"]} settings={[
      setting({ key: "icons", title: "Icons", kind: "enum", enum: ["unicode", "nerdfonts"], value: "nerdfonts", source: "config.yaml", pinned: true }),
    ]} />))
    expect((screen.getByRole("combobox", { name: "Icons" }) as HTMLSelectElement).disabled).toBe(true)
    expect(screen.getByLabelText("Pinned by config.yaml")).toBeTruthy()
    expect(screen.queryByRole("button", { name: /Reset/ })).toBeNull()
  })

  it("changes a setting and resets one set here", async () => {
    render(wrap(<RegistrySettings baseUrl="http://legato" prefixes={["agents", "theme"]} settings={[
      setting({}),
      setting({ key: "theme", title: "Theme", kind: "string", value: "dark", source: "database" }),
    ]} />))

    fireEvent.click(screen.getByRole("button", { name: "Reviews" }))
    await waitFor(() => expect(mockSet).toHaveBeenCalledWith("http://legato", "agents.reviews", false))

    fireEvent.click(within(screen.getByTestId("setting-theme")).getByRole("button", { name: "Reset Theme" }))
    await waitFor(() => expect(mockReset).toHaveBeenCalledWith("http://legato", "theme"))
  })

  it("says why a change was refused", async () => {
    mockSet.mockRejectedValue(new Error("agents.sandbox.backend: only bwrap is supported"))
    render(wrap(<RegistrySettings baseUrl="" prefixes={["agents"]} settings={[
      setting({ key: "agents.sandbox.backend", title: "Sandbox backend", kind: "string", value: "bwrap" }),
    ]} />))
    const input = screen.getByRole("textbox", { name: "Sandbox backend" })
    fireEvent.change(input, { target: { value: "nspawn" } })
    fireEvent.click(screen.getByRole("button", { name: "Apply" }))
    expect((await screen.findByRole("alert")).textContent).toContain("only bwrap")
  })

  it("says an own rule of the Reeve waits for a signed approval", async () => {
    mockSet.mockResolvedValue({ status: "needs_signature", approval_id: "ap-1" })
    render(wrap(<RegistrySettings baseUrl="" prefixes={["reeve"]} settings={[
      setting({ key: "reeve.levels.risky", title: "What a risky change needs", kind: "enum", enum: ["", "auto", "approve"], value: "", own_rule: true }),
    ]} />))
    fireEvent.change(screen.getByRole("combobox", { name: "What a risky change needs" }), { target: { value: "approve" } })
    await waitFor(() => expect(screen.getByRole("status").textContent).toContain("approve it on your phone"))
  })

  it("leaves out settings a section of their own shows", () => {
    render(wrap(<RegistrySettings baseUrl="" prefixes={["web"]} exclude={["web.tint"]} settings={[
      setting({ key: "web.tint", title: "Tint", kind: "enum", enum: ["", "red"], value: "" }),
    ]} />))
    expect(screen.queryByTestId("setting-web.tint")).toBeNull()
  })
})
