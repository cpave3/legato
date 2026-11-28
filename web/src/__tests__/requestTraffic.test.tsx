import { act, render, screen, waitFor } from "@testing-library/react"
import { useQuery, QueryClientProvider } from "@tanstack/react-query"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { Layout } from "../components/Layout"
import { QuerySyncProvider } from "../components/QuerySyncProvider"
import { PlanQueuePage } from "../pages/PlanQueuePage"
import App from "../App"
import { useWorkflowRun, useWorkflowRuns, useWorkflows } from "../hooks/useWorkflow"
import { useNavigationSummary } from "../hooks/useNavigationSummary"
import { useTaskCard } from "../hooks/useTaskCard"
import { useBoard } from "../hooks/useBoard"
import { useDevices } from "../hooks/useDevices"
import { usePreparingAgents, useRepositoryCache } from "../hooks/usePreparingAgents"
import { fetchHandoff } from "../lib/handoff"
import { createQueryClient, queryKeys } from "../lib/query"
import type { WSMessage } from "../lib/agentWire"

const { websocket } = vi.hoisted(() => ({
  websocket: {
    handler: undefined as ((message: WSMessage) => void) | undefined,
    connectionGeneration: 1,
    baseUrl: "",
  },
}))

vi.mock("../hooks/useServer", () => ({ useServer: () => ({ baseUrl: websocket.baseUrl, servers: [], activeServerName: "Local", setActiveServer: vi.fn() }) }))
vi.mock("../hooks/useWebSocket", () => ({
  useWebSocket: () => ({
    connected: true,
    connectionGeneration: websocket.connectionGeneration,
    subscribe: (handler: (message: WSMessage) => void) => { websocket.handler = handler; return () => { websocket.handler = undefined } },
  }),
}))
vi.mock("../components/PlanApprovalModal", () => ({ PlanApprovalModal: () => null }))
vi.mock("../components/OfflineDialog", () => ({ OfflineDialog: () => null }))
vi.mock("../components/AutoRunNotice", () => ({ AutoRunNotice: () => null }))
vi.mock("../hooks/useAgentSelection", () => ({ AgentSelectionProvider: ({ children }: { children: React.ReactNode }) => children, useAgentSelection: () => ({ selectedAgentId: null }) }))
vi.mock("../hooks/useToast", () => ({ useToast: () => ({ addToast: vi.fn() }) }))
vi.mock("../components/board/BoardGrid", () => ({ BoardGrid: () => null }))
vi.mock("../components/board/MobileBoardList", () => ({ MobileBoardList: () => null }))

function response(data: unknown) {
  return Promise.resolve({ ok: true, json: async () => data, text: async () => "" } as Response)
}

function requestPath(input: RequestInfo | URL) {
  return new URL(String(input), "http://legato.test").pathname
}

function WorkflowObservers() {
  useWorkflows()
  useWorkflowRuns()
  useWorkflowRun("run-a")
  useWorkflowRun("run-b")
  useNavigationSummary()
  return null
}

beforeEach(() => {
  websocket.handler = undefined
  websocket.connectionGeneration = 1
  websocket.baseUrl = ""
})

