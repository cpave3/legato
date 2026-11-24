import "@testing-library/jest-dom/vitest"
import { QueryClientProvider } from "@tanstack/react-query"
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { AgentEnsemble, ENSEMBLE_LAYOUT_KEY } from "../AgentEnsemble"
import type { AgentInfo, WSMessage } from "../../hooks/useWebSocket"
import type { AgentGraphData, GraphRun } from "../../lib/agentGraph"
import { createQueryClient } from "../../lib/query"
import { mockReactFlow } from "../../test/reactFlow"

mockReactFlow()

const { apiFetch, fetchRun, socket } = vi.hoisted(() => ({
  apiFetch: vi.fn(),
  fetchRun: vi.fn(),
  socket: { handlers: new Set<(message: WSMessage) => void>() },
}))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../lib/api", () => ({ apiFetch }))
vi.mock("../../lib/workflow", async importOriginal => ({ ...(await importOriginal<typeof import("../../lib/workflow")>()), fetchWorkflowRun: fetchRun }))
vi.mock("../../hooks/useWebSocket", async importOriginal => ({
  ...(await importOriginal<typeof import("../../hooks/useWebSocket")>()),
  useWebSocket: () => ({ connected: true, send: vi.fn(), subscribe: (handler: (message: WSMessage) => void) => { socket.handlers.add(handler); return () => socket.handlers.delete(handler) } }),
}))

const agent = (values: Partial<AgentInfo> & Pick<AgentInfo, "task_id">): AgentInfo => ({
  id: 1, card_task_id: values.task_id, slot: "main", task_title: `${values.task_id} title`, tmux_session: `legato-${values.task_id}`,
  command: "claude", agent_kind: "claude-code", status: "running", activity: "working", started_at: "2026-11-23T09:00:00Z",
  working_seconds: 0, waiting_seconds: 0, error_seconds: 0, ...values,
})

let graph: AgentGraphData

function show(agents: AgentInfo[], props: Partial<Parameters<typeof AgentEnsemble>[0]> = {}) {
  const handlers = { onOpen: vi.fn(), onSpawn: vi.fn(), onShowList: vi.fn(), onSend: vi.fn() }
  render(<QueryClientProvider client={createQueryClient()}><MemoryRouter><AgentEnsemble agents={agents} {...handlers} {...props} /></MemoryRouter></QueryClientProvider>)
  return handlers
}

const node = (name: RegExp) => screen.findByRole("button", { name })

