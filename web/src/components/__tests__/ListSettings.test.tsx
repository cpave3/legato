import { describe, it, expect, vi, afterEach, beforeEach } from "vitest"
import { render, screen, cleanup, fireEvent, waitFor, within } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"

const mockFetch = vi.fn()
const mockSave = vi.fn()
const mockRemove = vi.fn()
vi.mock("../../lib/configSettings", async (importActual) => ({
  ...(await importActual<typeof import("../../lib/configSettings")>()),
  fetchConfigLists: (...a: unknown[]) => mockFetch(...a),
  saveConfigListEntry: (...a: unknown[]) => mockSave(...a),
  removeConfigListEntry: (...a: unknown[]) => mockRemove(...a),
}))

import { ListSettings } from "../settings/ListSettings"

const workspaces = {
  key: "workspaces", title: "Workspaces", class: "change", mode: "restart", name_field: "name",
  entries: [
    { name: "Work", value: { name: "Work", color: "#f00" }, source: "config.yaml", pinned: true },
    { name: "Home", value: { color: "#0f0" }, source: "database", pinned: false },
  ],
}

function show() {
  render(<QueryClientProvider client={new QueryClient()}><ListSettings baseUrl="http://legato" /></QueryClientProvider>)
}

describe("ListSettings", () => {
  beforeEach(() => {
    mockFetch.mockReset(); mockFetch.mockResolvedValue([workspaces])
    mockSave.mockReset(); mockSave.mockResolvedValue(undefined)
    mockRemove.mockReset(); mockRemove.mockResolvedValue(undefined)
  })
  afterEach(() => cleanup())

  it("locks the file's entries and lets the database's be edited and removed", async () => {
    show()
    const work = await screen.findByTestId("entry-workspaces-Work")
    expect(work.textContent).toContain("config.yaml")
    expect(within(work).queryByRole("button", { name: "Edit" })).toBeNull()

    const home = screen.getByTestId("entry-workspaces-Home")
    fireEvent.click(within(home).getByRole("button", { name: "Remove Home" }))
    await waitFor(() => expect(mockRemove).toHaveBeenCalledWith("http://legato", "workspaces", "Home"))
  })

  it("adds an entry", async () => {
    show()
    fireEvent.click(await screen.findByRole("button", { name: "Add to workspaces" }))
    fireEvent.change(screen.getByRole("textbox", { name: "Workspaces name" }), { target: { value: "Side" } })
    fireEvent.change(screen.getByRole("textbox", { name: "Workspaces fields" }), { target: { value: '{"color": "#00f"}' } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(mockSave).toHaveBeenCalledWith("http://legato", "workspaces", "Side", { color: "#00f" }))
  })

  it("says why an entry was refused", async () => {
    mockSave.mockRejectedValue(new Error("workspaces \"work\": config.yaml already has an entry of this name"))
    show()
    fireEvent.click(await screen.findByRole("button", { name: "Add to workspaces" }))
    fireEvent.change(screen.getByRole("textbox", { name: "Workspaces name" }), { target: { value: "work" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect((await screen.findByRole("alert")).textContent).toContain("already has")
  })
})
