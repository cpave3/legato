import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { ReviewQueuePage } from "../ReviewQueuePage"

const mocks = vi.hoisted(() => ({
  bulkDeleteReviews: vi.fn(),
  refresh: vi.fn(),
}))

vi.mock("../../hooks/useServer", () => ({
  useServer: () => ({ baseUrl: "http://legato.test" }),
}))

vi.mock("../../lib/review", async (importOriginal) => ({
  ...await importOriginal<typeof import("../../lib/review")>(),
  bulkDeleteReviews: mocks.bulkDeleteReviews,
}))

vi.mock("../../hooks/useReview", () => ({
  useReviewQueue: () => ({
    data: [
      { task_id: "T-1", tour_id: "rt-task-1", name: "Review auth flow", title: "Review auth flow", status: "ready", summary: "Adds token refresh", unreviewed: 2, updated_at: "2026-07-17T09:00:00Z", activity_at: "2026-07-17T00:00:00Z" },
      { task_id: "T-2", tour_id: "rt-task-2", name: "Already checked", title: "Already checked", status: "ready", summary: "Ready to complete", unreviewed: 0, updated_at: "2026-07-17T09:00:00Z", activity_at: "2026-07-17T02:00:00Z" },
    ],
    loading: false,
    error: null,
    refresh: mocks.refresh,
  }),
}))

function renderPage() {
  render(<MemoryRouter><ReviewQueuePage /></MemoryRouter>)
}

describe("ReviewQueuePage", () => {
  afterEach(cleanup)

  beforeEach(() => {
    mocks.bulkDeleteReviews.mockReset()
    mocks.bulkDeleteReviews.mockResolvedValue(2)
    mocks.refresh.mockReset()
    mocks.refresh.mockResolvedValue(undefined)
  })

  it("shows queued work with progress and links into each tour", () => {
    renderPage()

    expect(screen.getByRole("heading", { name: "Review queue" })).toBeTruthy()
    expect(screen.getByText("Adds token refresh")).toBeTruthy()
    expect(screen.getByText("2 unreviewed")).toBeTruthy()
    expect(screen.getByText("Ready to complete")).toBeTruthy()
    expect(screen.getByRole("link", { name: /Review auth flow/ }).getAttribute("href")).toBe("/review/rt-task-1")
  })

  it("selects visible reviews and removes them after confirmation", async () => {
    renderPage()

    fireEvent.click(screen.getByRole("checkbox", { name: "Select Review auth flow" }))
    expect(screen.getByText("1 selected")).toBeTruthy()
    fireEvent.click(screen.getByRole("checkbox", { name: "Select all reviews" }))
    expect(screen.getByText("2 selected")).toBeTruthy()

    fireEvent.click(screen.getByRole("button", { name: "Remove selected" }))
    expect(screen.getByRole("dialog", { name: "Remove 2 reviews?" })).toBeTruthy()
    expect(screen.getByText(/Tasks and code will remain/)).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Remove 2 reviews" }))

    await waitFor(() => expect(mocks.bulkDeleteReviews).toHaveBeenCalledWith("http://legato.test", ["rt-task-2", "rt-task-1"]))
    expect(mocks.refresh).toHaveBeenCalledOnce()
  })

  it("keeps the selection and surfaces an error when removal fails", async () => {
    mocks.bulkDeleteReviews.mockRejectedValueOnce(new Error("review not found"))
    renderPage()

    fireEvent.click(screen.getByRole("checkbox", { name: "Select Review auth flow" }))
    fireEvent.click(screen.getByRole("button", { name: "Remove selected" }))
    fireEvent.click(screen.getByRole("button", { name: "Remove 1 review" }))

    expect((await screen.findByRole("alert")).textContent).toContain("review not found")
    expect((screen.getByRole("checkbox", { name: "Select Review auth flow" }) as HTMLInputElement).checked).toBe(true)
    expect(mocks.refresh).not.toHaveBeenCalled()
  })
})