describe("the ensemble's graph", () => {
  beforeEach(() => {
    localStorage.clear()
    graph = { agents: {}, links: [], peer_window_seconds: 1800 }
    apiFetch.mockReset()
    apiFetch.mockImplementation(async (_baseUrl: string, path: string) => {
      if (path === "/api/agents/graph") return new Response(JSON.stringify(graph), { status: 200 })
      return new Response("not available", { status: 404 })
    })
  })

  it("is a layout beside Tiles and By stage, remembered on this device", async () => {
    graph.agents = { "LEG-1": { repo: "cpave3/legato" }, "LEG-2": { repo: "cpave3/staccato" } }
    const onLayout = vi.fn()
    show([agent({ task_id: "LEG-1" }), agent({ task_id: "LEG-2" })], { onLayout })
    fireEvent.click(screen.getByRole("button", { name: "Graph" }))

    expect(await screen.findByRole("group", { name: "cpave3/legato, 1 agent" })).toBeInTheDocument()
    expect(screen.getByRole("group", { name: "cpave3/staccato, 1 agent" })).toBeInTheDocument()
    expect(await node(/^LEG-1, running/)).toBeInTheDocument()
    expect(localStorage.getItem(ENSEMBLE_LAYOUT_KEY)).toBe("graph")
    expect(onLayout).toHaveBeenCalledWith("graph")
  })

  it("opens on the graph when the address asks for it", async () => {
    show([agent({ task_id: "LEG-1" })], { layout: "graph" })
    expect(await node(/^LEG-1, running/)).toBeInTheDocument()
  })

  it("selects an agent on a click, with a card to open it or message it", async () => {
    graph.links = [{ kind: "peer_message", from: "LEG-2", to: "LEG-1", count: 2, last_at: new Date(Date.now() - 60_000).toISOString() }]
    const handlers = show([agent({ task_id: "LEG-1", stage: "implementing" }), agent({ task_id: "LEG-2" })], { layout: "graph" })
    fireEvent.click(await node(/^LEG-1, running/))

    const card = await screen.findByRole("region", { name: "LEG-1" })
    expect(within(card).getByText("LEG-1 title")).toBeInTheDocument()
    expect(within(card).getByText(/LEG-2 messaged it 2×/)).toBeInTheDocument()
    fireEvent.change(within(card).getByRole("textbox", { name: "Message LEG-1" }), { target: { value: "Rebase first" } })
    fireEvent.keyDown(within(card).getByRole("textbox", { name: "Message LEG-1" }), { key: "Enter" })
    expect(handlers.onSend).toHaveBeenCalledWith("LEG-1", "Rebase first")
    fireEvent.click(within(card).getByRole("button", { name: "Open LEG-1" }))
    expect(handlers.onOpen).toHaveBeenCalledWith("LEG-1")
  })

  it("opens an agent on a double-click", async () => {
    const handlers = show([agent({ task_id: "LEG-1" })], { layout: "graph" })
    fireEvent.doubleClick(await node(/^LEG-1, running/))
    expect(handlers.onOpen).toHaveBeenCalledWith("LEG-1")
  })

  it("lists the agents that need you, and a click selects one", async () => {
    show([agent({ task_id: "LEG-1" }), agent({ task_id: "LEG-2", activity: "waiting" })], { layout: "graph" })
    const strip = await screen.findByRole("list", { name: "Needs you" })
    expect(within(strip).getAllByRole("listitem")).toHaveLength(1)
    fireEvent.click(within(strip).getByRole("button", { name: /LEG-2/ }))
    expect(await screen.findByRole("region", { name: "LEG-2" })).toBeInTheDocument()
  })

  it("names the kinds of link it draws in a legend", async () => {
    graph.links = [{ kind: "reeve_watch", from: "reeve-1", to: "LEG-1" }]
    show([agent({ task_id: "LEG-1" }), agent({ task_id: "reeve-1", reeve: true, task_title: "Reeve" })], { layout: "graph" })
    const legend = await screen.findByRole("list", { name: "Links" })
    await waitFor(() => expect(within(legend).getByText("Reeve watches")).toBeInTheDocument())
  })

  // Dimmed, not removed (LEG-615): a stage is state, and state never moves
  // a node.
  it("dims the agents not at the stage the list is filtered to, leaving each in its place", async () => {
    show([agent({ task_id: "LEG-1", stage: "reviewing" }), agent({ task_id: "LEG-2", stage: "planning" })], { layout: "graph", stageFilter: "reviewing", onStageFilter: vi.fn() })
    expect(await node(/^LEG-1, running/)).toBeInTheDocument()
    expect(within(await node(/^LEG-2, running/)).getByText("LEG-2 title").closest(".opacity-30")).not.toBeNull()
    expect(within(await node(/^LEG-1, running/)).getByText("LEG-1 title").closest(".opacity-30")).toBeNull()
  })

  it("says so when no agent is at the stage", async () => {
    show([agent({ task_id: "LEG-1", stage: "reviewing" })], { layout: "graph", stageFilter: "planning", onStageFilter: vi.fn() })
    expect(await screen.findByText("No agents at this stage.")).toBeInTheDocument()
  })

  it("draws the Reeve's watch lines faint, and in full while the Reeve is selected", async () => {
    graph.links = [{ kind: "reeve_watch", from: "reeve-1", to: "LEG-1" }]
    show([agent({ task_id: "LEG-1" }), agent({ task_id: "reeve-1", reeve: true, task_title: "Reeve" })], { layout: "graph" })
    const line = async () => {
      let path: SVGPathElement | null = null
      await waitFor(() => {
        path = document.querySelector<SVGPathElement>(".react-flow__edge[data-id='reeve_watch:reeve-1->LEG-1'] path.react-flow__edge-path")
        expect(path).not.toBeNull()
      })
      return path!
    }
    expect((await line()).style.opacity).toBe("0.35")
    fireEvent.click(await node(/^reeve-1, /))
    await waitFor(async () => expect((await line()).style.opacity).toBe("1"))
  })

  it("moves no node when only the agents' state changes", async () => {
    graph.agents = { "LEG-1": { repo: "a/one" }, "LEG-2": { repo: "a/one" }, "LEG-3": { repo: "a/two" } }
    const agents = [agent({ task_id: "LEG-1" }), agent({ task_id: "LEG-2", started_at: "2026-11-23T09:05:00Z" }), agent({ task_id: "LEG-3" })]
    const handlers = { onOpen: vi.fn(), onSpawn: vi.fn(), onShowList: vi.fn(), onSend: vi.fn() }
    const ui = (list: AgentInfo[]) => <QueryClientProvider client={client}><MemoryRouter><AgentEnsemble agents={list} layout="graph" {...handlers} /></MemoryRouter></QueryClientProvider>
    const client = createQueryClient()
    const { rerender } = render(ui(agents))
    await node(/^LEG-2, running/)
    const where = () => Object.fromEntries([...document.querySelectorAll<HTMLElement>(".react-flow__node")].map((n) => [n.dataset.id, n.style.transform]))
    const before = where()
    rerender(ui(agents.map((a, i) => ({ ...a, activity: i === 0 ? "waiting" : "idle", stage: "reviewing", stage_reason: "a long reason that changes", working_seconds: 999 }))))
    await node(/^LEG-1, needs you/i)
    expect(where()).toEqual(before)
  })

  it("leaves a new agent out until the graph's next read says where it works", async () => {
    graph.agents = { "LEG-1": { repo: "a/one" } }
    let answer: (() => void) | null = null
    const handlers = { onOpen: vi.fn(), onSpawn: vi.fn(), onShowList: vi.fn(), onSend: vi.fn() }
    const client = createQueryClient()
    const ui = (list: AgentInfo[]) => <QueryClientProvider client={client}><MemoryRouter><AgentEnsemble agents={list} layout="graph" {...handlers} /></MemoryRouter></QueryClientProvider>
    const { rerender } = render(ui([agent({ task_id: "LEG-1" })]))
    await node(/^LEG-1, running/)
    // The next read is slow to answer.
    apiFetch.mockImplementation(() => new Promise((resolve) => { answer = () => resolve(new Response(JSON.stringify({ ...graph, agents: { ...graph.agents, "LEG-9": { repo: "a/one" } } }), { status: 200 })) }))
    rerender(ui([agent({ task_id: "LEG-1" }), agent({ task_id: "LEG-9" })]))
    await waitFor(() => expect(answer).not.toBeNull())
    expect(screen.queryByRole("button", { name: /^LEG-9, / })).not.toBeInTheDocument()
    expect(screen.queryByRole("group", { name: /^No repository/ })).not.toBeInTheDocument()
    act(() => answer!())
    expect(await node(/^LEG-9, running/)).toBeInTheDocument()
    expect(screen.getByRole("group", { name: "a/one, 2 agents" })).toBeInTheDocument()
  })
})

