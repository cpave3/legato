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

  it("requests capturing tours only when explicitly enabled", async () => {
    mockFetch.mockResolvedValue({ ok: true, json: async () => [] })

    await fetchReviewQueue("https://legato.example", true)

    expect(mockFetch).toHaveBeenCalledWith(
      "https://legato.example/api/review/queue?include_capturing=true",
      expect.objectContaining({ headers: {} }),
    )
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
