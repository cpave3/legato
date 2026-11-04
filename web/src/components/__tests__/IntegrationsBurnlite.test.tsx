import { describe, it, expect, vi, afterEach, beforeEach } from "vitest"
import { render, screen, waitFor, cleanup, within, fireEvent } from "@testing-library/react"

const mockFetch = vi.fn()
vi.stubGlobal("fetch", mockFetch)

const leaveFor = vi.fn()
vi.mock("../../lib/integrations", async (original) => ({
  ...(await original<typeof import("../../lib/integrations")>()),
  leaveFor: (url: string) => leaveFor(url),
}))

import { IntegrationsSettings } from "../IntegrationsSettings"

const argus = { configured: false, connection: { state: "none", replies: {} }, open_cases: 0 }

function overview(burnlite: object) {
  return { tokens: [], argus, burnlite }
}

const notConnected = { configured_url: "https://burnlite.example", projects: [], projects_from_config: false, connection: { state: "none" } }
const connected = {
  configured_url: "https://burnlite.example",
  projects: ["EST", "OPS"],
  projects_from_config: false,
  connection: { state: "connected", url: "https://burnlite.example", connected_at: "2026-11-01T03:10:00Z" },
}

function json(status: number, body: unknown) {
  return { ok: status >= 200 && status < 300, status, statusText: "", json: async () => body, text: async () => JSON.stringify(body) }
}

// Routes the component's requests: the overview, and whatever each test adds.
function serve(routes: Record<string, (init?: RequestInit) => unknown>) {
  mockFetch.mockImplementation(async (url: string, init?: RequestInit) => {
    const key = `${init?.method ?? "GET"} ${url}`
    const route = routes[key]
    if (!route) throw new Error(`unexpected request ${key}`)
    return route(init)
  })
}

async function burnliteCard() {
  return screen.findByRole("region", { name: "Burnlite" })
}