// A run of three steps at its second, which LEG-1 works on; LEG-2 reviews
// at its third.
const graphRun = (values: Partial<GraphRun> = {}): GraphRun => ({
  id: "wr-1", workflow_name: "one-off-task", workflow_title: "Triage LEG-1", status: "running", current_step_id: "implement", created_at: "2026-11-23 09:00:00",
  steps: [
    { step_id: "triage", title: "Triage the task", status: "succeeded" },
    { step_id: "implement", title: "Implement it", status: "waiting_agent" },
    { step_id: "review", title: "Review in passes", status: "pending" },
  ],
  ...values,
})

const runStep = (id: string, title: string, type: string) => ({ id: `step:${id}`, kind: "step", step: { id, title, type, trigger: "automatic", key: {}, definition: { id, title, type } } })
const runView = () => ({
  run: { id: "wr-1", workflow_name: "one-off-task", workflow_title: "Triage LEG-1", definition_source: "/w.yaml", definition_digest: "d", definition_json: "{}", inputs_json: "{}", status: "running", current_step_id: "implement", created_at: "2026-11-23T09:00:00Z" },
  steps: [
    { run_id: "wr-1", step_id: "triage", step_order: 0, title: "Triage the task", executor_type: "agent.run", definition_json: "{}", resolved_inputs_json: "{}", outputs_json: "{}", status: "succeeded", latest_attempt: 1 },
    { run_id: "wr-1", step_id: "implement", step_order: 1, title: "Implement it", executor_type: "agent.run", definition_json: "{}", resolved_inputs_json: "{}", outputs_json: "{}", status: "waiting_agent", latest_attempt: 1 },
    { run_id: "wr-1", step_id: "review", step_order: 2, title: "Review in passes", executor_type: "task.note", definition_json: "{}", resolved_inputs_json: "{}", outputs_json: "{}", status: "pending", latest_attempt: 0 },
  ],
  attempts: [], artifacts: [],
  graph: { version: 1, nodes: [runStep("triage", "Triage the task", "agent.run"), runStep("implement", "Implement it", "agent.run"), runStep("review", "Review in passes", "task.note")], edges: [
    { id: "order:step:triage>step:implement", kind: "order", source: "step:triage", target: "step:implement" },
    { id: "order:step:implement>step:review", kind: "order", source: "step:implement", target: "step:review" },
  ] },
})

