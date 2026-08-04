import { beforeEach, describe, expect, it, vi } from "vitest"
import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { AgentSelectionProvider } from "../../hooks/useAgentSelection"
import { BoardPage } from "../Board"

const mockFetch = vi.fn()

vi.stubGlobal("fetch", mockFetch)
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useSettings", () => ({ useSettings: () => ({ data: { repositories: [], working_dir: "", worktrees_enabled: false } }) }))
vi.mock("../../hooks/useBoard", () => ({
  useBoard: () => ({
    columns: [{ name: "Backlog", cards: [] }],
    workspaces: [],
    loading: false,
    error: null,
    refresh: vi.fn(),
  }),
}))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast: vi.fn() }) }))
vi.mock("../../hooks/useWorkflow", () => ({ useWorkflows: () => ({ data: null, loading: false, error: null }) }))

describe("Board search", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    mockFetch.mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes("/api/settings")) {
        return { ok: true, json: async () => ({ repositories: [] }) }
      }
      if (url.includes("/api/tasks/search")) {
        return {
          ok: true,
          json: async () => [{ id: "ARCHIVED-1", title: "Archived ticket", status: "Done" }],
        }
      }
      if (url.includes("/api/tasks/ARCHIVED-1")) {
        return {
          ok: true,
          json: async () => ({
            id: "ARCHIVED-1",
            title: "Archived ticket",
            description_md: "Completed work",
            status: "Done",
          }),
        }
      }
      throw new Error(`unexpected fetch: ${url}`)
    })
  })

  it("opens an archived search result with Enter", async () => {
    render(<MemoryRouter><AgentSelectionProvider><BoardPage /></AgentSelectionProvider></MemoryRouter>)

    fireEvent.keyDown(window, { key: "/" })
    const input = await screen.findByPlaceholderText("Search...")
    fireEvent.change(input, { target: { value: "archived" } })
    await screen.findByText("Archived ticket")

    fireEvent.keyDown(window, { key: "Enter" })

    await waitFor(() => expect(screen.getByText("Completed work")).toBeTruthy())
    expect(mockFetch).toHaveBeenCalledWith(expect.stringContaining("/api/tasks/ARCHIVED-1"), expect.anything())
  })
})
