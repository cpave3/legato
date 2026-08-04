import { act, renderHook, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { useRunningAgent } from "../useRunningAgent"
import type { AgentInfo, WSMessage } from "../useWebSocket"

const mockFetch = vi.fn()
const mockSubscribe = vi.fn()
vi.stubGlobal("fetch", mockFetch)

vi.mock("../useWebSocket", () => ({
  useWebSocket: () => ({ subscribe: mockSubscribe }),
}))

vi.mock("../useServer", () => ({
  useServer: () => ({ baseUrl: "" }),
}))

beforeEach(() => {
  mockFetch.mockReset()
  mockSubscribe.mockReset()
})

function makeAgent(taskId: string, status: string): AgentInfo {
  return {
    id: 1, task_id: taskId, task_title: "Test", tmux_session: "", command: "",
    agent_kind: "chimera", status, activity: "", started_at: "", working_seconds: 0, waiting_seconds: 0, error_seconds: 0,
  }
}

function agentsResponse(agents: AgentInfo[]) {
  return { ok: true, json: async () => agents }
}

describe("useRunningAgent", () => {
  it("returns the running agent matching the task id", async () => {
    mockSubscribe.mockImplementation(() => () => undefined)
    mockFetch.mockResolvedValueOnce(agentsResponse([makeAgent("T-1", "running"), makeAgent("T-2", "running")]))

    const { result } = renderHook(() => useRunningAgent("T-1"))
    await waitFor(() => expect(result.current.agent?.task_id).toBe("T-1"))
    expect(result.current.agent?.status).toBe("running")
  })

  it("returns null when no agent matches the task id", async () => {
    mockSubscribe.mockImplementation(() => () => undefined)
    mockFetch.mockResolvedValueOnce(agentsResponse([makeAgent("T-2", "running")]))

    const { result } = renderHook(() => useRunningAgent("T-1"))
    await waitFor(() => expect(result.current.agent).toBeNull())
  })

  it("returns null when the matching agent is not running", async () => {
    mockSubscribe.mockImplementation(() => () => undefined)
    mockFetch.mockResolvedValueOnce(agentsResponse([makeAgent("T-1", "ended")]))

    const { result } = renderHook(() => useRunningAgent("T-1"))
    await waitFor(() => expect(result.current.agent).toBeNull())
  })

  it("refetches when agents_changed fires", async () => {
    let handler: ((msg: WSMessage) => void) | undefined
    mockSubscribe.mockImplementation((next) => { handler = next; return () => undefined })
    mockFetch
      .mockResolvedValueOnce(agentsResponse([]))
      .mockResolvedValueOnce(agentsResponse([makeAgent("T-1", "running")]))

    const { result } = renderHook(() => useRunningAgent("T-1"))
    await waitFor(() => expect(result.current.agent).toBeNull())

    act(() => handler?.({ type: "agents_changed" }))
    await waitFor(() => expect(result.current.agent?.task_id).toBe("T-1"))
  })

  it("updates from agent_list message without refetching", async () => {
    let handler: ((msg: WSMessage) => void) | undefined
    mockSubscribe.mockImplementation((next) => { handler = next; return () => undefined })
    mockFetch.mockResolvedValueOnce(agentsResponse([]))

    const { result } = renderHook(() => useRunningAgent("T-1"))
    await waitFor(() => expect(result.current.agent).toBeNull())

    act(() => handler?.({ type: "agent_list", agents: [makeAgent("T-1", "running")] } as unknown as WSMessage))
    await waitFor(() => expect(result.current.agent?.task_id).toBe("T-1"))
    expect(mockFetch).toHaveBeenCalledTimes(1)
  })
})
