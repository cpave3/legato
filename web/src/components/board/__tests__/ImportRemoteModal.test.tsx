import { describe, expect, it, vi, beforeEach, afterEach } from "vitest"
import { render, screen, fireEvent, waitFor, cleanup } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router-dom"
import type { ComponentProps } from "react"
import { ImportRemoteModal } from "../ImportRemoteModal"

const mockFetch = vi.fn()

vi.stubGlobal("fetch", mockFetch)

vi.mock("../../../hooks/useServer", () => ({
  useServer: () => ({ baseUrl: "" }),
}))

type Reply = { ok: boolean; status?: number; body?: unknown }

// serve answers each endpoint the dialog calls; the providers list defaults
// to one connected provider.
function serve({ providers = [{ name: "jira", label: "Jira" }], search, imported }: {
  providers?: { name: string; label: string }[] | null
  search?: Reply
  imported?: Reply
}) {
  mockFetch.mockImplementation(async (url: string) => {
    let reply: Reply | undefined
    if (url.startsWith("/api/ticket-providers")) reply = providers === null ? { ok: false, status: 404, body: {} } : { ok: true, body: { providers } }
    else if (url.startsWith("/api/remote/search")) reply = search
    else if (url.startsWith("/api/remote/import")) reply = imported
    if (!reply) throw new Error(`unexpected fetch ${url}`)
    const { ok, status = ok ? 200 : 500, body } = reply
    return { ok, status, json: async () => body, text: async () => JSON.stringify(body ?? "") }
  })
}

function importBodies() {
  return mockFetch.mock.calls
    .filter(([url]) => String(url).startsWith("/api/remote/import"))
    .map(([, init]) => JSON.parse(String((init as RequestInit).body)))
}

function renderModal(props: Partial<ComponentProps<typeof ImportRemoteModal>> = {}) {
  return render(
    <MemoryRouter>
      <ImportRemoteModal open onClose={() => {}} onImported={() => {}} {...props} />
    </MemoryRouter>,
  )
}

const twoResults: Reply = {
  ok: true,
  body: [
    { id: "TICK-1", summary: "First ticket", status: "Open", priority: "High", issue_type: "Bug", provider: "jira" },
    { id: "TICK-2", summary: "Second ticket", status: "Open", priority: "", issue_type: "Task", provider: "jira" },
  ],
}

beforeEach(() => {
  vi.resetAllMocks()
})

afterEach(() => {
  cleanup()
})

