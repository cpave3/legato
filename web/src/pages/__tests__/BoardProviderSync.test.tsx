import { beforeEach, describe, expect, it, vi } from "vitest"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { AgentSelectionProvider } from "../../hooks/useAgentSelection"
import { BoardPage } from "../Board"

const mockFetch = vi.fn()
const refresh = vi.fn(async () => {})
const addToast = vi.fn()

vi.stubGlobal("fetch", mockFetch)
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useSettings", () => ({ useSettings: () => ({ data: { repositories: [], working_dir: "", worktrees_enabled: false } }) }))
vi.mock("../../hooks/useBoard", () => ({
  useBoard: () => ({ columns: [{ name: "Backlog", cards: [] }], workspaces: [], loading: false, error: null, refresh }),
}))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast }) }))
vi.mock("../../hooks/useWorkflow", () => ({ useWorkflows: () => ({ data: null, loading: false, error: null }) }))

describe("Board provider sync", () => {
  beforeEach(() => {
    cleanup()
    vi.resetAllMocks()
  })

  it("posts provider sync, disables while pending, and refetches before success feedback", async () => {
    let resolveSync!: (value: unknown) => void
    mockFetch.mockImplementation(() => new Promise(resolve => { resolveSync = resolve }))
    render(<MemoryRouter><AgentSelectionProvider><BoardPage /></AgentSelectionProvider></MemoryRouter>)

    const button = screen.getByRole("button", { name: "Sync providers" })
    fireEvent.click(button)
    expect((button as HTMLButtonElement).disabled).toBe(true)
    expect(mockFetch).toHaveBeenCalledWith("/api/board/sync", expect.objectContaining({ method: "POST" }))

    resolveSync({ ok: true, status: 200, json: async () => ({ jira: { status: "completed" }, github: { status: "completed" } }) })
    await waitFor(() => expect(refresh).toHaveBeenCalledTimes(1))
    expect(addToast).toHaveBeenCalledWith("Jira + GitHub sync completed", "success")
    expect((button as HTMLButtonElement).disabled).toBe(false)
  })

  it("surfaces partial failures after refetching successful provider work", async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ jira: { status: "completed" }, github: { status: "failed", error: "rate limited" } }),
    })
    render(<MemoryRouter><AgentSelectionProvider><BoardPage /></AgentSelectionProvider></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: "Sync providers" }))

    await waitFor(() => expect(addToast).toHaveBeenCalledWith(expect.stringContaining("GitHub: rate limited"), "error"))
    expect(refresh).toHaveBeenCalledTimes(1)
  })
})
