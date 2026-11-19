import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { useImperativeHandle, useState, type Ref } from "react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { AgentsPage } from "../Agents"
// The agents being started have their own tests, and a query client.
vi.mock("../../components/PreparingAgents", () => ({ PreparingAgents: () => null }))
vi.mock("../../components/InterruptedAgents", () => ({ InterruptedAgents: () => null }))


// The socket's functions keep their identity between renders, as the real
// ones do: the page reloads its agents whenever subscribe changes.
const { mockFetch, socket, voice, selection } = vi.hoisted(() => ({
  mockFetch: vi.fn(),
  voice: { enabled: false, start: vi.fn() },
  selection: { initial: null as string | null },
  socket: { send: vi.fn(), subscribe: () => () => {}, connected: true },
}))

vi.stubGlobal("fetch", mockFetch)
vi.mock("../../hooks/useWebSocket", () => ({ useWebSocket: () => socket }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useAgentSelection", () => ({
  useAgentSelection: () => {
    const [selectedAgentId, setSelectedAgentId] = useState<string | null>(selection.initial)
    return { selectedAgentId, setSelectedAgentId }
  },
  useAgentReselected: () => {},
}))
vi.mock("../../hooks/useSettings", () => ({
  useSettings: () => ({ data: { ntfy_configured: false, voice_enabled: voice.enabled } }),
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
// The terminal is typed into through xterm's own text box, named as xterm names it.
vi.mock("../../components/LiveTerminal", () => ({
  LiveTerminal: ({ agentId }: { agentId: string }) => <div>Terminal of {agentId}<textarea aria-label="Terminal input" /></div>,
}))
vi.mock("../../components/PromptBar", () => ({
  PromptBar: ({ ref }: { ref: Ref<unknown> }) => {
    useImperativeHandle(ref, () => ({ voiceStart: voice.start, voiceIsRecording: () => false }))
    return <textarea aria-label="Message" />
  },
}))
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

beforeEach(() => {
  mockFetch.mockImplementation(async () => new Response("[]", { status: 200, headers: { "Content-Type": "application/json" } }))
})
afterEach(cleanup)

// With no agents, the page offers to start one, in the spawn dialog.
it("an empty Agents page offers to start an agent", async () => {
  render(<MemoryRouter initialEntries={["/agents"]}><AgentsPage /></MemoryRouter>)
  fireEvent.click(await screen.findByRole("button", { name: "Start an agent" }))
  expect(screen.getByRole("dialog").textContent).toMatch(/Spawn an agent/)
})
