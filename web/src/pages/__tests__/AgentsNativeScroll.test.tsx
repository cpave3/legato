import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { AgentSelectionProvider } from "../../hooks/useAgentSelection"
import type { AgentInfo } from "../../hooks/useWebSocket"
import { AgentsPage } from "../Agents"

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
vi.mock("../../components/AgentSelect", () => ({ AgentSelect: () => null }))
vi.mock("../../components/RelatedWorkStrip", () => ({ RelatedWorkStrip: () => null }))
vi.mock("../../components/StartSwarmModal", () => ({ StartSwarmModal: () => null }))
vi.mock("../../components/SpawnEphemeralModal", () => ({ SpawnEphemeralModal: () => null }))
vi.mock("../../components/SwarmEventLog", () => ({ SwarmEventLog: () => null }))

function nativeAgent(taskId: string, title: string): AgentInfo {
  return {
    id: taskId === "agent-a" ? 1 : 2,
    task_id: taskId,
    task_title: title,
    tmux_session: `legato-${taskId}`,
    command: "chimera",
    agent_kind: "chimera",
    status: "running",
    activity: "working",
    started_at: "2026-08-27T00:00:00Z",
    working_seconds: 1,
    waiting_seconds: 0,
    error_seconds: 0,
    has_chimera_endpoint: true,
  }
}

function snapshot(id: string, latestReply: string) {
  return {
    id,
    status: "idle",
    messages: [
      { role: "user", content: "Launch prompt" },
      { role: "assistant", content: [{ type: "text", text: latestReply }] },
    ],
    toolCalls: [],
  }
}

beforeEach(() => {
  mockFetch.mockReset()
  localStorage.clear()
  Object.defineProperty(window, "innerWidth", { value: 1024, configurable: true })
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

it("opens a newly selected native agent at the latest activity", async () => {
  let resolveAgentB: ((response: Response) => void) | undefined
  const agentBSnapshot = new Promise<Response>((resolve) => { resolveAgentB = resolve })
  const agents = [nativeAgent("agent-a", "Agent A"), nativeAgent("agent-b", "Agent B")]

  mockFetch.mockImplementation(async (input: RequestInfo | URL) => {
    const path = String(input)
    if (path === "/api/agents") return new Response(JSON.stringify(agents), { status: 200 })
    if (path.includes("/agent-a/chimera/snapshot")) {
      return new Response(JSON.stringify(snapshot("session-a", "Agent A latest reply")), { status: 200 })
    }
    if (path.includes("/agent-b/chimera/snapshot")) return agentBSnapshot
    if (path === "/api/macros") return new Response(JSON.stringify({ macros: [] }), { status: 200 })
    return new Response("[]", { status: 200 })
  })

  let scrollTop = 0
  vi.spyOn(HTMLElement.prototype, "scrollHeight", "get").mockReturnValue(1200)
  vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockReturnValue(300)
  vi.spyOn(HTMLElement.prototype, "scrollTop", "get").mockImplementation(() => scrollTop)
  vi.spyOn(HTMLElement.prototype, "scrollTop", "set").mockImplementation((value) => {
    scrollTop = Math.min(Number(value), 900)
  })

  render(
    <MemoryRouter>
      <AgentSelectionProvider>
        <AgentsPage />
      </AgentSelectionProvider>
    </MemoryRouter>,
  )

  fireEvent.click(await screen.findByRole("button", { name: "Agent A" }))
  expect(await screen.findByText("Agent A latest reply")).not.toBeNull()
  await waitFor(() => expect(scrollTop).toBe(900))

  fireEvent.click(screen.getByRole("button", { name: "Agent B" }))
  await waitFor(() => expect(mockFetch.mock.calls.some(
    ([input]) => String(input) === "/api/agents/agent-b/chimera/snapshot?initial=true",
  )).toBe(true))
  scrollTop = 0
  resolveAgentB?.(new Response(JSON.stringify(snapshot("session-b", "Agent B latest reply")), { status: 200 }))

  expect(await screen.findByText("Agent B latest reply")).not.toBeNull()
  await waitFor(() => expect(scrollTop).toBe(900))
})
