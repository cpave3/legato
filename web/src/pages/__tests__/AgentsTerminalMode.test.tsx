import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { AgentSelectionProvider } from "../../hooks/useAgentSelection"
import type { AgentInfo } from "../../hooks/useWebSocket"
import { AgentsPage } from "../Agents"
// The agents being started have their own tests, and a query client.
vi.mock("../../components/PreparingAgents", () => ({ PreparingAgents: () => null }))


const { mockFetch } = vi.hoisted(() => ({ mockFetch: vi.fn() }))

vi.stubGlobal("fetch", mockFetch)
vi.mock("../../hooks/useWebSocket", () => ({
  useWebSocket: () => ({
    send: vi.fn(),
    subscribe: vi.fn(() => vi.fn()),
    connected: true,
    connectionGeneration: 1,
  }),
}))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useSettings", () => ({
  useSettings: () => ({ data: { ntfy_configured: false, voice_enabled: false } }),
}))
vi.mock("../../hooks/useSwarmEvents", () => ({
  useSwarmEvents: () => ({ states: {}, fetchState: vi.fn() }),
}))
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
vi.mock("../../components/AgentSidebar", () => ({
  AgentSidebar: ({ agents, onSelect }: { agents: AgentInfo[]; onSelect: (taskId: string) => void }) => (
    <aside>
      {agents.map((agent) => (
        <button key={agent.task_id} onClick={() => onSelect(agent.task_id)}>{agent.task_title}</button>
      ))}
    </aside>
  ),
}))
vi.mock("../../components/AgentSelect", () => ({
  AgentSelect: () => null,
  AgentPickerBar: ({ agents, onSelect }: { agents: AgentInfo[]; onSelect: (taskId: string) => void }) => (
    <nav>{agents.map((a) => <button key={a.task_id} onClick={() => onSelect(a.task_id)}>{a.task_title}</button>)}</nav>
  ),
}))
// The terminal draws on a canvas, which jsdom lacks; it shows only before the
// agents load here.
vi.mock("../../components/TerminalPanel", () => ({ TerminalPanel: () => <div data-testid="mirror" /> }))
vi.mock("../../components/LiveTerminal", () => ({ LiveTerminal: () => <div data-testid="live" /> }))
vi.mock("../../components/PromptBar", () => ({ PromptBar: () => <textarea aria-label="Message" /> }))
vi.mock("../../hooks/useTaskCard", () => ({ useTaskCard: () => null }))
vi.mock("../../components/StartSwarmModal", () => ({ StartSwarmModal: () => null }))
vi.mock("../../components/SpawnEphemeralModal", () => ({ SpawnEphemeralModal: () => null }))
vi.mock("../../components/SwarmEventLog", () => ({ SwarmEventLog: () => null }))

const agent: AgentInfo = {
  id: 1,
  task_id: "agent-a",
  task_title: "Agent A",
  tmux_session: "legato-agent-a",
  command: "claude",
  agent_kind: "claude-code",
  status: "running",
  activity: "working",
  started_at: "2026-08-27T00:00:00Z",
  working_seconds: 1,
  waiting_seconds: 0,
  error_seconds: 0,
}

function mount(width: number) {
  Object.defineProperty(window, "innerWidth", { value: width, configurable: true })
  mockFetch.mockImplementation(async (input: RequestInfo | URL) => {
    const path = String(input)
    if (path === "/api/agents") return new Response(JSON.stringify([agent]), { status: 200 })
    if (path === "/api/macros") return new Response(JSON.stringify({ macros: [] }), { status: 200 })
    return new Response("[]", { status: 200 })
  })
  render(
    <MemoryRouter>
      <AgentSelectionProvider>
        <AgentsPage />
      </AgentSelectionProvider>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  mockFetch.mockReset()
  localStorage.clear()
})
afterEach(cleanup)

it("hides the message box in terminal mode on desktop and brings it back on exit", async () => {
  mount(1024)
  fireEvent.click(await screen.findByRole("button", { name: "Agent A" }))
  expect(await screen.findByLabelText("Message")).not.toBeNull()
  expect(screen.getByTestId("mirror")).not.toBeNull()

  fireEvent.click(screen.getByRole("button", { name: "Terminal mode" }))
  await waitFor(() => expect(screen.getByTestId("live")).not.toBeNull())
  expect(screen.queryByLabelText("Message")).toBeNull()

  fireEvent.click(screen.getByRole("button", { name: "Exit terminal mode" }))
  expect(await screen.findByLabelText("Message")).not.toBeNull()
})

it("keeps the message box in terminal mode on a phone", async () => {
  localStorage.setItem("legato:liveTerminal", "1")
  mount(400)
  fireEvent.click(await screen.findByRole("button", { name: "Agent A" }))
  await waitFor(() => expect(screen.getByTestId("live")).not.toBeNull())
  expect(screen.getByLabelText("Message")).not.toBeNull()
})
