import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { describe, expect, it, vi } from "vitest"
import { ReviewQueuePage } from "../ReviewQueuePage"

vi.mock("../../hooks/useReview", () => ({
  useReviewQueue: () => ({
    data: [
      { task_id: "T-1", tour_id: "rt-task-1", name: "Review auth flow", title: "Review auth flow", status: "ready", summary: "Adds token refresh", unreviewed: 2 },
      { task_id: "T-2", tour_id: "rt-task-2", name: "Already checked", title: "Already checked", status: "ready", summary: "Ready to complete", unreviewed: 0 },
    ],
    loading: false,
    error: null,
    refresh: vi.fn(),
  }),
}))

describe("ReviewQueuePage", () => {
  it("shows queued work with progress and links into each tour", () => {
    render(<MemoryRouter><ReviewQueuePage /></MemoryRouter>)

    expect(screen.getByRole("heading", { name: "Review queue" })).toBeTruthy()
    expect(screen.getByText("Adds token refresh")).toBeTruthy()
    expect(screen.getByText("2 unreviewed")).toBeTruthy()
    expect(screen.getByText("Ready to complete")).toBeTruthy()
    expect(screen.getByRole("link", { name: /Review auth flow/ }).getAttribute("href")).toBe("/review/rt-task-1")
  })
})
