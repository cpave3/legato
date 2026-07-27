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
    tour: { id: "rt-task-1", task_id: "T-1", name: "Auth review", status: "ready", summary },
    steps: [],
    messages: [],
  }),
})

describe("useReviewTour", () => {
  it("refetches only when review_changed belongs to its tour", async () => {
    let handler: ((message: WSMessage) => void) | undefined
    mockSubscribe.mockImplementation((next) => {
      handler = next
      return () => undefined
    })
    mockFetch.mockResolvedValueOnce(response("Initial")).mockResolvedValueOnce(response("Updated"))

    const { result } = renderHook(() => useReviewTour("rt-task-1"))
    await waitFor(() => expect(result.current.data?.tour.summary).toBe("Initial"))

    act(() => handler?.({ type: "review_changed", tour_id: "rt-task-2" }))
    expect(mockFetch).toHaveBeenCalledTimes(1)

    act(() => handler?.({ type: "review_changed", tour_id: "rt-task-1", step_id: "S-1", kind: "answer" }))
    await waitFor(() => expect(result.current.data?.tour.summary).toBe("Updated"))
    expect(mockFetch).toHaveBeenCalledTimes(2)
  })

  it("keeps the newest tour snapshot when overlapping refreshes finish out of order", async () => {
    let handler: ((message: WSMessage) => void) | undefined
    let resolveInitial: (value: ReturnType<typeof response>) => void = () => undefined
    let resolveAnswer: (value: ReturnType<typeof response>) => void = () => undefined
    mockSubscribe.mockImplementation((next) => {
      handler = next
      return () => undefined
    })
    mockFetch
      .mockImplementationOnce(() => new Promise((resolve) => { resolveInitial = resolve }))
      .mockImplementationOnce(() => new Promise((resolve) => { resolveAnswer = resolve }))

    const { result } = renderHook(() => useReviewTour("rt-task-1"))
    await waitFor(() => expect(mockFetch).toHaveBeenCalledTimes(1))

    act(() => handler?.({ type: "review_changed", tour_id: "rt-task-1", step_id: "S-1", kind: "answer" }))
    await waitFor(() => expect(mockFetch).toHaveBeenCalledTimes(2))

    await act(async () => { resolveAnswer(response("Includes answer")) })
    await waitFor(() => expect(result.current.data?.tour.summary).toBe("Includes answer"))

    await act(async () => { resolveInitial(response("Before answer")) })
    expect(result.current.data?.tour.summary).toBe("Includes answer")
  })
})
