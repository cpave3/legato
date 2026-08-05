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

  it("surfaces an error message after a failed search", async () => {
    mockFetch.mockResolvedValue({ ok: false, status: 500 })

    render(<ImportRemoteModal open={true} onClose={() => {}} onImported={() => {}} />)
    const input = screen.getByPlaceholderText("Type to search...")
    fireEvent.change(input, { target: { value: "query" } })

    await waitFor(() => expect(screen.getByText(/Search failed/)).toBeTruthy(), { timeout: 2000 })
  })
})
