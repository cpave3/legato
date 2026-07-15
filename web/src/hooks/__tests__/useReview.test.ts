import { act, renderHook, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { useReviewTour } from "../useReview"
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

const response = (summary: string) => ({
  ok: true,
  json: async () => ({
    tour: { task_id: "T-1", status: "ready", summary },
    steps: [],
    messages: [],
  }),
})

describe("useReviewTour", () => {
  it("refetches only when review_changed belongs to its task", async () => {
    let handler: ((message: WSMessage) => void) | undefined
    mockSubscribe.mockImplementation((next) => {
      handler = next
      return () => undefined
    })
    mockFetch.mockResolvedValueOnce(response("Initial")).mockResolvedValueOnce(response("Updated"))

    const { result } = renderHook(() => useReviewTour("T-1"))
    await waitFor(() => expect(result.current.data?.tour.summary).toBe("Initial"))

    act(() => handler?.({ type: "review_changed", task_id: "T-2" }))
    expect(mockFetch).toHaveBeenCalledTimes(1)

    act(() => handler?.({ type: "review_changed", task_id: "T-1", step_id: "S-1", kind: "answer" }))
    await waitFor(() => expect(result.current.data?.tour.summary).toBe("Updated"))
    expect(mockFetch).toHaveBeenCalledTimes(2)
  })
})
