import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { useState } from "react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { MemoryRouter, useLocation } from "react-router-dom"
import { AgentsPage } from "../Agents"

// The socket's functions keep their identity between renders, as the real
// ones do: the page reloads its agents whenever subscribe changes.
const { mockFetch, socket } = vi.hoisted(() => ({
  mockFetch: vi.fn(),
  socket: { send: vi.fn(), subscribe: () => () => {}, connected: true },
}))

vi.stubGlobal("fetch", mockFetch)
vi.mock("../../hooks/useWebSocket", () => ({ useWebSocket: () => socket }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
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
vi.mock("../../components/TerminalPanel", () => ({
  TerminalPanel: ({ agentId }: { agentId: string }) => <div>Terminal of {agentId}</div>,
}))
vi.mock("../../components/PromptBar", () => ({ PromptBar: () => <textarea aria-label="Message" /> }))
vi.mock("../../hooks/useTaskCard", () => ({
  useTaskCard: (id: string | null) => id ? {
    id, title: `${id} title`, status: "Doing",
    pr_meta: { repo: "acme/app", branch: "feature", pr_number: 12, pr_url: "https://github.com/acme/app/pull/12", state: "OPEN", is_draft: false, review_decision: "", check_status: "pass", comment_count: 0 },
  } : null,
}))
vi.mock("../../components/StartSwarmModal", () => ({ StartSwarmModal: () => null }))
vi.mock("../../components/SpawnEphemeralModal", () => ({
  SpawnEphemeralModal: ({ open }: { open: boolean }) => open ? <div role="dialog">Spawn an agent</div> : null,
}))
vi.mock("../../components/SwarmEventLog", () => ({ SwarmEventLog: () => null }))
vi.mock("../../components/AgentArtifactPanel", () => ({ AgentArtifactPanel: () => null }))

function agent(taskId: string, activity = "") {
  return {
    id: taskId === "TASK-1" ? 1 : 2, task_id: taskId, task_title: `${taskId} title`, tmux_session: `legato-${taskId}`,
    command: "codex", agent_kind: "codex", status: "running", activity, role: "",
    started_at: "2026-08-13T00:00:00Z", working_seconds: 1, waiting_seconds: 0, error_seconds: 0,
  }
}

function Location() {
  const location = useLocation()
  return <div data-testid="location">{location.pathname}{location.search}</div>
}

function open(address: string) {
  render(<MemoryRouter initialEntries={[address]}><AgentsPage /><Location /></MemoryRouter>)
}

const address = () => screen.getByTestId("location").textContent

beforeEach(() => {
  mockFetch.mockReset()
  socket.send.mockReset()
  mockFetch.mockImplementation((input: RequestInfo | URL) => {
    const path = String(input)
    if (path === "/api/agents") return Promise.resolve({ ok: true, status: 200, json: async () => [agent("TASK-1"), agent("TASK-2", "waiting")] })
    if (path.endsWith("/preview")) return Promise.resolve({ ok: true, status: 200, json: async () => ({ content: `pane of ${path.split("/")[3]}` }) })
    return Promise.resolve({ ok: true, status: 200, json: async () => ({}), text: async () => "" })
  })
  localStorage.clear()
  Object.defineProperty(window, "innerWidth", { value: 1440, configurable: true })
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

it("shows every agent at once at /agents?view=ensemble, in place of one agent's terminal", async () => {
  open("/agents?view=ensemble")

  await waitFor(() => expect(screen.getByRole("region", { name: "Ensemble" })).toBeTruthy())
  // The agent that needs the person comes first.
  expect(screen.getAllByRole("article").map((tile) => tile.getAttribute("aria-label"))).toEqual(["TASK-2, needs you", "TASK-1, idle"])
  await waitFor(() => expect(screen.getByText("pane of TASK-2")).toBeTruthy())
  expect(screen.queryByText("Terminal of TASK-1")).toBeNull()
})

it("does not stream the selected agent's terminal while the ensemble is shown", async () => {
  open("/agents?view=ensemble")

  await waitFor(() => expect(screen.getByRole("region", { name: "Ensemble" })).toBeTruthy())
  expect(socket.send).not.toHaveBeenCalledWith(expect.objectContaining({ type: "subscribe_agent" }))
})

it("opens an agent from its tile and leaves the ensemble", async () => {
  open("/agents?view=ensemble")
  fireEvent.click(await screen.findByRole("button", { name: "Open TASK-2" }))

  await waitFor(() => expect(screen.getByText("Terminal of TASK-2")).toBeTruthy())
  expect(address()).toBe("/agents")
  expect(screen.queryByRole("region", { name: "Ensemble" })).toBeNull()
})

it("goes to the ensemble from the agent list and back", async () => {
  open("/agents")
  fireEvent.click(await screen.findByRole("button", { name: "Ensemble" }))

  await waitFor(() => expect(address()).toBe("/agents?view=ensemble"))
  fireEvent.click(screen.getByRole("button", { name: "List" }))

  await waitFor(() => expect(address()).toBe("/agents"))
  expect(screen.getByText("Terminal of TASK-1")).toBeTruthy()
})

it("goes to the ensemble from the agent picker of a phone", async () => {
  Object.defineProperty(window, "innerWidth", { value: 390, configurable: true })
  open("/agents")
  fireEvent.click(await screen.findByRole("button", { name: "Ensemble" }))

  await waitFor(() => expect(screen.getByRole("region", { name: "Ensemble" })).toBeTruthy())
})

it("changes between the list and the ensemble with e", async () => {
  open("/agents")
  await screen.findByText("Terminal of TASK-1")

  fireEvent.keyDown(window, { key: "e" })
  await waitFor(() => expect(address()).toBe("/agents?view=ensemble"))

  fireEvent.keyDown(window, { key: "e" })
  await waitFor(() => expect(address()).toBe("/agents"))
})

it("leaves e to the message box and to an open dialog", async () => {
  open("/agents")
  await screen.findByText("Terminal of TASK-1")

  fireEvent.keyDown(screen.getByLabelText("Message"), { key: "e" })
  fireEvent.keyDown(window, { key: "e", ctrlKey: true })
  expect(address()).toBe("/agents")

  fireEvent.click(screen.getByTitle("Spawn new agent"))
  await screen.findByRole("dialog")
  fireEvent.keyDown(window, { key: "e" })
  expect(address()).toBe("/agents")
})

it("spawns an agent from the ensemble", async () => {
  open("/agents?view=ensemble")
  fireEvent.click(await screen.findByRole("button", { name: "Spawn agent" }))

  expect(await screen.findByRole("dialog")).toBeTruthy()
})

it("switches with a number key in the order the agent list shows", async () => {
  open("/agents")
  await screen.findByText("Terminal of TASK-1")
  // TASK-2 waits on the person, so the list shows it first.
  fireEvent.keyDown(window, { key: "Control", ctrlKey: true })
  fireEvent.keyDown(window, { key: "1", ctrlKey: true })
  await screen.findByText("Terminal of TASK-2")
})

it("sends a message typed into a tile to that agent", async () => {
  open("/agents?view=ensemble")
  const box = await screen.findByRole("textbox", { name: "Message TASK-2" })
  fireEvent.change(box, { target: { value: "go ahead" } })
  fireEvent.keyDown(box, { key: "Enter" })

  expect(socket.send).toHaveBeenCalledWith({ type: "agent_message", agent_id: "TASK-2", content: "go ahead" })
})

it("heads a terminal session with its task, its pull request and its actions", async () => {
  open("/agents")
  await screen.findByText("Terminal of TASK-1")
  const header = screen.getByRole("banner")

  expect(header.textContent).toContain("TASK-1 title")
  expect(within(header).getByText("TASK-1 · PR #12")).toBeTruthy()
  expect(within(header).queryByRole("button", { name: "Open card" })).toBeNull()
  fireEvent.click(screen.getByTitle("More actions"))
  expect(screen.getByRole("menuitem", { name: "Refresh terminal" })).toBeTruthy()
})

it("opens the work related to the agent beside its chat, with a way to its card", async () => {
  open("/agents")
  await screen.findByText("Terminal of TASK-1")
  const related = within(screen.getByRole("banner")).getByRole("button", { name: "Related work (2)" })

  fireEvent.click(related)
  expect(related.getAttribute("aria-pressed")).toBe("true")
  const panel = screen.getByRole("complementary", { name: "Related work" })
  expect(within(panel).getByRole("link", { name: /Pull request · #12/ })).toBeTruthy()
  fireEvent.click(within(panel).getByRole("link", { name: /Card · TASK-1/ }))
  await waitFor(() => expect(address()).toBe("/board?highlight_task=TASK-1"))
})

it("closes the related work when Related is pressed again", async () => {
  open("/agents")
  await screen.findByText("Terminal of TASK-1")
  const related = within(screen.getByRole("banner")).getByRole("button", { name: "Related work (2)" })

  fireEvent.click(related)
  fireEvent.click(related)
  expect(screen.queryByRole("complementary", { name: "Related work" })).toBeNull()
})