describe("web request traffic", () => {
  it("loads only five authoritative snapshots for a board reload", async () => {
    const counts = new Map<string, number>()
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
      const path = requestPath(input)
      // The build version shown in the rail is read once and is no snapshot.
      if (path === "/api/meta") return response({ version: "v1.0.0" })
      counts.set(path, (counts.get(path) ?? 0) + 1)
      if (path === "/api/settings") return response({ repositories: [], working_dir: "", worktrees_enabled: false, voice_enabled: false })
      if (path === "/api/navigation-summary") return response({ attention: 0, review: 0, plans: 0, workflow_attention: 0, agents: 0 })
      if (path === "/api/board") return response({ columns: [], workspaces: [] })
      if (path === "/api/workflows") return response({ definitions: [], errors: [], repositories: [] })
      // The needs-you rail, on every page.
      if (path === "/api/attention") return response({ items: [], total: 0, hidden_groups: 0 })
      throw new Error(`unexpected request: ${path}`)
    }))

    window.history.pushState({}, "", "/board")
    const client = createQueryClient()
    render(<QueryClientProvider client={client}><QuerySyncProvider><App/></QuerySyncProvider></QueryClientProvider>)

    await waitFor(() => expect([...counts.values()].reduce((total, count) => total + count, 0)).toBe(5))
    expect(Object.fromEntries(counts)).toEqual({
      "/api/navigation-summary": 1,
      "/api/board": 1,
      "/api/workflows": 1,
      "/api/settings": 1,
      "/api/attention": 1,
    })

    vi.useFakeTimers()
    try {
      act(() => { websocket.handler?.({ type: "agents_changed" }) })
      await act(async () => { await vi.advanceTimersByTimeAsync(75) })
      expect(Object.fromEntries(counts)).toEqual({
        "/api/navigation-summary": 2,
        "/api/board": 2,
        "/api/workflows": 1,
        "/api/settings": 1,
        "/api/attention": 1,
      })

      act(() => {
        for (let index = 0; index < 20; index++) {
          websocket.handler?.({ type: "agents_changed" })
          websocket.handler?.({ type: "cards_changed" })
        }
      })
      await act(async () => { await vi.advanceTimersByTimeAsync(75) })
      expect(Object.fromEntries(counts)).toEqual({
        "/api/navigation-summary": 3,
        "/api/board": 3,
        "/api/workflows": 1,
        "/api/settings": 1,
        "/api/attention": 1,
      })
    } finally {
      vi.useRealTimers()
    }
  })

  it("refreshes visible navigation counts after one minute without a websocket event", async () => {
    let navigationRequests = 0
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
      const path = requestPath(input)
      if (path !== "/api/navigation-summary") throw new Error(`unexpected request: ${path}`)
      navigationRequests += 1
      return response({ attention: 0, review: 0, plans: 0, workflow_attention: 0, agents: navigationRequests === 1 ? 0 : 3 })
    }))

    vi.useFakeTimers()
    try {
      const client = createQueryClient()
      render(<QueryClientProvider client={client}><QuerySyncProvider><MemoryRouter initialEntries={["/agents"]}><Routes><Route element={<Layout/>}><Route path="/agents" element={<div>Agents page</div>}/></Route></Routes></MemoryRouter></QuerySyncProvider></QueryClientProvider>)

      await act(async () => { await vi.advanceTimersByTimeAsync(0) })
      expect(navigationRequests).toBe(1)
      expect(screen.getAllByLabelText("Agents").length).toBeGreaterThan(0)

      await act(async () => { await vi.advanceTimersByTimeAsync(59_999) })
      expect(navigationRequests).toBe(1)

      await act(async () => { await vi.advanceTimersByTimeAsync(1) })
      expect(navigationRequests).toBe(2)
      await act(async () => { await vi.advanceTimersByTimeAsync(1) })
      expect(navigationRequests).toBe(2)
      expect(screen.getAllByLabelText("Agents: 3 non-idle").length).toBeGreaterThan(0)
    } finally {
      vi.useRealTimers()
    }
  })

  it("revalidates cached snapshots when switching servers", async () => {
    const client = createQueryClient()
    client.setQueryData(["https://remote.example", "navigation-summary"], { attention: 0, review: 0, plans: 0, workflow_attention: 0, agents: 0 })
    function Probe() {
      useNavigationSummary()
      return null
    }
    const fetchMock = vi.fn(() => response({ attention: 0, review: 0, plans: 0, workflow_attention: 0, agents: 0 }))
    vi.stubGlobal("fetch", fetchMock)
    const view = render(<QueryClientProvider client={client}><QuerySyncProvider><Probe/></QuerySyncProvider></QueryClientProvider>)
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))

    websocket.baseUrl = "https://remote.example"
    view.rerender(<QueryClientProvider client={client}><QuerySyncProvider><Probe/></QuerySyncProvider></QueryClientProvider>)
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
  })

  it("scopes workflow events to runs and never refreshes definitions", async () => {
    const counts = new Map<string, number>()
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
      const path = requestPath(input)
      counts.set(path, (counts.get(path) ?? 0) + 1)
      if (path === "/api/workflows") return response({ definitions: [], errors: [], repositories: [] })
      if (path === "/api/workflow-runs") return response([])
      if (path === "/api/workflow-runs/run-a" || path === "/api/workflow-runs/run-b") return response({ run: { id: path.endsWith("run-a") ? "run-a" : "run-b" }, steps: [], attempts: [], artifacts: [] })
      if (path === "/api/navigation-summary") return response({ attention: 0, review: 0, plans: 0, workflow_attention: 0, agents: 0 })
      throw new Error(`unexpected request: ${path}`)
    }))
    const client = createQueryClient()
    render(<QueryClientProvider client={client}><QuerySyncProvider><WorkflowObservers/></QuerySyncProvider></QueryClientProvider>)
    await waitFor(() => expect([...counts.values()].reduce((total, count) => total + count, 0)).toBe(5))

    vi.useFakeTimers()
    try {
      act(() => { for (let index = 0; index < 20; index++) websocket.handler?.({ type: "workflow_changed", workflow_run_id: "run-a" }) })
      await act(async () => { await vi.advanceTimersByTimeAsync(75) })
      expect(Object.fromEntries(counts)).toEqual({
        "/api/workflows": 1,
        "/api/workflow-runs": 2,
        "/api/workflow-runs/run-a": 2,
        "/api/workflow-runs/run-b": 1,
        "/api/navigation-summary": 2,
      })
    } finally {
      vi.useRealTimers()
    }
  })

  function countingWorkflowFetch(counts: Map<string, number>) {
    return vi.fn((input: RequestInfo | URL) => {
      const path = requestPath(input)
      counts.set(path, (counts.get(path) ?? 0) + 1)
      if (path === "/api/workflows") return response({ definitions: [], errors: [], repositories: [] })
      if (path === "/api/workflow-runs") return response([])
      if (path === "/api/workflow-runs/run-a" || path === "/api/workflow-runs/run-b") return response({ run: { id: path.endsWith("run-a") ? "run-a" : "run-b" }, steps: [], attempts: [], artifacts: [] })
      if (path === "/api/navigation-summary") return response({ attention: 0, review: 0, plans: 0, workflow_attention: 0, agents: 0 })
      if (path === "/api/review/queue") return response([])
      throw new Error(`unexpected request: ${path}`)
    })
  }

  it("reads the trigger states again when a trigger or a workflow file changes", async () => {
    const counts = new Map<string, number>()
    vi.stubGlobal("fetch", countingWorkflowFetch(counts))
    const client = createQueryClient()
    render(<QueryClientProvider client={client}><QuerySyncProvider><WorkflowObservers/></QuerySyncProvider></QueryClientProvider>)
    await waitFor(() => expect(counts.get("/api/navigation-summary")).toBe(1))

    vi.useFakeTimers()
    try {
      act(() => { websocket.handler?.({ type: "workflow_changed", kind: "trigger" }) })
      await act(async () => { await vi.advanceTimersByTimeAsync(75) })
      expect(counts.get("/api/workflows")).toBe(2)
      // No run is named: no run page reads itself again.
      expect(counts.get("/api/workflow-runs/run-a")).toBe(1)
      act(() => { websocket.handler?.({ type: "workflow_changed", kind: "definitions" }) })
      await act(async () => { await vi.advanceTimersByTimeAsync(75) })
      expect(counts.get("/api/workflows")).toBe(3)
    } finally {
      vi.useRealTimers()
    }
  })

  it("refreshes a run's list at least once a second while its events keep coming", async () => {
    const counts = new Map<string, number>()
    vi.stubGlobal("fetch", countingWorkflowFetch(counts))
    const client = createQueryClient()
    render(<QueryClientProvider client={client}><QuerySyncProvider><WorkflowObservers/></QuerySyncProvider></QueryClientProvider>)
    await waitFor(() => expect(counts.get("/api/workflow-runs")).toBe(1))

    vi.useFakeTimers()
    try {
      // An event every 50 ms never leaves the 75 ms quiet a burst waits for.
      for (let elapsed = 0; elapsed < 2000; elapsed += 50) {
        act(() => { websocket.handler?.({ type: "workflow_changed", workflow_run_id: "run-a", kind: "step_started" }) })
        await act(async () => { await vi.advanceTimersByTimeAsync(50) })
      }
      expect(counts.get("/api/workflow-runs")).toBeGreaterThanOrEqual(3)
      expect(counts.get("/api/workflow-runs")).toBeLessThanOrEqual(4)
      expect(counts.get("/api/workflow-runs/run-a")).toBeGreaterThanOrEqual(3)
      expect(counts.get("/api/workflow-runs/run-b")).toBe(1)
    } finally {
      vi.useRealTimers()
    }
  })

  it("reads every open view again when the socket reconnects", async () => {
    const counts = new Map<string, number>()
    vi.stubGlobal("fetch", countingWorkflowFetch(counts))
    function Observers() {
      useWorkflowRun("run-a")
      useWorkflowRuns()
      useQuery({ queryKey: queryKeys.reviewQueue("", false), queryFn: () => fetch("/api/review/queue").then((res) => res.json()) })
      return null
    }
    const client = createQueryClient()
    const tree = () => <QueryClientProvider client={client}><QuerySyncProvider><Observers/></QuerySyncProvider></QueryClientProvider>
    const view = render(tree())
    await waitFor(() => expect(counts.get("/api/review/queue")).toBe(1))
    expect(counts.get("/api/workflow-runs/run-a")).toBe(1)

    // A laptop wakes: the socket opens again.
    websocket.connectionGeneration = 2
    view.rerender(tree())
    await waitFor(() => expect(counts.get("/api/review/queue")).toBe(2))
    expect(counts.get("/api/workflow-runs/run-a")).toBe(2)
    expect(counts.get("/api/workflow-runs")).toBe(2)
  })

  it("shares the visible plan queue with Layout and coalesces event bursts", async () => {
    const counts = new Map<string, number>()
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
      const path = requestPath(input)
      counts.set(path, (counts.get(path) ?? 0) + 1)
      if (path === "/api/navigation-summary") return response({ attention: 0, review: 0, plans: 1, workflow_attention: 0, agents: 0 })
      if (path === "/api/plans/queue") return response([])
      if (path === "/api/attention") return response({ items: [], total: 0, hidden_groups: 0 })
      throw new Error(`unexpected request: ${path}`)
    }))

    const client = createQueryClient()
    render(<QueryClientProvider client={client}><QuerySyncProvider><MemoryRouter initialEntries={["/plans"]}><Routes><Route element={<Layout/>}><Route path="/plans" element={<PlanQueuePage/>}/></Route></Routes></MemoryRouter></QuerySyncProvider></QueryClientProvider>)

    await waitFor(() => expect(counts.get("/api/plans/queue")).toBe(1))
    expect(counts.get("/api/navigation-summary")).toBe(1)
    expect(counts.has("/api/review/queue")).toBe(false)
    expect(counts.has("/api/workflow-runs")).toBe(false)
    // The needs-you rail's view, once: plans' own changes do not ask again.
    expect(counts.get("/api/attention")).toBe(1)
    expect(counts.has("/api/agents")).toBe(false)

    vi.useFakeTimers()
    try {
      act(() => {
        for (let index = 0; index < 20; index++) websocket.handler?.({ type: "plan_changed", plan_id: `plan-${index}` })
      })
      expect(counts.get("/api/plans/queue")).toBe(1)
      await act(async () => { await vi.advanceTimersByTimeAsync(75) })
      expect(counts.get("/api/plans/queue")).toBe(2)
      expect(counts.get("/api/navigation-summary")).toBe(2)
      expect(counts.get("/api/attention")).toBe(1)
    } finally {
      vi.useRealTimers()
    }
  })
})

