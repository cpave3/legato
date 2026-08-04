import { beforeEach, describe, expect, it, vi } from "vitest"
import { fetchReviewQueue, fetchReviewTour, setStepReviewed } from "../review"

const mockFetch = vi.fn()
vi.stubGlobal("fetch", mockFetch)

beforeEach(() => {
  mockFetch.mockReset()
})

describe("review API", () => {
  it("fetches a task review from the active server", async () => {
    const tour = { tour: { id: "rt-task-1", task_id: "TASK / 1", name: "Auth review", status: "ready", summary: "Summary" }, steps: [], messages: [] }
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => tour })

    await expect(fetchReviewTour("https://legato.example", "rt-task-1")).resolves.toEqual(tour)
    expect(mockFetch).toHaveBeenCalledWith(
      "https://legato.example/api/review/tours/rt-task-1",
      expect.objectContaining({ headers: {} }),
    )
  })

  it("shares an in-flight review queue request across callers", async () => {
    let resolveRequest!: (value: unknown) => void
    mockFetch.mockImplementationOnce(() => new Promise((resolve) => { resolveRequest = resolve }))

    const first = fetchReviewQueue("https://legato.example")
    const second = fetchReviewQueue("https://legato.example")
    expect(mockFetch).toHaveBeenCalledTimes(1)

    resolveRequest({ ok: true, json: async () => [{ tour_id: "rt-task-1" }] })
    await expect(Promise.all([first, second])).resolves.toEqual([
      [{ tour_id: "rt-task-1" }],
      [{ tour_id: "rt-task-1" }],
    ])
  })

  it("sends reviewed state and exposes a server failure", async () => {
    mockFetch.mockResolvedValueOnce({ ok: false, statusText: "Conflict", text: async () => "step changed" })

    await expect(setStepReviewed("", "rt-1", "step/1", true)).rejects.toThrow("step changed")
    expect(mockFetch).toHaveBeenCalledWith("/api/review/tours/rt-1/steps/step%2F1/reviewed", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ reviewed: true }),
    })
  })
})
