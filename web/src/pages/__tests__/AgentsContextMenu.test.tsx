import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { AgentsPage } from "../Agents"
// The agents being started have their own tests, and a query client.
vi.mock("../../components/PreparingAgents", () => ({ PreparingAgents: () => null }))


const { mockFetch, setSelectedAgentId, addToast } = vi.hoisted(() => ({
  mockFetch: vi.fn(),
  setSelectedAgentId: vi.fn(),
  addToast: vi.fn(),
}))

vi.stubGlobal("fetch", mockFetch)
vi.mock("../../hooks/useWebSocket", () => ({
  useWebSocket: () => ({ send: vi.fn(), subscribe: vi.fn(() => vi.fn()), connected: true }),
}))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useAgentSelection", () => ({
  useAgentSelection: () => ({ selectedAgentId: "TASK-1", setSelectedAgentId }),
}))
vi.mock("../../hooks/useSettings", () => ({
  useSettings: () => ({ data: { ntfy_configured: false, voice_enabled: false } }),
}))
vi.mock("../../hooks/useSwarmEvents", () => ({ useSwarmEvents: () => ({ fetchState: vi.fn() }) }))
vi.mock("../../hooks/useTaskActivity", () => ({ useTaskActivity: () => ({ items: [] }) }))
vi.mock("../../hooks/useAgentUI", () => ({
  useAgentUIItems: () => ({ items: [], loading: false, error: null }),
  useAgentUIItem: () => ({ item: null, loading: false, error: null }),
}))
vi.mock("../../hooks/useReviewBriefing", () => ({
  useReviewBriefing: () => ({ pages: [] }),
  useReviewBriefingPage: () => ({ page: null, loading: false, error: null }),
  useUnseenBriefingPages: () => 0,
}))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast }) }))
vi.mock("../../components/TerminalPanel", () => ({ TerminalPanel: () => <div>Terminal session</div> }))
vi.mock("../../components/LiveTerminal", () => ({ LiveTerminal: () => <div>Terminal session</div> }))
vi.mock("../../components/PromptBar", () => ({
  PromptBar: ({ onKill }: { onKill: () => void }) => <button onClick={onKill}>Dot menu kill</button>,
}))
vi.mock("../../hooks/useTaskCard", () => ({ useTaskCard: () => null }))
vi.mock("../../components/StartSwarmModal", () => ({ StartSwarmModal: () => null }))
vi.mock("../../components/SpawnEphemeralModal", () => ({ SpawnEphemeralModal: () => null }))
vi.mock("../../components/SwarmEventLog", () => ({ SwarmEventLog: () => null }))

function agent(taskId: string, role = "") {
  return {
    id: taskId === "TASK-1" ? 1 : 2,
    task_id: taskId,
    task_title: `${taskId} title`,
    tmux_session: `legato-${taskId}`,
    command: "chimera",
    agent_kind: "chimera",
    status: "running",
    activity: "working",
    role,
    started_at: "2026-08-13T00:00:00Z",
    working_seconds: 1,
    waiting_seconds: 0,
    error_seconds: 0,
  }
}