it("refreshes only the server-scoped handoff named by review_changed", async () => {
  const fetchMock = vi.fn<typeof fetch>(() => response({ tour_id: "a", checked_at: "", packets: [] }))
  vi.stubGlobal("fetch", fetchMock)
  function HandoffObservers() {
    useQuery({ queryKey: queryKeys.handoff("", "a"), queryFn: () => fetchHandoff("", "a") })
    useQuery({ queryKey: queryKeys.handoff("", "b"), queryFn: () => fetchHandoff("", "b") })
    useQuery({ queryKey: queryKeys.handoff("https://remote.example", "a"), queryFn: () => fetchHandoff("https://remote.example", "a") })
    return null
  }
  render(<QueryClientProvider client={createQueryClient()}><QuerySyncProvider><HandoffObservers /></QuerySyncProvider></QueryClientProvider>)
  await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3))
  act(() => { websocket.handler?.({ type: "review_changed", tour_id: "a" }) })
  await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(4))
  expect(fetchMock.mock.calls.at(-1)?.[0]).toBe("/api/review/tours/a/handoff")
})

it("reads an agent's card again when the cards change", async () => {
  let reads = 0
  vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
    const path = requestPath(input)
    if (path !== "/api/tasks/AG-1") throw new Error(`unexpected request: ${path}`)
    reads += 1
    return response({ id: "AG-1", title: "Cover", status: reads === 1 ? "Column one" : "Column two", pr_meta: null })
  }))
  function CardStatus() {
    const card = useTaskCard("AG-1")
    return <span>{card?.status ?? "loading"}</span>
  }

  render(<QueryClientProvider client={createQueryClient()}><QuerySyncProvider><CardStatus /></QuerySyncProvider></QueryClientProvider>)
  await screen.findByText("Column one")
  act(() => { websocket.handler?.({ type: "cards_changed" }) })
  await screen.findByText("Column two")
  expect(reads).toBe(2)
})

