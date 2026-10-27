import { cleanup, render, screen, waitFor } from "@testing-library/react"
import { useState } from "react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { MemoryRouter, useLocation } from "react-router-dom"
import { AgentsPage } from "../Agents"

// The socket's functions keep their identity between renders, as the real
// ones do: the page reloads its agents whenever subscribe changes.
const { mockFetch, socket } = vi.hoisted(() => ({
  mockFetch: vi.fn(),
  socket: { send: () => {}, subscribe: () => () => {}, connected: true },
}))

vi.stubGlobal("fetch", mockFetch)
vi.mock("../../hooks/useWebSocket", () => ({ useWebSocket: () => socket }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
// Selection is real state here: the link has to select its agent.
vi.mock("../../hooks/useAgentSelection", () => ({
  useAgentSelection: () => {
    const [selectedAgentId, setSelectedAgentId] = useState<string | null>("TASK-1")
    return { selectedAgentId, setSelectedAgentId }
  },
  useAgentReselected: () => {},
}))
vi.mock("../../hooks/useSettings", () => ({
  useSettings: () => ({ data: { ntfy_configured: false, voice_enabled: false } }),
}))
vi.mock("../../hooks/useSwarmEvents", () => ({ useSwarmEvents: () => ({ fetchState: vi.fn(), states: {} }) }))
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
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast: vi.fn() }) }))
vi.mock("../../components/TerminalPanel", () => ({ TerminalPanel: () => <div>Terminal session</div> }))
vi.mock("../../components/PromptBar", () => ({ PromptBar: () => null }))
vi.mock("../../hooks/useTaskCard", () => ({ useTaskCard: () => null }))
vi.mock("../../components/StartSwarmModal", () => ({ StartSwarmModal: () => null }))
vi.mock("../../components/SpawnEphemeralModal", () => ({ SpawnEphemeralModal: () => null }))
vi.mock("../../components/SwarmEventLog", () => ({ SwarmEventLog: () => null }))
vi.mock("../../components/AgentArtifactPanel", () => ({
  AgentArtifactPanel: ({ agentId, focus }: { agentId: string; focus: { itemId: string } | null }) => (
    <div data-testid="artifact-panel">{agentId}:{focus?.itemId ?? "none"}</div>
  ),
}))

function agent(taskId: string) {
  return {
    id: taskId === "TASK-1" ? 1 : 2, task_id: taskId, task_title: `${taskId} title`, tmux_session: `legato-${taskId}`,
    command: "codex", agent_kind: "codex", status: "running", activity: "", role: "",
    started_at: "2026-08-13T00:00:00Z", working_seconds: 1, waiting_seconds: 0, error_seconds: 0,
  }
}

function Location() {
  const location = useLocation()
  return <div data-testid="location">{location.pathname}{location.search}</div>
}

beforeEach(() => {
  mockFetch.mockReset()
  mockFetch.mockImplementation((input: RequestInfo | URL) => {
    if (String(input) === "/api/agents") return Promise.resolve({ ok: true, status: 200, json: async () => [agent("TASK-1"), agent("TASK-2")] })
    return Promise.resolve({ ok: true, status: 200, json: async () => ({}), text: async () => "" })
  })
  localStorage.clear()
  Object.defineProperty(window, "innerWidth", { value: 1440, configurable: true })
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

// The TUI cannot draw a card, so it shows a link to it: /agents?task=…&artifact=….
it("opens the artifact panel on the linked item of the linked agent, then drops the link from the address", async () => {
  render(<MemoryRouter initialEntries={["/agents?task=TASK-2&artifact=ui-0123456789"]}><AgentsPage /><Location /></MemoryRouter>)

  await waitFor(() => expect(screen.getByTestId("artifact-panel").textContent).toBe("TASK-2:ui-0123456789"))
  // A link is followed once; it must not open the panel again on every update.
  await waitFor(() => expect(screen.getByTestId("location").textContent).toBe("/agents"))
})

it("opens nothing for a link that names only an agent", async () => {
  render(<MemoryRouter initialEntries={["/agents?task=TASK-2"]}><AgentsPage /><Location /></MemoryRouter>)

  await waitFor(() => expect(screen.getByTestId("location").textContent).toBe("/agents"))
  expect(screen.queryByTestId("artifact-panel")).toBeNull()
})