describe("ImportRemoteModal", () => {
  it("renders search input", () => {
    serve({})
    renderModal()
    expect(screen.getByPlaceholderText("Type to search...")).toBeTruthy()
  })

  it("lets j and k be typed into the search", async () => {
    serve({ search: { ok: true, body: [] } })
    renderModal()
    const input = screen.getByPlaceholderText("Type to search...") as HTMLInputElement
    await userEvent.type(input, "jk task")
    expect(input.value).toBe("jk task")
  })

  it("moves with Up and Down, imports the highlighted result with Enter, and closes with Escape", async () => {
    const onClose = vi.fn()
    serve({ search: twoResults, imported: { ok: true, status: 201, body: { id: "TICK-2", status: "Backlog" } } })
    renderModal({ onClose })
    const input = screen.getByPlaceholderText("Type to search...")
    await userEvent.type(input, "ticket")
    await screen.findByText("Second ticket", {}, { timeout: 2000 })

    await userEvent.keyboard("{ArrowDown}{ArrowDown}{ArrowUp}{ArrowDown}{Enter}")
    await waitFor(() => expect(importBodies()).toHaveLength(1))
    expect(importBodies()[0]).toMatchObject({ ticket_id: "TICK-2", provider: "jira" })

    await waitFor(() => expect(onClose).toHaveBeenCalled())
    onClose.mockClear()
    await userEvent.keyboard("{Escape}")
    expect(onClose).toHaveBeenCalled()
  })

  it("renders results after successful search", async () => {
    serve({ search: { ok: true, body: [{ id: "TICK-1", summary: "Remote ticket one", status: "Open", priority: "High", issue_type: "Bug" }] } })
    renderModal()
    fireEvent.change(screen.getByPlaceholderText("Type to search..."), { target: { value: "ticket" } })
    await waitFor(() => expect(screen.getByText(/Remote ticket one/)).toBeTruthy(), { timeout: 2000 })
  })

  it("offers the provider's default column and the board's columns, and sends only a picked one", async () => {
    serve({ search: twoResults, imported: { ok: true, status: 201, body: { id: "TICK-1", status: "Ready" } } })
    renderModal({ columns: ["Backlog", "Ready", "Done"] })
    const column = screen.getByRole("combobox", { name: "Column" }) as HTMLSelectElement
    expect(Array.from(column.options).map((o) => o.text)).toEqual(["Provider default", "Backlog", "Ready", "Done"])
    expect(column.value).toBe("")

    fireEvent.change(screen.getByPlaceholderText("Type to search..."), { target: { value: "ticket" } })
    fireEvent.click(await screen.findByText("First ticket", {}, { timeout: 2000 }))
    await waitFor(() => expect(importBodies()).toHaveLength(1))
    expect(importBodies()[0]).not.toHaveProperty("column")

    cleanup()
    mockFetch.mockClear()
    renderModal({ columns: ["Backlog", "Ready", "Done"] })
    fireEvent.change(screen.getByRole("combobox", { name: "Column" }), { target: { value: "Ready" } })
    fireEvent.change(screen.getByPlaceholderText("Type to search..."), { target: { value: "ticket" } })
    fireEvent.click(await screen.findByText("First ticket", {}, { timeout: 2000 }))
    await waitFor(() => expect(importBodies()).toHaveLength(1))
    expect(importBodies()[0]).toEqual({ ticket_id: "TICK-1", provider: "jira", workspace_id: null, column: "Ready" })
  })

  it("starts the workspace from the board's view", () => {
    serve({})
    const workspaces = [{ id: 1, name: "Work", color: "#fff" }, { id: 2, name: "Personal", color: "#000" }]
    const value = () => (screen.getByRole("combobox", { name: "Workspace" }) as HTMLSelectElement).value

    renderModal({ workspaces, view: "2", defaultWorkspaceId: 1 })
    expect(value()).toBe("2")
    cleanup()
    renderModal({ workspaces, view: "unassigned", defaultWorkspaceId: 1 })
    expect(value()).toBe("")
    cleanup()
    renderModal({ workspaces, view: "all", defaultWorkspaceId: 1 })
    expect(value()).toBe("1")
  })

  it("prefills workspace and submits explicit selection", async () => {
    serve({ search: { ok: true, body: [{ id: "TICK-1", summary: "Remote ticket", status: "Open", priority: "High", issue_type: "Bug" }] }, imported: { ok: true, body: { id: "task-1" } } })
    renderModal({ workspaces: [{ id: 1, name: "Work", color: "#fff" }, { id: 2, name: "Personal", color: "#000" }], defaultWorkspaceId: 1 })
    const workspace = screen.getByRole("combobox", { name: "Workspace" }) as HTMLSelectElement
    expect(workspace.value).toBe("1")
    fireEvent.change(workspace, { target: { value: "" } })
    fireEvent.change(screen.getByPlaceholderText("Type to search..."), { target: { value: "ticket" } })
    await waitFor(() => expect(screen.getByText("Remote ticket")).toBeTruthy(), { timeout: 2000 })
    fireEvent.click(screen.getByText("Remote ticket"))
    await waitFor(() => expect(importBodies()).toHaveLength(1))
    expect(importBodies()[0]).toEqual({ ticket_id: "TICK-1", workspace_id: null })
  })

  it("names each result's provider and imports from that provider", async () => {
    serve({
      providers: [{ name: "jira", label: "Jira" }, { name: "burnlite", label: "Burnlite" }],
      search: {
        ok: true,
        body: [
          { id: "EST-42", summary: "Burnlite task", status: "Ready", priority: "", issue_type: "Work", provider: "burnlite" },
          { id: "TICK-1", summary: "Jira ticket", status: "Open", priority: "High", issue_type: "Bug", provider: "jira" },
        ],
      },
      imported: { ok: true, body: { id: "EST-42" } },
    })
    renderModal()
    fireEvent.change(screen.getByPlaceholderText("Type to search..."), { target: { value: "task" } })
    await waitFor(() => expect(screen.getByText("Burnlite task")).toBeTruthy(), { timeout: 2000 })
    const burnliteRow = screen.getByText("Burnlite task").closest("[data-remote-result]") as HTMLElement
    expect(burnliteRow.querySelector("[data-provider]")?.textContent).toBe("burnlite")
    const jiraRow = screen.getByText("Jira ticket").closest("[data-remote-result]") as HTMLElement
    expect(jiraRow.querySelector("[data-provider]")?.textContent).toBe("jira")

    fireEvent.click(screen.getByText("Burnlite task"))
    await waitFor(() => expect(importBodies()).toHaveLength(1))
    expect(importBodies()[0]).toEqual({ ticket_id: "EST-42", provider: "burnlite", workspace_id: null })
  })

  it("tells the person to connect a provider in Settings when none is connected", async () => {
    serve({ providers: [] })
    renderModal()
    const link = await screen.findByRole("link", { name: /Settings/ })
    expect(link.getAttribute("href")).toBe("/settings")
    expect(screen.queryByPlaceholderText("Type to search...")).toBeNull()
    expect(mockFetch.mock.calls.some(([url]) => String(url).startsWith("/api/remote/search"))).toBe(false)
  })

  it("still searches when the server cannot list its providers", async () => {
    serve({ providers: null, search: { ok: true, body: [{ id: "TICK-1", summary: "Old server ticket", status: "Open", priority: "", issue_type: "" }] } })
    renderModal()
    fireEvent.change(screen.getByPlaceholderText("Type to search..."), { target: { value: "ticket" } })
    await screen.findByText("Old server ticket", {}, { timeout: 2000 })
  })

  it("hands the imported task's column and workspace to onImported", async () => {
    const onImported = vi.fn()
    serve({ search: twoResults, imported: { ok: true, status: 201, body: { id: "TICK-1", status: "Ready", workspace_id: 1, workspace_name: "Legato" } } })
    renderModal({ onImported })
    fireEvent.change(screen.getByPlaceholderText("Type to search..."), { target: { value: "ticket" } })
    fireEvent.click(await screen.findByText("First ticket", {}, { timeout: 2000 }))
    await waitFor(() => expect(onImported).toHaveBeenCalledWith(expect.objectContaining({ id: "TICK-1", status: "Ready", workspace_name: "Legato" })))
  })

  it("shows the server's reason when no provider can search", async () => {
    serve({ search: { ok: false, status: 503, body: { error: "No ticket provider is connected" } } })
    renderModal()
    fireEvent.change(screen.getByPlaceholderText("Type to search..."), { target: { value: "task" } })
    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("No ticket provider is connected"), { timeout: 2000 })
  })

  it("shows the server's reason when the key is already a task from another source", async () => {
    const onClose = vi.fn()
    serve({
      search: { ok: true, body: [{ id: "EST-42", summary: "Burnlite task", status: "Ready", priority: "", issue_type: "Work", provider: "burnlite" }] },
      imported: { ok: false, status: 409, body: { error: "EST-42 is already a task from jira" } },
    })
    renderModal({ onClose })
    fireEvent.change(screen.getByPlaceholderText("Type to search..."), { target: { value: "task" } })
    await waitFor(() => expect(screen.getByText("Burnlite task")).toBeTruthy(), { timeout: 2000 })
    fireEvent.click(screen.getByText("Burnlite task"))
    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("already a task from jira"))
    expect(onClose).not.toHaveBeenCalled()
  })

  it("surfaces an error message after a failed search", async () => {
    serve({ search: { ok: false, status: 500 } })
    renderModal()
    fireEvent.change(screen.getByPlaceholderText("Type to search..."), { target: { value: "query" } })
    await waitFor(() => expect(screen.getByText(/Search failed|remoteSearch failed/)).toBeTruthy(), { timeout: 2000 })
  })
})
