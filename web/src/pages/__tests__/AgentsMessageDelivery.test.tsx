import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { AgentSelectionProvider } from "../../hooks/useAgentSelection"
import type { AgentInfo } from "../../hooks/useWebSocket"
import { AgentsPage } from "../Agents"
// The agents being started have their own tests, and a query client.
vi.mock("../../components/PreparingAgents", () => ({ PreparingAgents: () => null }))
vi.mock("../../components/InterruptedAgents", () => ({ InterruptedAgents: () => null }))


const { mockFetch, mockSend, liveMounts, handlers, addToast } = vi.hoisted(() => ({
  mockFetch: vi.fn(),
  mockSend: vi.fn(),
  liveMounts: { count: 0 },
  handlers: [] as Array<(msg: { type: string; [key: string]: unknown }) => void>,
  addToast: vi.fn(),
}))

vi.stubGlobal("fetch", mockFetch)
vi.mock("../../hooks/useWebSocket", () => ({
  useWebSocket: () => ({
    send: mockSend,
    subscribe: vi.fn((handler: (msg: { type: string; [key: string]: unknown }) => void) => { handlers.push(handler); return vi.fn() }),
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
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast }) }))
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
vi.mock("../../components/LiveTerminal", async () => {
  const { useEffect } = await import("react")
  return {
    LiveTerminal: () => {
      useEffect(() => { liveMounts.count++ }, [])
      return <div data-testid="live" />
    },
  }
})
vi.mock("../../components/PromptBar", () => ({
  PromptBar: ({ onSubmitMessage }: { onSubmitMessage: (content: string) => void }) => (
    <button onClick={() => onSubmitMessage("Image: /tmp/a.png what is this?")}>Send</button>
  ),
}))
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
  activity: "idle",
  started_at: "2026-08-27T00:00:00Z",
  working_seconds: 1,
  waiting_seconds: 0,
  error_seconds: 0,
}

beforeEach(() => {
  mockFetch.mockReset()
  mockSend.mockReset()
  addToast.mockReset()
  handlers.length = 0
  localStorage.clear()
  Object.defineProperty(window, "innerWidth", { value: 400, configurable: true })
  mockFetch.mockImplementation(async (input: RequestInfo | URL) => {
    const path = String(input)
    if (path === "/api/agents") return new Response(JSON.stringify([agent]), { status: 200 })
    if (path === "/api/macros") return new Response(JSON.stringify({ macros: [] }), { status: 200 })
    return new Response("[]", { status: 200 })
  })
})
afterEach(cleanup)

// LEG-151: a message to a terminal agent that tmux could not deliver says so,
// in the server's words, instead of vanishing.
it("shows a message the server could not deliver to a terminal agent as an error", async () => {
  render(
    <MemoryRouter>
      <AgentSelectionProvider>
        <AgentsPage />
      </AgentSelectionProvider>
    </MemoryRouter>,
  )
  fireEvent.click(await screen.findByRole("button", { name: "Agent A" }))
  fireEvent.click(await screen.findByRole("button", { name: "Send" }))

  const sent = mockSend.mock.calls.map(([message]) => message).find((message) => message.type === "agent_message")
  expect(sent).toMatchObject({ agent_id: "agent-a", content: "Image: /tmp/a.png what is this?", request_id: expect.any(String) })

  act(() => {
    for (const handler of handlers) handler({ type: "error", agent_id: "agent-a", request_id: "someone else's", error: "not ours" })
  })
  expect(addToast).not.toHaveBeenCalled()

  act(() => {
    for (const handler of handlers) handler({ type: "error", agent_id: "agent-a", request_id: sent.request_id, error: "send message failed: tmux send-keys: can't find session" })
  })
  expect(addToast).toHaveBeenCalledWith(expect.stringContaining("can't find session"), "error")
})

it("says a message to a terminal agent was not sent when Legato is not connected", async () => {
  mockSend.mockImplementation((message: { type: string }) => message.type !== "agent_message")
  render(
    <MemoryRouter>
      <AgentSelectionProvider>
        <AgentsPage />
      </AgentSelectionProvider>
    </MemoryRouter>,
  )
  fireEvent.click(await screen.findByRole("button", { name: "Agent A" }))
  fireEvent.click(await screen.findByRole("button", { name: "Send" }))

  expect(addToast).toHaveBeenCalledWith(expect.stringMatching(/not sent/i), "error")
})

it("takes no error for a message the agent already got as its own", async () => {
  render(
    <MemoryRouter>
      <AgentSelectionProvider>
        <AgentsPage />
      </AgentSelectionProvider>
    </MemoryRouter>,
  )
  fireEvent.click(await screen.findByRole("button", { name: "Agent A" }))
  fireEvent.click(await screen.findByRole("button", { name: "Send" }))
  const sent = mockSend.mock.calls.map(([message]) => message).find((message) => message.type === "agent_message")

  act(() => {
    for (const handler of handlers) handler({ type: "agent_message_sent", agent_id: "agent-a", request_id: sent.request_id })
    for (const handler of handlers) handler({ type: "error", agent_id: "agent-a", request_id: sent.request_id, error: "late" })
  })

  expect(addToast).not.toHaveBeenCalled()
})
