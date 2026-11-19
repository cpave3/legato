import { describe, it, expect, vi, afterEach, beforeEach } from "vitest"
import { render, screen, cleanup, fireEvent, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { MemoryRouter } from "react-router-dom"

const mockFetch = vi.fn()
const mockDismiss = vi.fn()
vi.mock("../../lib/setup", async (importActual) => ({
  ...(await importActual<typeof import("../../lib/setup")>()),
  fetchSetup: (...a: unknown[]) => mockFetch(...a),
  setSetupDismissed: (...a: unknown[]) => mockDismiss(...a),
}))

import { SetupChip } from "../SetupChip"
import type { SetupStatus, SetupSummary } from "../../lib/setup"

const checklist: SetupStatus = {
  done: 2, total: 4, dismissed: false, show: true,
  items: [
    { id: "checks", title: "This machine has what Legato needs", done: true, settings: "server" },
    { id: "default_tool", title: "Choose the default agent tool", done: true, settings: "agents" },
    { id: "repository", title: "Add a repository", done: false, settings: "repositories" },
    { id: "device", title: "Pair a phone or another device", done: false, settings: "devices" },
    { id: "tracker", title: "Connect a ticket tracker", done: false, optional: true, settings: "integrations" },
  ],
}

function renderChip(summary: SetupSummary | undefined) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}><MemoryRouter><SetupChip baseUrl="http://legato" summary={summary} /></MemoryRouter></QueryClientProvider>)
}

describe("SetupChip", () => {
  beforeEach(() => {
    mockFetch.mockReset().mockResolvedValue(checklist)
    mockDismiss.mockReset().mockResolvedValue({ ...checklist, dismissed: true, show: false })
  })
  afterEach(() => cleanup())

  it("counts what is done", () => {
    renderChip({ show: true, done: 2, total: 4 })
    expect(screen.getByRole("button", { name: /Setup 2\/4/ })).toBeTruthy()
  })

  it("hides when the server says so: all done, or dismissed with every check passing", () => {
    renderChip({ show: false, done: 4, total: 4 })
    expect(screen.queryByRole("button", { name: /Setup/ })).toBeNull()
  })

  it("comes back when the server shows it again, as for a failed required check", () => {
    const { rerender } = renderChip({ show: false, done: 3, total: 4 })
    expect(screen.queryByRole("button", { name: /Setup/ })).toBeNull()
    const client = new QueryClient()
    rerender(<QueryClientProvider client={client}><MemoryRouter><SetupChip baseUrl="http://legato" summary={{ show: true, done: 2, total: 4 }} /></MemoryRouter></QueryClientProvider>)
    expect(screen.getByRole("button", { name: /Setup 2\/4/ })).toBeTruthy()
  })

  it("opens the checklist with a way to each item, and dismisses it for every device", async () => {
    renderChip({ show: true, done: 2, total: 4 })
    fireEvent.click(screen.getByRole("button", { name: /Setup 2\/4/ }))

    await screen.findByText("Add a repository")
    const dialog = screen.getByRole("dialog")
    expect(dialog.textContent).toMatch(/optional/)
    expect(screen.getByRole("link", { name: /Add a repository/ }).getAttribute("href")).toBe("/settings#repositories")

    fireEvent.click(screen.getByRole("button", { name: "Hide the checklist" }))
    await waitFor(() => expect(mockDismiss).toHaveBeenCalledWith("http://legato", true))
  })
})
