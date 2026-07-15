import { beforeEach, describe, expect, it, vi } from "vitest"
import { fetchReviewTour, setStepReviewed } from "../review"

const mockFetch = vi.fn()
vi.stubGlobal("fetch", mockFetch)

beforeEach(() => {
  mockFetch.mockReset()
})

describe("review API", () => {
  it("fetches a task review from the active server", async () => {
    const tour = { tour: { task_id: "TASK / 1", status: "ready", summary: "Summary" }, steps: [], messages: [] }
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => tour })

    await expect(fetchReviewTour("https://legato.example", "TASK / 1")).resolves.toEqual(tour)
    expect(mockFetch).toHaveBeenCalledWith(
      "https://legato.example/api/tasks/TASK%20%2F%201/review",
      expect.objectContaining({ headers: {} }),
    )
  })

  it("sends reviewed state and exposes a server failure", async () => {
    mockFetch.mockResolvedValueOnce({ ok: false, statusText: "Conflict", text: async () => "step changed" })

    await expect(setStepReviewed("", "T-1", "step/1", true)).rejects.toThrow("step changed")
    expect(mockFetch).toHaveBeenCalledWith("/api/tasks/T-1/review/steps/step%2F1/reviewed", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ reviewed: true }),
    })
  })
})
