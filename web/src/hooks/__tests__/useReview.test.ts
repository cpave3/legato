import { QueryClientProvider } from "@tanstack/react-query"
import { createElement } from "react"
import { renderHook, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { createQueryClient } from "../../lib/query"
import { useReviewQueue, useReviewTour } from "../useReview"

const mockFetch = vi.fn()
vi.stubGlobal("fetch", mockFetch)
vi.mock("../useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))

function wrapper() {
  const client = createQueryClient()
  return ({ children }: { children: React.ReactNode }) => createElement(QueryClientProvider, { client }, children)
}

beforeEach(() => mockFetch.mockReset())

describe("useReviewQueue", () => {
  it("uses distinct shared keys for ready and capturing queues", async () => {
    mockFetch.mockResolvedValue({ ok: true, json: async () => [] })
    const sharedWrapper = wrapper()
    const ready = renderHook(() => useReviewQueue(false), { wrapper: sharedWrapper })
    const secondReady = renderHook(() => useReviewQueue(false), { wrapper: sharedWrapper })
    await waitFor(() => expect(ready.result.current.data).toEqual([]))
    await waitFor(() => expect(secondReady.result.current.data).toEqual([]))
    expect(mockFetch).toHaveBeenCalledTimes(1)

    const capturing = renderHook(() => useReviewQueue(true), { wrapper: sharedWrapper })
    await waitFor(() => expect(capturing.result.current.data).toEqual([]))
    expect(mockFetch).toHaveBeenCalledWith("/api/review/queue?include_capturing=true", expect.anything())
    expect(mockFetch).toHaveBeenCalledTimes(2)
  })
})

describe("useReviewTour", () => {
  it("shares a settled tour snapshot", async () => {
    mockFetch.mockResolvedValue({ ok: true, json: async () => ({ tour: { id: "rt-1", summary: "Ready" }, steps: [], messages: [] }) })
    const sharedWrapper = wrapper()
    const first = renderHook(() => useReviewTour("rt-1"), { wrapper: sharedWrapper })
    await waitFor(() => expect(first.result.current.data?.tour.summary).toBe("Ready"))
    const second = renderHook(() => useReviewTour("rt-1"), { wrapper: sharedWrapper })
    await waitFor(() => expect(second.result.current.data?.tour.summary).toBe("Ready"))
    expect(mockFetch).toHaveBeenCalledTimes(1)
  })
})
