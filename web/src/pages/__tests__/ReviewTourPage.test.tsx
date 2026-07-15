import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { ReviewTourPage } from "../ReviewTourPage"

const mockFetch = vi.fn()
const mockRefresh = vi.fn()
vi.stubGlobal("fetch", mockFetch)

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useReview", () => ({
  useReviewTour: () => ({
    data: {
      tour: { task_id: "T-1", status: "ready", summary: "Review the authentication changes" },
      steps: [
        { id: "S-1", task_id: "T-1", kind: "commit", commit_sha: "abcdef123456", title: "Refresh expired tokens", narration: "Retries once after refreshing.", risk: "medium", reviewed_at: null, orphaned_at: null },
        { id: "S-2", task_id: "T-1", kind: "dirty", commit_sha: "", title: "Uncommitted changes", narration: "", risk: "", reviewed_at: "2026-01-01", orphaned_at: null },
      ],
      messages: [
        { id: 1, step_id: "S-1", author: "agent", kind: "answer", body: "The refresh is single-flight." },
      ],
    },
    loading: false,
    error: null,
    refresh: mockRefresh,
  }),
}))

beforeEach(() => {
  mockFetch.mockReset()
  mockRefresh.mockReset()
  mockFetch.mockResolvedValue({ ok: true, json: async () => [] })
})

function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/review/T-1"]}>
      <Routes>
        <Route path="/review/:taskId" element={<ReviewTourPage />} />
        <Route path="/review" element={<div>Queue destination</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

describe("ReviewTourPage", () => {
  it("lets a reviewer inspect, mark, question, and complete a tour", async () => {
    renderPage()

    expect(screen.getByRole("heading", { name: "Review the authentication changes" })).toBeTruthy()
    expect(screen.getByText("1/2 reviewed")).toBeTruthy()
    await waitFor(() => expect(mockFetch).toHaveBeenCalledWith("/api/tasks/T-1/review/steps/S-1/diff", expect.anything()))

    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => ({ status: "ok" }) })
    fireEvent.click(screen.getByRole("button", { name: "Mark step reviewed" }))
    await waitFor(() => expect(mockFetch).toHaveBeenCalledWith(
      "/api/tasks/T-1/review/steps/S-1/reviewed",
      expect.objectContaining({ method: "POST", body: JSON.stringify({ reviewed: true }) }),
    ))

    fireEvent.change(screen.getByLabelText("Question for agent"), { target: { value: "What prevents duplicate refreshes?" } })
    fireEvent.click(screen.getByRole("button", { name: "Ask agent" }))
    await waitFor(() => expect(mockFetch).toHaveBeenCalledWith(
      "/api/tasks/T-1/review/steps/S-1/question",
      expect.objectContaining({ body: JSON.stringify({ text: "What prevents duplicate refreshes?" }) }),
    ))

    fireEvent.click(screen.getByRole("button", { name: "Complete review" }))
    await waitFor(() => expect(screen.getByText("Queue destination")).toBeTruthy())
    expect(mockFetch).toHaveBeenCalledWith("/api/tasks/T-1/review/complete", expect.objectContaining({ method: "POST" }))
  })

  it("loads the selected step's diff", async () => {
    renderPage()
    await waitFor(() => expect(mockFetch).toHaveBeenCalledTimes(1))
    fireEvent.click(screen.getByRole("button", { name: /Uncommitted changes/ }))
    await waitFor(() => expect(mockFetch).toHaveBeenCalledWith("/api/tasks/T-1/review/steps/S-2/diff", expect.anything()))
  })
})
