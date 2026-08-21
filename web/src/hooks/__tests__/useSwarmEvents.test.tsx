import { act, cleanup, renderHook, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { useSwarmInbox } from "../useSwarmEvents"
import type { WSMessage } from "../useWebSocket"

const { apiFetch } = vi.hoisted(() => ({ apiFetch: vi.fn() }))
let handler: ((message: WSMessage) => void) | null = null
vi.mock("../useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../useWebSocket", () => ({ useWebSocket: () => ({ subscribe: (value: (message: WSMessage) => void) => { handler = value; return () => { handler = null } } }) }))
vi.mock("../../lib/api", () => ({ apiFetch }))

beforeEach(() => {
  apiFetch.mockReset()
  apiFetch.mockResolvedValue(new Response(JSON.stringify({ entries: [] }), { status: 200 }))
})
afterEach(cleanup)

it("peeks again for event-only swarm changes without draining", async () => {
  renderHook(() => useSwarmInbox("P-1"))
  await waitFor(() => expect(apiFetch).toHaveBeenCalledTimes(1))

  act(() => handler?.({ type: "swarm_changed", parent_task_id: "P-1", status: "event" }))

  await waitFor(() => expect(apiFetch).toHaveBeenCalledTimes(2))
  expect(apiFetch).toHaveBeenLastCalledWith("", "/api/swarm/inbox/P-1?peek=true")
})
