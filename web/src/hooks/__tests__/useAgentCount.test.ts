import { act, renderHook, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { useAgentCount } from "../useAgentCount"
import type { AgentInfo, WSMessage } from "../useWebSocket"

const { apiFetch, subscribe, server } = vi.hoisted(() => ({
  apiFetch: vi.fn(),
  subscribe: vi.fn(),
  server: { baseUrl: "" },
}))

vi.mock("../../lib/api", () => ({ apiFetch }))
vi.mock("../useWebSocket", () => ({ useWebSocket: () => ({ subscribe }) }))
vi.mock("../useServer", () => ({ useServer: () => ({ baseUrl: server.baseUrl }) }))

function makeAgent(activity: string): AgentInfo {
  return {
    id: 1,
    task_id: `task-${activity || "idle"}`,
    task_title: "Test agent",
    tmux_session: "",
    command: "",
    agent_kind: "chimera",
    status: "running",
    activity,
    started_at: "",
    working_seconds: 0,
    waiting_seconds: 0,
    error_seconds: 0,
  }
}

function agentsResponse(agents: AgentInfo[]): Response {
  return { ok: true, json: async () => agents } as Response
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((next) => { resolve = next })
  return { promise, resolve }
}

beforeEach(() => {
  apiFetch.mockReset()
  subscribe.mockReset()
  subscribe.mockReturnValue(() => undefined)
  server.baseUrl = ""
})

describe("useAgentCount", () => {
  it("counts every non-empty activity and excludes idle agents", async () => {
    apiFetch.mockResolvedValueOnce(agentsResponse([
      makeAgent("working"),
      makeAgent("waiting"),
      makeAgent("error"),
      makeAgent("future-state"),
      makeAgent(""),
    ]))

    const { result } = renderHook(() => useAgentCount())

    await waitFor(() => expect(result.current).toBe(4))
    expect(apiFetch).toHaveBeenCalledWith("", "/api/agents")
  })

  it("replaces the count from an agent_list snapshot without refetching", async () => {
    let handler: ((message: WSMessage) => void) | undefined
    subscribe.mockImplementation((next) => {
      handler = next
      return () => undefined
    })
    apiFetch.mockResolvedValueOnce(agentsResponse([makeAgent("working")]))

    const { result } = renderHook(() => useAgentCount())
    await waitFor(() => expect(result.current).toBe(1))

    act(() => handler?.({
      type: "agent_list",
      agents: [makeAgent(""), makeAgent("waiting"), makeAgent("future-state")],
    }))

    expect(result.current).toBe(2)
    expect(apiFetch).toHaveBeenCalledTimes(1)
  })

  it("does not let an older fetch overwrite an agent_list snapshot", async () => {
    let handler: ((message: WSMessage) => void) | undefined
    subscribe.mockImplementation((next) => {
      handler = next
      return () => undefined
    })
    const initialFetch = deferred<Response>()
    apiFetch.mockReturnValueOnce(initialFetch.promise)

    const { result } = renderHook(() => useAgentCount())
    await waitFor(() => expect(apiFetch).toHaveBeenCalledTimes(1))

    act(() => handler?.({
      type: "agent_list",
      agents: [makeAgent("working"), makeAgent("waiting")],
    }))
    expect(result.current).toBe(2)

    await act(async () => {
      initialFetch.resolve(agentsResponse([makeAgent("working")]))
      await initialFetch.promise
    })

    expect(result.current).toBe(2)
  })

  it("refetches after agents_changed and updates active-to-idle transitions", async () => {
    let handler: ((message: WSMessage) => void) | undefined
    subscribe.mockImplementation((next) => {
      handler = next
      return () => undefined
    })
    apiFetch
      .mockResolvedValueOnce(agentsResponse([makeAgent("working")]))
      .mockResolvedValueOnce(agentsResponse([makeAgent("")]))

    const { result } = renderHook(() => useAgentCount())
    await waitFor(() => expect(result.current).toBe(1))

    act(() => handler?.({ type: "agents_changed" }))

    await waitFor(() => expect(result.current).toBe(0))
    expect(apiFetch).toHaveBeenCalledTimes(2)
  })

  it("ignores a stale response after the selected server changes", async () => {
    const oldServer = deferred<Response>()
    apiFetch
      .mockReturnValueOnce(oldServer.promise)
      .mockResolvedValueOnce(agentsResponse([makeAgent("working"), makeAgent("waiting")]))

    const { result, rerender } = renderHook(() => useAgentCount())
    await waitFor(() => expect(apiFetch).toHaveBeenCalledWith("", "/api/agents"))

    server.baseUrl = "https://remote.example"
    rerender()

    await waitFor(() => expect(result.current).toBe(2))
    expect(apiFetch).toHaveBeenCalledWith("https://remote.example", "/api/agents")

    await act(async () => {
      oldServer.resolve(agentsResponse([makeAgent("working")]))
      await oldServer.promise
    })

    expect(result.current).toBe(2)
  })

  it("retains the last confirmed count after a transient fetch failure", async () => {
    let handler: ((message: WSMessage) => void) | undefined
    subscribe.mockImplementation((next) => {
      handler = next
      return () => undefined
    })
    apiFetch
      .mockResolvedValueOnce(agentsResponse([makeAgent("working")]))
      .mockRejectedValueOnce(new Error("temporarily unavailable"))

    const { result } = renderHook(() => useAgentCount())
    await waitFor(() => expect(result.current).toBe(1))

    act(() => handler?.({ type: "agents_changed" }))
    await waitFor(() => expect(apiFetch).toHaveBeenCalledTimes(2))

    expect(result.current).toBe(1)
  })
})
