import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
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
      hunk_notes: [
        { id: "N-1", task_id: "T-1", step_id: "S-1", file_path: "src/auth.ts", hunk_anchor: "missing-anchor", body: "This note lost its hunk.", created_at: "2026-01-01" },
        { id: "N-2", task_id: "T-1", step_id: "S-2", file_path: "src/other.ts", hunk_anchor: "other-anchor", body: "Other step note.", created_at: "2026-01-01" },
      ],
    },
    loading: false,
    error: null,
    refresh: mockRefresh,
  }),
}))

afterEach(cleanup)

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

  it("asks for confirmation before deleting a review", async () => {
    renderPage()
    await waitFor(() => expect(mockFetch).toHaveBeenCalledTimes(1))

    fireEvent.click(screen.getByRole("button", { name: "Delete review" }))

    expect(screen.getByRole("dialog", { name: "Delete review" })).toBeTruthy()
    expect(mockFetch.mock.calls.some(([, options]) => options?.method === "DELETE")).toBe(false)
  })

  it("cancels review deletion without sending a request", async () => {
    renderPage()
    await waitFor(() => expect(mockFetch).toHaveBeenCalledTimes(1))

    fireEvent.click(screen.getByRole("button", { name: "Delete review" }))
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))

    expect(screen.queryByRole("dialog", { name: "Delete review" })).toBeNull()
    expect(mockFetch.mock.calls.some(([, options]) => options?.method === "DELETE")).toBe(false)
  })

  it("deletes a confirmed review and returns to the queue", async () => {
    renderPage()
    await waitFor(() => expect(mockFetch).toHaveBeenCalledTimes(1))
    mockFetch.mockResolvedValueOnce({ ok: true })

    fireEvent.click(screen.getByRole("button", { name: "Delete review" }))
    fireEvent.click(within(screen.getByRole("dialog", { name: "Delete review" })).getByRole("button", { name: "Delete review" }))

    await waitFor(() => expect(screen.getByText("Queue destination")).toBeTruthy())
    expect(mockFetch).toHaveBeenCalledWith("/api/tasks/T-1/review", expect.objectContaining({ method: "DELETE" }))
  })

  it("shows an error and stays on the tour when deletion fails", async () => {
    renderPage()
    await waitFor(() => expect(mockFetch).toHaveBeenCalledTimes(1))
    mockFetch.mockResolvedValueOnce({ ok: false, statusText: "Conflict", text: async () => "review is still capturing" })

    fireEvent.click(screen.getByRole("button", { name: "Delete review" }))
    fireEvent.click(within(screen.getByRole("dialog", { name: "Delete review" })).getByRole("button", { name: "Delete review" }))

    expect((await screen.findByText("review is still capturing")).getAttribute("role")).toBe("alert")
    expect(screen.getByRole("heading", { name: "Review the authentication changes" })).toBeTruthy()
  })

  it("shows unmatched hunk notes for only the selected step outside Q&A", async () => {
    renderPage()

    const warning = await screen.findByRole("alert", { name: "Unmatched hunk notes" })
    expect(warning.textContent).toContain("This note lost its hunk.")
    expect(screen.queryByText("Other step note.")).toBeNull()
    expect(screen.getByText("The refresh is single-flight.").closest("aside")?.textContent).toContain("Questions & answers")
  })

  it("loads the selected step's diff", async () => {
    renderPage()
    await waitFor(() => expect(mockFetch).toHaveBeenCalledTimes(1))
    fireEvent.click(screen.getByRole("button", { name: /Uncommitted changes/ }))
    await waitFor(() => expect(mockFetch).toHaveBeenCalledWith("/api/tasks/T-1/review/steps/S-2/diff", expect.anything()))
  })
})
