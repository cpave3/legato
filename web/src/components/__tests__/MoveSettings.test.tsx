import { describe, it, expect, vi, afterEach, beforeEach } from "vitest"
import { render, screen, cleanup, fireEvent, waitFor } from "@testing-library/react"
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
    await waitFor(() => expect(mockMove).toHaveBeenCalledWith("http://legato", { settings: ["agents.reviews"], entries: [{ list: "macros", name: "hi" }] }))
    expect((await screen.findByRole("status")).textContent).toMatch(/Restarting/)
  })

  it("shows nothing when nothing is pinned", async () => {
    mockFetch.mockResolvedValue({ settings: [], entries: [] })
    const { container } = render(<QueryClientProvider client={new QueryClient()}><MoveSettings baseUrl="" /></QueryClientProvider>)
    await waitFor(() => expect(mockFetch).toHaveBeenCalled())
    expect(container.textContent).toBe("")
  })
})
