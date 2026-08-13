import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { AgentsPage } from "../Agents"

const mockFetch = vi.fn()
const subscribe = vi.fn(() => vi.fn())
vi.stubGlobal("fetch", mockFetch)

vi.mock("../../hooks/useWebSocket", () => ({
  useWebSocket: () => ({ send: vi.fn(), subscribe, connected: true }),
}))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useAgentSelection", () => ({
  useAgentSelection: () => ({ selectedAgentId: "TASK-42", setSelectedAgentId: vi.fn() }),
}))
vi.mock("../../hooks/useSettings", () => ({
  useSettings: () => ({ data: { ntfy_configured: false, voice_enabled: false } }),
}))
vi.mock("../../hooks/useSwarmEvents", () => ({ useSwarmEvents: () => ({ fetchState: vi.fn() }) }))
vi.mock("../../hooks/useTaskActivity", () => ({ useTaskActivity: () => ({ items: [] }) }))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast: vi.fn() }) }))
vi.mock("../../components/TerminalPanel", () => ({ TerminalPanel: () => <div>Terminal session</div> }))
vi.mock("../../components/NativeAgentPanel", () => ({
  NativeAgentPanel: ({ onDowngradeChimera }: { onDowngradeChimera?: () => Promise<void> }) => (
    <div>
      Native Chimera session
      <button onClick={() => void onDowngradeChimera?.()}>Downgrade to tmux</button>
    </div>
  ),
}))
vi.mock("../../components/AgentSidebar", () => ({ AgentSidebar: () => <aside>Agents</aside> }))
vi.mock("../../components/AgentSelect", () => ({ AgentSelect: () => null }))
vi.mock("../../components/RelatedWorkStrip", () => ({ RelatedWorkStrip: () => null }))
vi.mock("../../components/StartSwarmModal", () => ({ StartSwarmModal: () => null }))
vi.mock("../../components/SpawnEphemeralModal", () => ({ SpawnEphemeralModal: () => null }))
vi.mock("../../components/SwarmEventLog", () => ({ SwarmEventLog: () => null }))

const terminalAgent = {
  id: 1,
  task_id: "TASK-42",
  task_title: "Chimera task",
  tmux_session: "legato-TASK-42",
  command: "chimera",
  agent_kind: "chimera",
  status: "running",
  activity: "",
  started_at: "now",
  working_seconds: 0,
  waiting_seconds: 0,
  error_seconds: 0,
  has_chimera_endpoint: false,
}

beforeEach(() => {
  mockFetch.mockReset()
  subscribe.mockClear()
  localStorage.clear()
})

afterEach(cleanup)

it("manually downgrades a native panel and replaces it with the terminal", async () => {
  let agentFetches = 0
  mockFetch.mockImplementation((input: RequestInfo | URL) => {
    const path = String(input)
    if (path === "/api/agents") {
      agentFetches += 1
      return Promise.resolve({
        ok: true,
        status: 200,
        json: async () => [{ ...terminalAgent, has_chimera_endpoint: agentFetches === 1 }],
      })
    }
    if (path === "/api/macros") {
      return Promise.resolve({ ok: true, json: async () => ({ macros: [] }) })
    }
    if (path === "/api/agents/TASK-42/chimera/downgrade") {
      return Promise.resolve({ ok: true, status: 200, text: async () => "" })
    }
    throw new Error(`unexpected request: ${path}`)
  })

  render(<MemoryRouter><AgentsPage /></MemoryRouter>)
  expect(await screen.findByText("Native Chimera session")).not.toBeNull()

  fireEvent.click(screen.getByRole("button", { name: "Downgrade to tmux" }))

  expect(await screen.findByText("Terminal session")).not.toBeNull()
  expect(screen.queryByText("Native Chimera session")).toBeNull()
  expect(screen.getByPlaceholderText("Type a message...")).not.toBeNull()
  expect(mockFetch).toHaveBeenCalledWith(
    "/api/agents/TASK-42/chimera/downgrade",
    expect.objectContaining({ method: "POST" }),
  )
})

it("rescans Chimera and replaces the terminal with the native panel", async () => {
  let agentFetches = 0
  mockFetch.mockImplementation((input: RequestInfo | URL) => {
    const path = String(input)
    if (path === "/api/agents") {
      agentFetches += 1
      return Promise.resolve({
        ok: true,
        status: 200,
        json: async () => [{ ...terminalAgent, has_chimera_endpoint: agentFetches > 1 }],
      })
    }
    if (path === "/api/macros") {
      return Promise.resolve({ ok: true, json: async () => ({ macros: [] }) })
    }
    if (path === "/api/agents/TASK-42/chimera/discover") {
      return Promise.resolve({ ok: true, status: 200, text: async () => "" })
    }
    throw new Error(`unexpected request: ${path}`)
  })

  render(<MemoryRouter><AgentsPage /></MemoryRouter>)
  expect(await screen.findByText("Terminal session")).not.toBeNull()

  fireEvent.click(screen.getByTitle("More actions"))
  fireEvent.click(screen.getByRole("button", { name: "Upgrade to native Chimera" }))

  await waitFor(() => expect(screen.queryByText("Terminal session")).toBeNull())
  expect(screen.getByText("Native Chimera session")).not.toBeNull()
  expect(mockFetch).toHaveBeenCalledWith(
    "/api/agents/TASK-42/chimera/discover",
    expect.objectContaining({ method: "POST" }),
  )
})
