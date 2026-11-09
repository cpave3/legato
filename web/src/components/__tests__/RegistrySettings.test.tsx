import { describe, it, expect, vi, afterEach, beforeEach } from "vitest"
import { render, screen, cleanup, fireEvent, waitFor, within } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { ReactNode } from "react"

const mockSet = vi.fn()
const mockReset = vi.fn()
vi.mock("../../lib/configSettings", async (importActual) => ({
  ...(await importActual<typeof import("../../lib/configSettings")>()),
  setConfigSetting: (...a: unknown[]) => mockSet(...a),
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
    mockSet.mockReset(); mockSet.mockResolvedValue({})
    mockReset.mockReset(); mockReset.mockResolvedValue({})
  })
  afterEach(() => cleanup())

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
    fireEvent.blur(input)
    expect((await screen.findByRole("alert")).textContent).toContain("only bwrap")
  })

  it("says an own rule of the Reeve waits for a signed approval", async () => {
    mockSet.mockResolvedValue({ status: "needs_signature", approval_id: "ap-1" })
    render(wrap(<RegistrySettings baseUrl="" prefixes={["reeve"]} settings={[
      setting({ key: "reeve.levels.risky", title: "What a risky change needs", kind: "enum", enum: ["", "auto", "approve"], value: "", own_rule: true }),
    ]} />))
    fireEvent.change(screen.getByRole("combobox", { name: "What a risky change needs" }), { target: { value: "approve" } })
    expect((await screen.findByRole("status")).textContent).toContain("approve it on your phone")
  })

  it("leaves out settings a section of their own shows", () => {
    render(wrap(<RegistrySettings baseUrl="" prefixes={["web"]} exclude={["web.tint"]} settings={[
      setting({ key: "web.tint", title: "Tint", kind: "enum", enum: ["", "red"], value: "" }),
    ]} />))
    expect(screen.queryByTestId("setting-web.tint")).toBeNull()
  })
})
