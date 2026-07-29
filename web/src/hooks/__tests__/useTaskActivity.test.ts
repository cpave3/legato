import { act, renderHook, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { useTaskActivity } from "../useTaskActivity"
import type { WSMessage } from "../useWebSocket"

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

const activityResponse = () => ({
  ok: true,
  json: async () => ({
    reviews: [{ id: "rt-1", task_id: "T-1", name: "Auth review", status: "ready", updated_at: "" }],
    plans: [{ id: "pl-1", task_id: "T-1", name: "", title: "Add search", summary: "FTS", status: "proposed", updated_at: "" }],
    workflow_runs: [{ run: { id: "wr-1", workflow_title: "Triage", workflow_name: "triage", status: "running", created_at: "" }, artifacts: [] }],
  }),
})

describe("useTaskActivity", () => {
  it("fetches and normalizes plans, reviews, and workflows into related-work items", async () => {
    mockSubscribe.mockImplementation(() => () => undefined)
    mockFetch.mockResolvedValueOnce(activityResponse())

    const { result } = renderHook(() => useTaskActivity("T-1"))

    await waitFor(() => expect(result.current.items.length).toBe(3))

    const plan = result.current.items.find((i) => i.kind === "plan")
    expect(plan?.href).toBe("/plans/pl-1")
    expect(plan?.title).toBe("Add search")
    expect(plan?.status).toBe("proposed")

    const review = result.current.items.find((i) => i.kind === "review")
    expect(review?.href).toBe("/review/rt-1")
    expect(review?.title).toBe("Auth review")

    const workflow = result.current.items.find((i) => i.kind === "workflow_run")
    expect(workflow?.href).toBe("/workflows/runs/wr-1")
    expect(workflow?.title).toBe("Triage")
    expect(workflow?.status).toBe("running")
  })

  it("refetches when a plan_changed event arrives for the selected task", async () => {
    let handler: ((message: WSMessage) => void) | undefined
    mockSubscribe.mockImplementation((next) => { handler = next; return () => undefined })
    mockFetch
      .mockResolvedValueOnce({ ok: true, json: async () => ({ reviews: [], plans: [], workflow_runs: [] }) })
      .mockResolvedValueOnce(activityResponse())

    const { result } = renderHook(() => useTaskActivity("T-1"))
    await waitFor(() => expect(result.current.items.length).toBe(0))

    act(() => handler?.({ type: "plan_changed", plan_id: "pl-1", task_id: "T-1", kind: "submitted" }))
    await waitFor(() => expect(result.current.items.length).toBe(3))
  })

  it("refetches when a review_changed event arrives for the selected task", async () => {
    let handler: ((message: WSMessage) => void) | undefined
    mockSubscribe.mockImplementation((next) => { handler = next; return () => undefined })
    mockFetch
      .mockResolvedValueOnce({ ok: true, json: async () => ({ reviews: [], plans: [], workflow_runs: [] }) })
      .mockResolvedValueOnce(activityResponse())

    const { result } = renderHook(() => useTaskActivity("T-1"))
    await waitFor(() => expect(result.current.items.length).toBe(0))

    act(() => handler?.({ type: "review_changed", tour_id: "rt-1", task_id: "T-1", kind: "ready" }))
    await waitFor(() => expect(result.current.items.length).toBe(3))
  })

  it("does not refetch for plan/review events targeting a different task", async () => {
    let handler: ((message: WSMessage) => void) | undefined
    mockSubscribe.mockImplementation((next) => { handler = next; return () => undefined })
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => ({ reviews: [], plans: [], workflow_runs: [] }) })

    const { result } = renderHook(() => useTaskActivity("T-1"))
    await waitFor(() => expect(result.current.items.length).toBe(0))
    expect(mockFetch).toHaveBeenCalledTimes(1)

    act(() => handler?.({ type: "plan_changed", plan_id: "pl-2", task_id: "T-OTHER", kind: "submitted" }))
    act(() => handler?.({ type: "review_changed", tour_id: "rt-2", task_id: "T-OTHER", kind: "ready" }))

    // workflow_changed carries no task_id and always triggers a refetch; verify
    // plan/review cross-task events do not.
    await waitFor(() => expect(mockFetch).toHaveBeenCalledTimes(1))
  })

  it("rehydrates from the activity endpoint after remount, not stale events", async () => {
    mockSubscribe.mockImplementation(() => () => undefined)
    mockFetch.mockResolvedValueOnce(activityResponse())

    const first = renderHook(() => useTaskActivity("T-1"))
    await waitFor(() => expect(first.result.current.items.length).toBe(3))
    const firstCount = first.result.current.items.length
    expect(firstCount).toBe(3)
    first.unmount()

    mockFetch.mockReset()
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => ({ reviews: [], plans: [], workflow_runs: [] }) })
    const second = renderHook(() => useTaskActivity("T-1"))
    await waitFor(() => expect(second.result.current.items.length).toBe(0))
    expect(mockFetch).toHaveBeenCalledTimes(1)
  })
})