describe("IntegrationsSettings › Burnlite", () => {
  beforeEach(() => {
    mockFetch.mockReset()
    leaveFor.mockReset()
    window.history.replaceState(null, "", "/settings")
  })
  afterEach(() => cleanup())

  it("connects: posts the address and goes to Burnlite to consent", async () => {
    let posted: unknown
    serve({
      "GET /api/settings/integrations": () => json(200, overview(notConnected)),
      "POST /api/settings/integrations/burnlite/connect": (init) => {
        posted = JSON.parse(String(init?.body))
        return json(200, { authorize_url: "https://burnlite.example/oauth/authorize?x=1" })
      },
    })
    render(<IntegrationsSettings baseUrl="" />)
    const card = await burnliteCard()
    const address = within(card).getByRole("textbox", { name: "Burnlite address" }) as HTMLInputElement
    expect(address.value).toBe("https://burnlite.example")
    fireEvent.change(address, { target: { value: "https://tasks.example" } })
    fireEvent.click(within(card).getByRole("button", { name: "Connect" }))
    await waitFor(() => expect(leaveFor).toHaveBeenCalledWith("https://burnlite.example/oauth/authorize?x=1"))
    expect(posted).toEqual({ url: "https://tasks.example" })
  })

  it("shows the server's reason when it cannot connect", async () => {
    serve({
      "GET /api/settings/integrations": () => json(200, overview(notConnected)),
      "POST /api/settings/integrations/burnlite/connect": () => json(409, { error: "Unlock Legato's secret store first" }),
    })
    render(<IntegrationsSettings baseUrl="" />)
    const card = await burnliteCard()
    fireEvent.click(within(card).getByRole("button", { name: "Connect" }))
    expect((await within(card).findByRole("alert")).textContent).toContain("Unlock Legato's secret store first")
    expect(leaveFor).not.toHaveBeenCalled()
  })

  it("says Burnlite refused Legato and offers to connect again at the same address", async () => {
    serve({
      "GET /api/settings/integrations": () => json(200, overview({
        projects: [], projects_from_config: false,
        connection: { state: "disconnected", url: "https://old.example" },
      })),
    })
    render(<IntegrationsSettings baseUrl="" />)
    const card = await burnliteCard()
    expect(within(card).getByText(/refused Legato/)).toBeTruthy()
    expect((within(card).getByRole("textbox", { name: "Burnlite address" }) as HTMLInputElement).value).toBe("https://old.example")
    expect(within(card).getByRole("button", { name: "Connect" })).toBeTruthy()
  })

  it("shows a connection: its address, since when, and the projects searched", async () => {
    serve({ "GET /api/settings/integrations": () => json(200, overview(connected)) })
    render(<IntegrationsSettings baseUrl="" />)
    const card = await burnliteCard()
    expect(within(card).getByRole("link", { name: "https://burnlite.example" }).getAttribute("href")).toBe("https://burnlite.example")
    expect(within(card).getByText(/Connected since/)).toBeTruthy()
    expect((within(card).getByRole("textbox", { name: "Projects" }) as HTMLInputElement).value).toBe("EST, OPS")
    expect(within(card).queryByRole("button", { name: "Connect" })).toBeNull()
  })

  it("saves the projects as keys and shows what the server kept", async () => {
    let put: unknown
    serve({
      "GET /api/settings/integrations": () => json(200, overview(connected)),
      "PUT /api/settings/integrations/burnlite": (init) => {
        put = JSON.parse(String(init?.body))
        return json(200, { ...connected, projects: ["EST", "WEB2"] })
      },
    })
    render(<IntegrationsSettings baseUrl="" />)
    const card = await burnliteCard()
    const projects = within(card).getByRole("textbox", { name: "Projects" })
    fireEvent.change(projects, { target: { value: "est,  web2 ," } })
    fireEvent.click(within(card).getByRole("button", { name: "Save" }))
    await waitFor(() => expect(put).toEqual({ projects: ["EST", "WEB2"] }))
    await waitFor(() => expect((within(card).getByRole("textbox", { name: "Projects" }) as HTMLInputElement).value).toBe("EST, WEB2"))
  })

  it("saves no projects as every project, and shows a key the server refused", async () => {
    const bodies: unknown[] = []
    serve({
      "GET /api/settings/integrations": () => json(200, overview(connected)),
      "PUT /api/settings/integrations/burnlite": (init) => {
        bodies.push(JSON.parse(String(init?.body)))
        return bodies.length === 1 ? json(400, { error: "\"E\" is not a Burnlite project key" }) : json(200, { ...connected, projects: [] })
      },
    })
    render(<IntegrationsSettings baseUrl="" />)
    const card = await burnliteCard()
    const projects = within(card).getByRole("textbox", { name: "Projects" })
    fireEvent.change(projects, { target: { value: "E" } })
    fireEvent.click(within(card).getByRole("button", { name: "Save" }))
    expect((await within(card).findByRole("alert")).textContent).toContain("not a Burnlite project key")

    fireEvent.change(projects, { target: { value: "" } })
    fireEvent.click(within(card).getByRole("button", { name: "Save" }))
    await waitFor(() => expect(bodies[1]).toEqual({ projects: [] }))
    expect(within(card).getByText(/every project/i)).toBeTruthy()
  })

  it("keeps projects set in config.yaml out of reach", async () => {
    serve({ "GET /api/settings/integrations": () => json(200, overview({ ...connected, projects_from_config: true })) })
    render(<IntegrationsSettings baseUrl="" />)
    const card = await burnliteCard()
    expect((within(card).getByRole("textbox", { name: "Projects" }) as HTMLInputElement).disabled).toBe(true)
    expect(within(card).queryByRole("button", { name: "Save" })).toBeNull()
    expect(within(card).getByText(/set in config\.yaml/i)).toBeTruthy()
  })

  it("disconnects only once confirmed", async () => {
    let state: object = connected
    const deleted = vi.fn()
    serve({
      "GET /api/settings/integrations": () => json(200, overview(state)),
      "DELETE /api/settings/integrations/burnlite": () => {
        deleted()
        state = notConnected
        return json(200, { disconnected: "burnlite" })
      },
    })
    render(<IntegrationsSettings baseUrl="" />)
    const card = await burnliteCard()
    fireEvent.click(within(card).getByRole("button", { name: "Disconnect" }))
    expect(deleted).not.toHaveBeenCalled()
    fireEvent.click(within(card).getByRole("button", { name: "Keep" }))
    expect(within(card).queryByRole("button", { name: "Keep" })).toBeNull()

    fireEvent.click(within(card).getByRole("button", { name: "Disconnect" }))
    fireEvent.click(within(card).getByRole("button", { name: "Disconnect from Burnlite" }))
    await waitFor(() => expect(deleted).toHaveBeenCalledTimes(1))
    expect(await within(card).findByRole("button", { name: "Connect" })).toBeTruthy()
  })

  it("says once that the connection was made, and drops it from the address", async () => {
    window.history.replaceState(null, "", "/settings?integration=burnlite&result=connected#integrations")
    serve({ "GET /api/settings/integrations": () => json(200, overview(connected)) })
    render(<IntegrationsSettings baseUrl="" />)
    const card = await burnliteCard()
    expect(within(card).getByRole("status").textContent).toMatch(/Connected to Burnlite/)
    expect(window.location.search).toBe("")
    expect(window.location.hash).toBe("#integrations")
  })

  it("says why connecting failed, once", async () => {
    window.history.replaceState(null, "", `/settings?integration=burnlite&result=failed&error=${encodeURIComponent("access denied")}#integrations`)
    serve({ "GET /api/settings/integrations": () => json(200, overview(notConnected)) })
    render(<IntegrationsSettings baseUrl="" />)
    const card = await burnliteCard()
    expect(within(card).getByRole("alert").textContent).toContain("access denied")
    expect(window.location.search).toBe("")
  })

  it("leaves the Burnlite block out for a server that does not have it", async () => {
    serve({ "GET /api/settings/integrations": () => json(200, { tokens: [], argus }) })
    render(<IntegrationsSettings baseUrl="" />)
    await screen.findByRole("region", { name: "Argus" })
    expect(screen.queryByRole("region", { name: "Burnlite" })).toBeNull()
  })
})
