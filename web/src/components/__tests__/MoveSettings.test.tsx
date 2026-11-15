import { describe, it, expect, vi, afterEach, beforeEach } from "vitest"
import { render, screen, cleanup, fireEvent, waitFor, within, act } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"

const mockFetch = vi.fn()
const mockMove = vi.fn()
vi.mock("../../lib/configSettings", async (importActual) => ({
  ...(await importActual<typeof import("../../lib/configSettings")>()),
  fetchMovable: (...a: unknown[]) => mockFetch(...a),
  moveSettings: (...a: unknown[]) => mockMove(...a),
}))

import { MoveSettings } from "../settings/MoveSettings"

describe("MoveSettings", () => {
  beforeEach(() => {
    mockFetch.mockReset(); mockFetch.mockResolvedValue({ settings: ["agents.reviews", "icons"], entries: [{ list: "macros", name: "hi" }] })
    mockMove.mockReset(); mockMove.mockResolvedValue(undefined)
  })
  afterEach(() => cleanup())

  it("moves every pinned setting but those unticked", async () => {
    render(<QueryClientProvider client={new QueryClient()}><MoveSettings baseUrl="http://legato" /></QueryClientProvider>)
    fireEvent.click(await screen.findByRole("checkbox", { name: "icons" }))
    fireEvent.click(screen.getByRole("button", { name: "Move 2 to the database and restart" }))
    expect(mockMove).not.toHaveBeenCalled()
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect((screen.getByRole("button", { name: "Move 2 to the database and restart" }) as HTMLButtonElement).disabled).toBe(false))
    fireEvent.click(screen.getByRole("button", { name: "Move 2 to the database and restart" }))
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Move and restart" }))
    await waitFor(() => expect(mockMove).toHaveBeenCalledWith("http://legato", { settings: ["agents.reviews"], entries: [{ list: "macros", name: "hi" }] }))
    expect((await screen.findByRole("status")).textContent).toMatch(/Restarting/)
  })

  it("keeps unticked choices across refreshed eligibility and counts only current items", async () => {
    const client = new QueryClient()
    render(<QueryClientProvider client={client}><MoveSettings baseUrl="" /></QueryClientProvider>)
    fireEvent.click(await screen.findByRole("checkbox", { name: "icons" }))
    await act(async () => { client.setQueryData(["", "config-move"], { settings: ["icons", "theme"], entries: [] }) })
    await screen.findByRole("checkbox", { name: "theme" })
    expect((screen.getByRole("checkbox", { name: "icons" }) as HTMLInputElement).checked).toBe(false)
    await waitFor(() => expect(screen.getByRole("button", { name: "Move 1 to the database and restart" })).toBeTruthy())
    await act(async () => { client.setQueryData(["", "config-move"], { settings: ["theme"], entries: [] }) })
    await waitFor(() => expect(screen.getByRole("button", { name: "Move 1 to the database and restart" })).toBeTruthy())
  })

  it("shows eligibility loading and retries a failed check", async () => {
    mockFetch.mockRejectedValueOnce(new Error("Eligibility unavailable"))
    render(<QueryClientProvider client={new QueryClient()}><MoveSettings baseUrl="" /></QueryClientProvider>)
    expect(screen.getByRole("status").textContent).toContain("Loading")
    expect((await screen.findByRole("alert")).textContent).toContain("Eligibility unavailable")
    fireEvent.click(screen.getByRole("button", { name: "Retry" }))
    expect(await screen.findByRole("checkbox", { name: "icons" })).toBeTruthy()
    expect(mockMove).not.toHaveBeenCalled()
  })

  it("locks the chosen payload while moving and allows retry after a refusal", async () => {
    let refuse!: (error: Error) => void
    mockMove.mockImplementationOnce(() => new Promise((_, reject) => { refuse = reject }))
    render(<QueryClientProvider client={new QueryClient()}><MoveSettings baseUrl="" /></QueryClientProvider>)
    fireEvent.click(await screen.findByRole("button", { name: "Move 3 to the database and restart" }))
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Move and restart" }))
    await waitFor(() => expect(screen.getByRole("status").textContent).toBe("Moving…"))
    expect((screen.getByRole("checkbox", { name: "icons" }) as HTMLInputElement).disabled).toBe(true)
    expect(mockMove).toHaveBeenCalledTimes(1)
    await act(async () => { refuse(new Error("A restart is already pending")) })
    expect(screen.getByRole("alert").textContent).toContain("A restart is already pending")
    expect((screen.getByRole("checkbox", { name: "icons" }) as HTMLInputElement).disabled).toBe(false)
    fireEvent.click(screen.getByRole("button", { name: "Move 3 to the database and restart" }))
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Move and restart" }))
    await waitFor(() => expect(screen.getByRole("status").textContent).toBe("Restarting…"))
    expect(mockMove).toHaveBeenCalledTimes(2)
  })

  it("keeps confirmation dismissible if a refresh removes all eligible items", async () => {
    const client = new QueryClient()
    render(<QueryClientProvider client={client}><MoveSettings baseUrl="" /></QueryClientProvider>)
    fireEvent.click(await screen.findByRole("button", { name: "Move 3 to the database and restart" }))
    await act(async () => { client.setQueryData(["", "config-move"], { settings: [], entries: [] }) })
    await waitFor(() => expect(screen.queryByRole("checkbox")).toBeNull())
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    expect(mockMove).not.toHaveBeenCalled()
  })

  it("retains stale eligibility after a failed refresh but prevents a move until retry", async () => {
    const client = new QueryClient()
    render(<QueryClientProvider client={client}><MoveSettings baseUrl="" /></QueryClientProvider>)
    await screen.findByRole("checkbox", { name: "icons" })
    mockFetch.mockRejectedValueOnce(new Error("Offline"))
    await act(async () => { await client.invalidateQueries({ queryKey: ["", "config-move"] }) })
    expect((await screen.findByRole("alert")).textContent).toContain("Offline")
    expect(screen.getByRole("checkbox", { name: "icons" })).toBeTruthy()
    expect((screen.getByRole("button", { name: "Move 3 to the database and restart" }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.click(screen.getByRole("button", { name: "Retry" }))
    await waitFor(() => expect((screen.getByRole("button", { name: "Move 3 to the database and restart" }) as HTMLButtonElement).disabled).toBe(false))
  })

  it("keeps the migration destination visible when nothing can move", async () => {
    mockFetch.mockResolvedValue({ settings: [], entries: [] })
    render(<QueryClientProvider client={new QueryClient()}><MoveSettings baseUrl="" /></QueryClientProvider>)
    await waitFor(() => expect(mockFetch).toHaveBeenCalled())
    expect(await screen.findByText("No settings can move from config.yaml.")).toBeTruthy()
  })
})
