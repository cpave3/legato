import { act, render, screen, waitFor } from "@testing-library/react"
import { QueryClientProvider } from "@tanstack/react-query"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { Layout } from "../components/Layout"
import { QuerySyncProvider } from "../components/QuerySyncProvider"
import { PlanQueuePage } from "../pages/PlanQueuePage"
import App from "../App"
import { useWorkflowRun, useWorkflowRuns, useWorkflows } from "../hooks/useWorkflow"
import { useNavigationSummary } from "../hooks/useNavigationSummary"
import { createQueryClient } from "../lib/query"
import type { WSMessage } from "../hooks/useWebSocket"

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
vi.mock("../hooks/useAgentSelection", () => ({ AgentSelectionProvider: ({ children }: { children: React.ReactNode }) => children }))
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
  it("loads only four authoritative snapshots for a board reload", async () => {
    const counts = new Map<string, number>()
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
      const path = requestPath(input)
      counts.set(path, (counts.get(path) ?? 0) + 1)
      if (path === "/api/settings") return response({ repositories: [], working_dir: "", worktrees_enabled: false, voice_enabled: false })
      if (path === "/api/navigation-summary") return response({ attention: 0, review: 0, plans: 0, workflow_attention: 0, agents: 0 })
      if (path === "/api/board") return response({ columns: [], workspaces: [] })
      if (path === "/api/workflows") return response({ definitions: [], errors: [], repositories: [] })
      throw new Error(`unexpected request: ${path}`)
    }))

    window.history.pushState({}, "", "/board")
    const client = createQueryClient()
    render(<QueryClientProvider client={client}><QuerySyncProvider><App/></QuerySyncProvider></QueryClientProvider>)

    await waitFor(() => expect([...counts.values()].reduce((total, count) => total + count, 0)).toBe(4))
    expect(Object.fromEntries(counts)).toEqual({
      "/api/navigation-summary": 1,
      "/api/board": 1,
      "/api/workflows": 1,
      "/api/settings": 1,
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

  it("shares the visible plan queue with Layout and coalesces event bursts", async () => {
    const counts = new Map<string, number>()
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
      const path = requestPath(input)
      counts.set(path, (counts.get(path) ?? 0) + 1)
      if (path === "/api/navigation-summary") return response({ attention: 0, review: 0, plans: 1, workflow_attention: 0, agents: 0 })
      if (path === "/api/plans/queue") return response([])
      throw new Error(`unexpected request: ${path}`)
    }))

    const client = createQueryClient()
    render(<QueryClientProvider client={client}><QuerySyncProvider><MemoryRouter initialEntries={["/plans"]}><Routes><Route element={<Layout/>}><Route path="/plans" element={<PlanQueuePage/>}/></Route></Routes></MemoryRouter></QuerySyncProvider></QueryClientProvider>)

    await waitFor(() => expect(counts.get("/api/plans/queue")).toBe(1))
    expect(counts.get("/api/navigation-summary")).toBe(1)
    expect(counts.has("/api/review/queue")).toBe(false)
    expect(counts.has("/api/workflow-runs")).toBe(false)
    expect(counts.has("/api/attention")).toBe(false)
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
    } finally {
      vi.useRealTimers()
    }
  })
})
