import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { AgentsPage } from "../Agents"

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
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast }) }))
vi.mock("../../components/TerminalPanel", () => ({ TerminalPanel: () => <div>Terminal session</div> }))
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
  vi.spyOn(window, "confirm").mockReturnValue(true)
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

    await waitFor(() => expect(mockFetch).toHaveBeenCalledWith(
      "/api/agents/kill",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ task_id: "TASK-2" }),
      }),
    ))
    expect(window.confirm).toHaveBeenCalledWith("Kill this agent session?")
    expect(localStorage.getItem("legato:draft:TASK-1")).toBe("selected draft")
    expect(localStorage.getItem("legato:draft:TASK-2")).toBeNull()
    expect(setSelectedAgentId).not.toHaveBeenCalled()
    expect(screen.queryByRole("menu", { name: "Actions for TASK-2" })).toBeNull()
  })

  it("closes the menu without killing or clearing a draft when confirmation is cancelled", async () => {
    vi.mocked(window.confirm).mockReturnValue(false)
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

    expect(window.confirm).toHaveBeenCalledWith("Cancel this swarm? This kills the conductor and all workers.")
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
    fireEvent.click(screen.getByRole("button", { name: "Dot menu kill" }))

    expect(window.confirm).toHaveBeenCalledWith("Cancel this swarm? This kills the conductor and all workers.")
    await waitFor(() => expect(mockFetch).toHaveBeenCalledWith(
      "/api/agents/kill",
      expect.objectContaining({ body: JSON.stringify({ task_id: "TASK-1" }) }),
    ))
  })

})