it("reads the board again when Attention changes, so a card says what needs the operator", async () => {
  let reads = 0
  vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
    const path = requestPath(input)
    if (path === "/api/board") {
      reads += 1
      return response({ columns: [{ name: "Doing", cards: [{ id: "T-1", title: reads === 1 ? "Before" : "After" }] }], workspaces: [] })
    }
    return response({})
  }))
  function BoardTitle() {
    const board = useBoard()
    return <span>{board.columns[0]?.cards[0]?.title ?? "loading"}</span>
  }

  render(<QueryClientProvider client={createQueryClient()}><QuerySyncProvider><BoardTitle /></QuerySyncProvider></QueryClientProvider>)
  await screen.findByText("Before")
  act(() => { websocket.handler?.({ type: "attention_changed" }) })
  await screen.findByText("After")
  expect(reads).toBe(2)
})

// LEG-543: a review loop's agent author that stops or starts work changes
// what the run waits on; the rail hears of it as attention_changed, and an
// open run page reads the run again so the two agree.
it("reads an open run again when Attention changes, so its Needs you agrees with the rail", async () => {
  let reads = 0
  vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
    const path = requestPath(input)
    if (path !== "/api/workflow-runs/run-a") return response({})
    reads += 1
    return response({ run: { id: "run-a", status: reads === 1 ? "waiting" : "running" }, steps: [], attempts: [], artifacts: [] })
  }))
  function RunStatus() {
    const run = useWorkflowRun("run-a")
    return <span>{run.data?.run.status ?? "loading"}</span>
  }

  render(<QueryClientProvider client={createQueryClient()}><QuerySyncProvider><RunStatus /></QuerySyncProvider></QueryClientProvider>)
  await screen.findByText("waiting")
  act(() => { websocket.handler?.({ type: "attention_changed" }) })
  await screen.findByText("running")
  expect(reads).toBe(2)
})