beforeEach(() => {
  mockFetch.mockReset()
  setSelectedAgentId.mockReset()
  addToast.mockReset()
  localStorage.clear()
  Object.defineProperty(window, "innerWidth", { value: 1024, configurable: true })
  vi.spyOn(window, "confirm").mockImplementation(() => { throw new Error("native confirm used") })
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe("AgentsPage sidebar context menu", () => {
  it("kills the right-clicked agent rather than the selected agent", async () => {
    mockFetch.mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      if (path === "/api/agents") {
        return Promise.resolve({ ok: true, status: 200, json: async () => [agent("TASK-1"), agent("TASK-2")] })
      }
      if (path === "/api/agents/kill") {
        return Promise.resolve({ ok: true, status: 200, text: async () => "" })
      }
      throw new Error(`unexpected request: ${path}`)
    })
    localStorage.setItem("legato:draft:TASK-1", "selected draft")
    localStorage.setItem("legato:draft:TASK-2", "target draft")

    render(<MemoryRouter><AgentsPage /></MemoryRouter>)
    const target = await screen.findByRole("button", { name: /TASK-2/ })
    fireEvent.contextMenu(target, { clientX: 50, clientY: 60 })
    fireEvent.click(screen.getByRole("menuitem", { name: "Kill agent" }))
    expect(screen.getByText("Kill this agent session?")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Kill agent" }))

    await waitFor(() => expect(mockFetch).toHaveBeenCalledWith(
      "/api/agents/kill",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ task_id: "TASK-2" }),
      }),
    ))
    expect(localStorage.getItem("legato:draft:TASK-1")).toBe("selected draft")
    expect(localStorage.getItem("legato:draft:TASK-2")).toBeNull()
    expect(setSelectedAgentId).not.toHaveBeenCalled()
    expect(screen.queryByRole("menu", { name: "Actions for TASK-2" })).toBeNull()
  })

  it("closes the menu without killing or clearing a draft when confirmation is cancelled", async () => {
    mockFetch.mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      if (path === "/api/agents") {
        return Promise.resolve({ ok: true, status: 200, json: async () => [agent("TASK-1"), agent("TASK-2")] })
      }
      throw new Error(`unexpected request: ${path}`)
    })
    localStorage.setItem("legato:draft:TASK-2", "target draft")

    render(<MemoryRouter><AgentsPage /></MemoryRouter>)
    fireEvent.contextMenu(await screen.findByRole("button", { name: /TASK-2/ }))
    fireEvent.click(screen.getByRole("menuitem", { name: "Kill agent" }))
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))

    expect(mockFetch).not.toHaveBeenCalledWith("/api/agents/kill", expect.anything())
    expect(localStorage.getItem("legato:draft:TASK-2")).toBe("target draft")
    expect(screen.queryByRole("menu", { name: "Actions for TASK-2" })).toBeNull()
  })

  it("surfaces a failed kill request", async () => {
    mockFetch.mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      if (path === "/api/agents") {
        return Promise.resolve({ ok: true, status: 200, json: async () => [agent("TASK-1"), agent("TASK-2")] })
      }
      if (path === "/api/agents/kill") {
        return Promise.resolve({ ok: false, status: 500, text: async () => "agent refused to stop" })
      }
      throw new Error(`unexpected request: ${path}`)
    })

    render(<MemoryRouter><AgentsPage /></MemoryRouter>)
    fireEvent.contextMenu(await screen.findByRole("button", { name: /TASK-2/ }))
    fireEvent.click(screen.getByRole("menuitem", { name: "Kill agent" }))
    fireEvent.click(screen.getByRole("button", { name: "Kill agent" }))

    await waitFor(() => expect(addToast).toHaveBeenCalledWith("agent refused to stop", "error"))
  })
  it("cancels the swarm from the conductor right-click menu", async () => {
    mockFetch.mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      if (path === "/api/agents") {
        return Promise.resolve({ ok: true, status: 200, json: async () => [agent("TASK-1"), agent("PARENT-1", "conductor")] })
      }
      if (path === "/api/agents/kill") {
        return Promise.resolve({ ok: true, status: 200, text: async () => "" })
      }
      throw new Error(`unexpected request: ${path}`)
    })

    render(<MemoryRouter><AgentsPage /></MemoryRouter>)
    fireEvent.contextMenu(await screen.findByRole("button", { name: /PARENT-1/ }))
    fireEvent.click(screen.getByRole("menuitem", { name: "Kill agent" }))

    expect(screen.getByText("Cancel this swarm? This kills the conductor and all workers.")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Cancel swarm" }))
    await waitFor(() => expect(mockFetch).toHaveBeenCalledWith(
      "/api/agents/kill",
      expect.objectContaining({ body: JSON.stringify({ task_id: "PARENT-1" }) }),
    ))
  })

  it("cancels the swarm from the selected conductor dot menu action", async () => {
    mockFetch.mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      if (path === "/api/agents") {
        return Promise.resolve({ ok: true, status: 200, json: async () => [agent("TASK-1", "conductor")] })
      }
      if (path === "/api/agents/kill") {
        return Promise.resolve({ ok: true, status: 200, text: async () => "" })
      }
      throw new Error(`unexpected request: ${path}`)
    })

    render(<MemoryRouter><AgentsPage /></MemoryRouter>)
    await screen.findByRole("button", { name: /TASK-1/ })
    // On a desktop the agent's terminal has no message box; its menu is the header's.
    fireEvent.click(screen.getByRole("button", { name: "More actions" }))
    fireEvent.click(screen.getByRole("menuitem", { name: "Kill agent" }))

    expect(screen.getByText("Cancel this swarm? This kills the conductor and all workers.")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Cancel swarm" }))
    await waitFor(() => expect(mockFetch).toHaveBeenCalledWith(
      "/api/agents/kill",
      expect.objectContaining({ body: JSON.stringify({ task_id: "TASK-1" }) }),
    ))
  })

  it("offers rename in the context menu and renames the right-clicked agent", async () => {
    mockFetch.mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path === "/api/agents") {
        return Promise.resolve({ ok: true, status: 200, json: async () => [agent("TASK-1"), agent("TASK-2")] })
      }
      if (path === "/api/tasks/TASK-2" && init?.method === "PATCH") {
        return Promise.resolve({ ok: true, status: 200, json: async () => ({ status: "ok" }) })
      }
      throw new Error(`unexpected request: ${path}`)
    })

    render(<MemoryRouter><AgentsPage /></MemoryRouter>)
    fireEvent.contextMenu(await screen.findByRole("button", { name: /TASK-2/ }))
    fireEvent.click(screen.getByRole("menuitem", { name: "Rename" }))
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Better name" } })
    fireEvent.click(screen.getByRole("button", { name: "Rename" }))

    await waitFor(() => expect(mockFetch).toHaveBeenCalledWith(
      "/api/tasks/TASK-2",
      expect.objectContaining({ method: "PATCH", body: JSON.stringify({ title: "Better name" }) }),
    ))
  })

  it("gives the selected agent the same entries as its dot menu", async () => {
    mockFetch.mockImplementation((input: RequestInfo | URL) => {
      if (String(input) === "/api/agents") {
        return Promise.resolve({ ok: true, status: 200, json: async () => [agent("TASK-1"), agent("TASK-2")] })
      }
      throw new Error(`unexpected request: ${String(input)}`)
    })

    render(<MemoryRouter><AgentsPage /></MemoryRouter>)
    fireEvent.contextMenu(await screen.findByRole("button", { name: /TASK-1/ }))
    const labels = screen.getAllByRole("menuitem").map((item) => item.textContent)
    expect(labels).toEqual(expect.arrayContaining(["Rename", "Refresh terminal", "Re-detect prompt", "Disconnect", "Kill agent"]))
    expect(labels[labels.length - 1]).toBe("Kill agent")
  })
})
