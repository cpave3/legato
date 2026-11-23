import "@testing-library/jest-dom/vitest"
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { AgentEnsemble, ENSEMBLE_LAYOUT_KEY } from "../AgentEnsemble"
import type { AgentInfo } from "../../hooks/useWebSocket"
import type { AgentGraphData } from "../../lib/agentGraph"
import { mockReactFlow } from "../../test/reactFlow"

mockReactFlow()

const { apiFetch } = vi.hoisted(() => ({ apiFetch: vi.fn() }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../lib/api", () => ({ apiFetch }))

const agent = (values: Partial<AgentInfo> & Pick<AgentInfo, "task_id">): AgentInfo => ({
  id: 1, card_task_id: values.task_id, slot: "main", task_title: `${values.task_id} title`, tmux_session: `legato-${values.task_id}`,
  command: "claude", agent_kind: "claude-code", status: "running", activity: "working", started_at: "2026-11-23T09:00:00Z",
  working_seconds: 0, waiting_seconds: 0, error_seconds: 0, ...values,
})

let graph: AgentGraphData

function show(agents: AgentInfo[], props: Partial<Parameters<typeof AgentEnsemble>[0]> = {}) {
  const handlers = { onOpen: vi.fn(), onSpawn: vi.fn(), onShowList: vi.fn(), onSend: vi.fn() }
  render(<AgentEnsemble agents={agents} {...handlers} {...props} />)
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

  it("draws only the agents at the stage the list is filtered to", async () => {
    show([agent({ task_id: "LEG-1", stage: "reviewing" }), agent({ task_id: "LEG-2", stage: "planning" })], { layout: "graph", stageFilter: "reviewing", onStageFilter: vi.fn() })
    expect(await node(/^LEG-1, running/)).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: /^LEG-2, running/ })).not.toBeInTheDocument()
  })
})
