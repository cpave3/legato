import { describe, it, expect, vi, afterEach } from "vitest"
import { render, screen, waitFor, cleanup, within } from "@testing-library/react"

const mockFetch = vi.fn()
vi.mock("../../lib/integrations", () => ({
  fetchIntegrations: (...a: unknown[]) => mockFetch(...a),
}))

import { IntegrationsSettings } from "../IntegrationsSettings"

const connected = {
  tokens: [
    { id: "it-1", name: "argus", scopes: ["owner:read", "delegate", "attention"], created_at: "2026-11-01T03:00:00Z", last_used_at: "2026-11-01T04:00:00Z" },
    { id: "it-0", name: "old", scopes: ["owner:read"], created_at: "2026-10-30T03:00:00Z", revoked_at: "2026-10-31T03:00:00Z" },
  ],
  argus: {
    configured: true, url: "http://localhost:5173", delivery: "ask", agent: "claude-code",
    connection: { name: "argus", url: "http://localhost:5173", state: "connected", connected_at: "2026-11-01T03:10:00Z", replies: { pending: 2, failed: 1 } },
    open_cases: 3,
  },
}

describe("IntegrationsSettings", () => {
  afterEach(() => cleanup())

  it("shows Argus's settings, its connection and the tokens, never a token itself", async () => {
    mockFetch.mockResolvedValue(connected)
    render(<IntegrationsSettings baseUrl="http://legato" />)
    const argus = await screen.findByRole("region", { name: "Argus" })
    expect(within(argus).getByRole("link", { name: "http://localhost:5173" }).getAttribute("href")).toBe("http://localhost:5173")
    expect(within(argus).getByText(/after you approve/)).toBeTruthy()
    expect(within(argus).getByText("claude-code")).toBeTruthy()
    expect(within(argus).getByText(/Connected/)).toBeTruthy()
    expect(within(argus).getByText(/2 waiting/)).toBeTruthy()
    expect(within(argus).getByText(/1 failed/)).toBeTruthy()
    expect(within(argus).getByText(/3 open/)).toBeTruthy()

    const tokens = screen.getByRole("region", { name: "Integration tokens" })
    const live = within(tokens).getByText("argus").closest("li") as HTMLElement
    expect(within(live).getByText("attention")).toBeTruthy()
    expect(within(live).queryByText("revoked")).toBeNull()
    const old = within(tokens).getByText("old").closest("li") as HTMLElement
    expect(within(old).getByText("revoked")).toBeTruthy()
    expect(mockFetch).toHaveBeenCalledWith("http://legato")
  })

  it("says what to do when Argus is not set up", async () => {
    mockFetch.mockResolvedValue({ tokens: [], argus: { configured: false, connection: { name: "argus", state: "none", replies: {} }, open_cases: 0 } })
    render(<IntegrationsSettings baseUrl="" />)
    const argus = await screen.findByRole("region", { name: "Argus" })
    expect(within(argus).getByText(/integrations\.argus\.url/)).toBeTruthy()
    expect(screen.getByText(/No integration tokens/)).toBeTruthy()
  })

  it("says to connect again when Argus refused Legato", async () => {
    mockFetch.mockResolvedValue({ ...connected, argus: { ...connected.argus, connection: { ...connected.argus.connection, state: "disconnected" } } })
    render(<IntegrationsSettings baseUrl="" />)
    const argus = await screen.findByRole("region", { name: "Argus" })
    expect(within(argus).getByText(/legato integration connect argus/)).toBeTruthy()
  })

  it("comes into view when the address names it, as Attention's link to it does", async () => {
    mockFetch.mockResolvedValue(connected)
    const scroll = vi.fn()
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value: scroll })
    window.history.replaceState(null, "", "/settings#integrations")
    render(<IntegrationsSettings baseUrl="" />)
    await waitFor(() => expect(scroll).toHaveBeenCalledWith({ block: "start" }))
    window.history.replaceState(null, "", "/")
  })

  it("shows an error it could not load", async () => {
    mockFetch.mockRejectedValue(new Error("integrations are not available on this server"))
    render(<IntegrationsSettings baseUrl="" />)
    expect((await screen.findByRole("alert")).textContent).toContain("not available")
  })
})
