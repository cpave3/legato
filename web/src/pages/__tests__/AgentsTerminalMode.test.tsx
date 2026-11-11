import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { AgentSelectionProvider } from "../../hooks/useAgentSelection"
import type { AgentInfo } from "../../hooks/useWebSocket"
import { AgentsPage } from "../Agents"
// The agents being started have their own tests, and a query client.
vi.mock("../../components/PreparingAgents", () => ({ PreparingAgents: () => null }))
vi.mock("../../components/InterruptedAgents", () => ({ InterruptedAgents: () => null }))


const { mockFetch, mockSend, liveMounts } = vi.hoisted(() => ({ mockFetch: vi.fn(), mockSend: vi.fn(), liveMounts: { count: 0 } }))

vi.stubGlobal("fetch", mockFetch)
vi.mock("../../hooks/useWebSocket", () => ({
  useWebSocket: () => ({
    send: mockSend,
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
vi.mock("../../components/LiveTerminal", async () => {
  const { useEffect } = await import("react")
  return {
    LiveTerminal: () => {
      useEffect(() => { liveMounts.count++ }, [])
      return <div data-testid="live" />
    },
  }
})
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

it("opens a tmux agent in terminal mode, with no message box on a desktop", async () => {
  mount(1024)
  fireEvent.click(await screen.findByRole("button", { name: "Agent A" }))
  await waitFor(() => expect(screen.getByTestId("live")).not.toBeNull())
  expect(screen.queryByTestId("mirror")).toBeNull()
  expect(screen.queryByRole("button", { name: /terminal mode/i })).toBeNull()
  expect(screen.queryByLabelText("Message")).toBeNull()
  expect(screen.queryByRole("button", { name: "Escape" })).toBeNull()
})

it("keeps the message box on a phone, with keys the phone's keyboard lacks", async () => {
  mount(400)
  fireEvent.click(await screen.findByRole("button", { name: "Agent A" }))
  await waitFor(() => expect(screen.getByTestId("live")).not.toBeNull())
  expect(screen.getByLabelText("Message")).not.toBeNull()

  for (const [label, keys] of [["Escape", "Escape"], ["Shift+Tab", "BTab"], ["Up", "Up"], ["Down", "Down"]]) {
    mockSend.mockClear()
    fireEvent.click(screen.getByRole("button", { name: label }))
    expect(mockSend).toHaveBeenCalledWith({ type: "send_keys", agent_id: "agent-a", keys })
  }
})

it("sends Ctrl with the next character typed after Ctrl is pressed", async () => {
  mount(400)
  fireEvent.click(await screen.findByRole("button", { name: "Agent A" }))
  const ctrl = await screen.findByRole("button", { name: "Ctrl" })
  fireEvent.click(ctrl)
  expect(ctrl.getAttribute("aria-pressed")).toBe("true")

  mockSend.mockClear()
  const typed = new InputEvent("beforeinput", { data: "C", inputType: "insertText", bubbles: true, cancelable: true })
  act(() => { screen.getByLabelText("Message").dispatchEvent(typed) })
  expect(typed.defaultPrevented).toBe(true)
  expect(mockSend).toHaveBeenCalledWith({ type: "send_keys", agent_id: "agent-a", keys: "C-c" })
  expect(ctrl.getAttribute("aria-pressed")).toBe("false")

  // Disarmed: the next character is typed as usual.
  const next = new InputEvent("beforeinput", { data: "x", inputType: "insertText", bubbles: true, cancelable: true })
  act(() => { screen.getByLabelText("Message").dispatchEvent(next) })
  expect(next.defaultPrevented).toBe(false)
})

it("reconnects the terminal when it is refreshed from the menu", async () => {
  mount(1024)
  fireEvent.click(await screen.findByRole("button", { name: "Agent A" }))
  await waitFor(() => expect(screen.getByTestId("live")).not.toBeNull())
  const before = liveMounts.count
  fireEvent.click(screen.getByRole("button", { name: "More actions" }))
  fireEvent.click(screen.getByRole("menuitem", { name: "Refresh terminal" }))
  await waitFor(() => expect(liveMounts.count).toBe(before + 1))
})