describe("workflow runs on the ensemble's graph", () => {
  beforeEach(() => {
    localStorage.clear()
    socket.handlers.clear()
    graph = {
      agents: {
        "LEG-1": { workflows: [{ run_id: "wr-1", step_id: "implement", role: "agent" }] },
        "LEG-2": { workflows: [{ run_id: "wr-1", step_id: "review", role: "reviewer" }] },
      },
      links: [], peer_window_seconds: 1800, runs: { "wr-1": graphRun() },
    }
    apiFetch.mockReset()
    apiFetch.mockImplementation(async (_baseUrl: string, path: string) => {
      if (path === "/api/agents/graph") return new Response(JSON.stringify(graph), { status: 200 })
      return new Response("not available", { status: 404 })
    })
    fetchRun.mockReset()
    fetchRun.mockResolvedValue(runView())
    vi.stubGlobal("matchMedia", (query: string) => ({ matches: query.includes("min-width"), media: query, addEventListener: () => {}, removeEventListener: () => {} }))
  })
  afterEach(() => { vi.unstubAllGlobals() })

  const agents = () => [agent({ task_id: "LEG-1" }), agent({ task_id: "LEG-2" }), agent({ task_id: "LEG-3" })]

  it("shows an agent's run and its step at rest, and nothing of a run for an agent in none", async () => {
    show(agents(), { layout: "graph" })
    const leg1 = await node(/^LEG-1, running.*run Triage LEG-1, step 2 of 3: Implement it/)
    expect(within(leg1).getByRole("img", { name: "Triage LEG-1: Running, step 2 of 3" })).toBeInTheDocument()
    expect(await node(/^LEG-2, running.*run Triage LEG-1, step 3 of 3: Review in passes/)).toBeInTheDocument()
    const leg3 = await node(/^LEG-3, running/)
    expect(leg3.getAttribute("aria-label")).not.toMatch(/, run /)
    expect(within(leg3).queryByRole("img")).not.toBeInTheDocument()
  })

  it("shows the agent's run in its card, with a link to the run page", async () => {
    graph.runs!["wr-1"] = graphRun({ current_step_id: "review", pass: 2 })
    show(agents(), { layout: "graph" })
    fireEvent.click(await node(/^LEG-2, running/))
    const card = await screen.findByRole("region", { name: "LEG-2" })
    const section = within(card).getByRole("group", { name: "Workflow run" })
    expect(within(section).getByRole("link", { name: "Triage LEG-1" })).toHaveAttribute("href", "/workflows/runs/wr-1")
    expect(section).toHaveTextContent("Reviewer in “Review in passes” · pass 2")
    expect(within(section).getByRole("button", { name: "Show run graph" })).toBeInTheDocument()
  })

  it("says where the run is when it has moved past the agent's step", async () => {
    graph.runs!["wr-1"] = graphRun({ current_step_id: "review" })
    show(agents(), { layout: "graph" })
    fireEvent.click(await node(/^LEG-1, running/))
    const section = within(await screen.findByRole("region", { name: "LEG-1" })).getByRole("group", { name: "Workflow run" })
    expect(section).toHaveTextContent("Runs “Implement it”")
    expect(section).toHaveTextContent("The run is at “Review in passes”")
  })

  it("opens the run's own graph beside the fleet, the agent's step selected, and Escape closes it, then the card", async () => {
    show(agents(), { layout: "graph" })
    fireEvent.keyDown(await node(/^LEG-1, running/), { key: "Enter" })
    const card = await screen.findByRole("region", { name: "LEG-1" })
    fireEvent.click(within(card).getByRole("button", { name: "Show run graph" }))

    const panel = await screen.findByRole("region", { name: "Run graph: Triage LEG-1" })
    const step = await within(panel).findByRole("button", { name: /^Step: Implement it/ })
    expect(step.className).toContain("selected")
    expect(within(panel).getByRole("link", { name: "Open run page" })).toHaveAttribute("href", "/workflows/runs/wr-1")
    expect(screen.getByRole("region", { name: "LEG-1" })).toBeInTheDocument()

    fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" })
    await waitFor(() => expect(screen.queryByRole("region", { name: "Run graph: Triage LEG-1" })).not.toBeInTheDocument())
    expect(screen.getByRole("region", { name: "LEG-1" })).toBeInTheDocument()
    fireEvent.keyDown(document.body, { key: "Escape" })
    await waitFor(() => expect(screen.queryByRole("region", { name: "LEG-1" })).not.toBeInTheDocument())
  })

  it("on a phone shows the run's graph as a sheet in place of the card, and Close goes back to the card", async () => {
    vi.stubGlobal("matchMedia", (query: string) => ({ matches: query.includes("max-width"), media: query, addEventListener: () => {}, removeEventListener: () => {} }))
    show(agents(), { layout: "graph" })
    fireEvent.click(await node(/^LEG-1, running/))
    fireEvent.click(within(await screen.findByRole("region", { name: "LEG-1" })).getByRole("button", { name: "Show run graph" }))
    const panel = await screen.findByRole("dialog", { name: "Run graph: Triage LEG-1" })
    expect(panel).toHaveAttribute("aria-modal", "true")
    expect(screen.queryByRole("region", { name: "LEG-1" })).not.toBeInTheDocument()
    // The graph behind the sheet takes no focus.
    expect(screen.getByRole("button", { name: "Fit the graph" }).closest("[inert]")).not.toBeNull()
    fireEvent.click(within(panel).getByRole("button", { name: "Close the run graph" }))
    expect(await screen.findByRole("region", { name: "LEG-1" })).toBeInTheDocument()
  })

  it("reads the graph again when a workflow run moves on", async () => {
    show(agents(), { layout: "graph" })
    await node(/^LEG-1, running/)
    const reads = () => apiFetch.mock.calls.filter(([, path]) => path === "/api/agents/graph").length
    const before = reads()
    act(() => { for (const handler of socket.handlers) handler({ type: "workflow_changed" } as WSMessage) })
    await waitFor(() => expect(reads()).toBe(before + 1))
  })

  it("opens on an agent and its run's graph when the address names them, with a way back to the run", async () => {
    show(agents(), { layout: "graph", graphFocus: { agentId: "LEG-1", runId: "wr-1" } })
    expect(await screen.findByRole("region", { name: "LEG-1" })).toBeInTheDocument()
    expect(await screen.findByRole("region", { name: "Run graph: Triage LEG-1" })).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Back to workflow" })).toHaveAttribute("href", "/workflows/runs/wr-1")
  })
})
