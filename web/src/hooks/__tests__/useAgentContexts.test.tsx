import { act, cleanup, renderHook } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"
import { useAgentContexts } from "../useAgentContexts"
import type { AgentInfo, WSMessage } from "../useWebSocket"

let handler: ((message: WSMessage) => void) | null = null
vi.mock("../useWebSocket", () => ({ useWebSocket: () => ({ subscribe: (value: (message: WSMessage) => void) => { handler = value; return () => { handler = null } } }) }))

afterEach(cleanup)

function agent(task_id: string, extra: Partial<AgentInfo> = {}): AgentInfo {
  return { id: 1, task_id, task_title: "", tmux_session: "", command: "", agent_kind: "claude-code", status: "running", activity: "", started_at: "", working_seconds: 0, waiting_seconds: 0, error_seconds: 0, ...extra }
}

it("starts from the readings the agent list carries", () => {
  const { result } = renderHook(() => useAgentContexts([agent("a", { context_used: 90_400, context_window: 1_000_000 }), agent("b")]))
  expect(result.current.a).toEqual({ used: 90_400, window: 1_000_000, share: 9 })
  expect(result.current.b).toBeUndefined()
})

it("follows the readings the server pushes", () => {
  const { result } = renderHook(() => useAgentContexts([agent("a", { context_used: 1000, context_window: 100_000 })]))
  act(() => handler?.({ type: "agent_context", agent_id: "a", context_used: 73_000, context_window: 100_000 }))
  expect(result.current.a?.share).toBe(73)
  act(() => handler?.({ type: "agent_context", agent_id: "b", context_used: 5, context_window: 10 }))
  expect(result.current.b?.share).toBe(50)
})

it("keeps a pushed reading when the agent list is read again with an older one", () => {
  const agents = [agent("a", { context_used: 1000, context_window: 100_000 })]
  const { result, rerender } = renderHook(({ list }) => useAgentContexts(list), { initialProps: { list: agents } })
  act(() => handler?.({ type: "agent_context", agent_id: "a", context_used: 50_000, context_window: 100_000 }))
  rerender({ list: [agent("a", { context_used: 50_000, context_window: 100_000 })] })
  expect(result.current.a?.share).toBe(50)
})