it("reads the devices again when an approval changes, as one is enrolled", async () => {
  let reads = 0
  vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
    const path = requestPath(input)
    if (path !== "/api/devices") throw new Error(`unexpected request: ${path}`)
    reads += 1
    return response({ devices: [{ id: "dev-p", kind: "passkey", name: "Desktop", status: reads === 1 ? "pending" : "active", created_at: "" }], enrolled: true, server_id: "srv-1" })
  }))
  function DeviceStatus() {
    const devices = useDevices()
    return <span>{devices.data?.devices[0]?.status ?? "loading"}</span>
  }

  render(<QueryClientProvider client={createQueryClient()}><QuerySyncProvider><DeviceStatus /></QuerySyncProvider></QueryClientProvider>)
  await screen.findByText("pending")
  act(() => { websocket.handler?.({ type: "approvals_changed" }) })
  await screen.findByText("active")
  expect(reads).toBe(2)
})

it("reads the agents being started again when the agents change", async () => {
  let reads = 0
  vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
    const path = requestPath(input)
    if (path !== "/api/preparing-agents") throw new Error(`unexpected request: ${path}`)
    reads += 1
    return response(reads === 1 ? [{ task_id: "T-1", title: "Cloning", started_at: "" }] : [])
  }))
  function Preparing() {
    const { data } = usePreparingAgents()
    return <span>{data ? `${data.length} starting` : "loading"}</span>
  }

  render(<QueryClientProvider client={createQueryClient()}><QuerySyncProvider><Preparing /></QuerySyncProvider></QueryClientProvider>)
  await screen.findByText("1 starting")
  act(() => { websocket.handler?.({ type: "agents_changed" }) })
  await screen.findByText("0 starting")
})

it("takes a repository's copy from the message, without asking again", async () => {
  let reads = 0
  vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
    const path = requestPath(input)
    if (path !== "/api/repositories") throw new Error(`unexpected request: ${path}`)
    reads += 1
    return response({ repositories: [{ name: "Widget", repo: "acme/widget", cache: { repo: "acme/widget", state: "copying", percent: 10 } }] })
  }))
  function Copy() {
    const { data } = useRepositoryCache()
    const status = data?.["acme/widget"]
    return <span>{status ? `${status.state} ${status.percent ?? ""}`.trim() : "loading"}</span>
  }

  render(<QueryClientProvider client={createQueryClient()}><QuerySyncProvider><Copy /></QuerySyncProvider></QueryClientProvider>)
  await screen.findByText("copying 10")
  act(() => { websocket.handler?.({ type: "repository_cache", cache: { repo: "acme/widget", state: "copying", percent: 64 } }) })
  await screen.findByText("copying 64")
  act(() => { websocket.handler?.({ type: "repository_cache", cache: { repo: "acme/widget", state: "ready" } }) })
  await screen.findByText("ready")
  expect(reads).toBe(1)
})
