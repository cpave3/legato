import { describe, expect, it, vi, beforeEach, afterEach } from "vitest"
import { render, screen, fireEvent, waitFor, cleanup } from "@testing-library/react"
import { ImportRemoteModal } from "../ImportRemoteModal"

const mockFetch = vi.fn()

beforeEach(() => {
  vi.resetAllMocks()
})

afterEach(() => {
  cleanup()
})

vi.stubGlobal("fetch", mockFetch)

vi.mock("../../../hooks/useServer", () => ({
  useServer: () => ({ baseUrl: "" }),
}))

describe("ImportRemoteModal", () => {
  it("renders search input", () => {
    render(<ImportRemoteModal open={true} onClose={() => {}} onImported={() => {}} />)
    expect(screen.getByPlaceholderText("Type to search...")).toBeTruthy()
  })

  it("renders results after successful search", async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: async () => [
        { id: "TICK-1", summary: "Remote ticket one", status: "Open", priority: "High", issue_type: "Bug" },
      ],
    })

    render(<ImportRemoteModal open={true} onClose={() => {}} onImported={() => {}} />)
    const input = screen.getByPlaceholderText("Type to search...")
    fireEvent.change(input, { target: { value: "ticket" } })

    await waitFor(() => expect(screen.getByText(/Remote ticket one/)).toBeTruthy(), { timeout: 2000 })
  })

  it("prefills workspace and submits explicit selection", async () => {
    mockFetch
      .mockResolvedValueOnce({
        ok: true,
        json: async () => [{ id: "TICK-1", summary: "Remote ticket", status: "Open", priority: "High", issue_type: "Bug" }],
      })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ id: "task-1" }) })

    render(
      <ImportRemoteModal
        open
        workspaces={[{ id: 1, name: "Work", color: "#fff" }, { id: 2, name: "Personal", color: "#000" }]}
        defaultWorkspaceId={1}
        onClose={() => {}}
        onImported={() => {}}
      />
    )
    const workspace = screen.getByRole("combobox", { name: "Workspace" }) as HTMLSelectElement
    expect(workspace.value).toBe("1")
    fireEvent.change(workspace, { target: { value: "" } })
    fireEvent.change(screen.getByPlaceholderText("Type to search..."), { target: { value: "ticket" } })
    await waitFor(() => expect(screen.getByText("Remote ticket")).toBeTruthy(), { timeout: 2000 })
    fireEvent.click(screen.getByText("Remote ticket"))
    await waitFor(() => expect(mockFetch).toHaveBeenCalledTimes(2))
    const request = mockFetch.mock.calls[1][1] as RequestInit
    expect(JSON.parse(String(request.body))).toEqual({ ticket_id: "TICK-1", workspace_id: null })
  })

  it("names each result's provider and imports from that provider", async () => {
    mockFetch
      .mockResolvedValueOnce({
        ok: true,
        json: async () => [
          { id: "EST-42", summary: "Burnlite task", status: "Ready", priority: "", issue_type: "Work", provider: "burnlite" },
          { id: "TICK-1", summary: "Jira ticket", status: "Open", priority: "High", issue_type: "Bug", provider: "jira" },
        ],
      })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ id: "EST-42" }) })

    render(<ImportRemoteModal open onClose={() => {}} onImported={() => {}} />)
    fireEvent.change(screen.getByPlaceholderText("Type to search..."), { target: { value: "task" } })
    await waitFor(() => expect(screen.getByText("Burnlite task")).toBeTruthy(), { timeout: 2000 })
    const burnliteRow = screen.getByText("Burnlite task").closest("[data-remote-result]") as HTMLElement
    expect(burnliteRow.querySelector("[data-provider]")?.textContent).toBe("burnlite")
    const jiraRow = screen.getByText("Jira ticket").closest("[data-remote-result]") as HTMLElement
    expect(jiraRow.querySelector("[data-provider]")?.textContent).toBe("jira")

    fireEvent.click(screen.getByText("Burnlite task"))
    await waitFor(() => expect(mockFetch).toHaveBeenCalledTimes(2))
    const request = mockFetch.mock.calls[1][1] as RequestInit
    expect(JSON.parse(String(request.body))).toEqual({ ticket_id: "EST-42", provider: "burnlite", workspace_id: null })
  })

  it("shows the server's reason when no provider can search", async () => {
    mockFetch.mockResolvedValue({ ok: false, status: 503, json: async () => ({ error: "No ticket provider is connected" }), text: async () => "" })

    render(<ImportRemoteModal open onClose={() => {}} onImported={() => {}} />)
    fireEvent.change(screen.getByPlaceholderText("Type to search..."), { target: { value: "task" } })
    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("No ticket provider is connected"), { timeout: 2000 })
  })

  it("shows the server's reason when the key is already a task from another source", async () => {
    const onClose = vi.fn()
    mockFetch
      .mockResolvedValueOnce({
        ok: true,
        json: async () => [{ id: "EST-42", summary: "Burnlite task", status: "Ready", priority: "", issue_type: "Work", provider: "burnlite" }],
      })
      .mockResolvedValueOnce({ ok: false, status: 409, json: async () => ({ error: "EST-42 is already a task from jira" }) })

    render(<ImportRemoteModal open onClose={onClose} onImported={() => {}} />)
    fireEvent.change(screen.getByPlaceholderText("Type to search..."), { target: { value: "task" } })
    await waitFor(() => expect(screen.getByText("Burnlite task")).toBeTruthy(), { timeout: 2000 })
    fireEvent.click(screen.getByText("Burnlite task"))
    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("already a task from jira"))
    expect(onClose).not.toHaveBeenCalled()
  })

  it("surfaces an error message after a failed search", async () => {
    mockFetch.mockResolvedValue({ ok: false, status: 500 })

    render(<ImportRemoteModal open={true} onClose={() => {}} onImported={() => {}} />)
    const input = screen.getByPlaceholderText("Type to search...")
    fireEvent.change(input, { target: { value: "query" } })

    await waitFor(() => expect(screen.getByText(/Search failed/)).toBeTruthy(), { timeout: 2000 })
  })
})
