import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { QueryClientProvider } from "@tanstack/react-query"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { createQueryClient } from "../../lib/query"
import { ReviewTourPage } from "../ReviewTourPage"

const { mockFetch } = vi.hoisted(() => ({ mockFetch: vi.fn() }))
vi.stubGlobal("fetch", mockFetch)

const BEFORE = "/review-images/rt-task-1/" + "a".repeat(64) + ".png"
const AFTER = "/review-images/rt-task-1/" + "b".repeat(64) + ".png"
const NOW = "/review-images/rt-task-1/" + "c".repeat(64) + ".png"
const BOARD = "/plan-images/pl-1/" + "d".repeat(64) + ".png"
const at = (url: string) => new URL(url, window.location.origin).href

vi.mock("mermaid", () => ({ default: { initialize: vi.fn(), render: vi.fn() } }))
vi.mock("@wiremark/core", () => ({ render: vi.fn() }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useVoiceEnabled", () => ({ useVoiceEnabled: () => false }))
vi.mock("../../hooks/useRunningAgent", () => ({ useRunningAgent: () => ({ agent: null, loading: false }) }))
vi.mock("../../hooks/useReview", () => {
  const steps = [
    { id: "S-1", task_id: "T-1", kind: "chapter", commit_sha: "", title: "The new header", narration: "The header is **sticky** now.\n\n![Before](before.png)\n\n![After](/tmp/shots/after.png)", risk: "medium", reviewed_at: null, orphaned_at: null },
  ]
  const messages = [{ id: 4, step_id: "S-1", author: "agent", kind: "answer", body: "Like this:\n\n![Now](now.png)" }]
  return {
    useReviewTour: () => ({
      data: {
        tour: { id: "rt-task-1", task_id: "T-1", name: "Header review", status: "ready", base_sha: "base", head_sha: "head", summary: "" },
        passes: [{
          pass: { id: "rp-1", number: 1, status: "ready", summary: "", guidance: "" },
          captured_plan: { plan_id: "pl-1", revision_id: "pr-1", revision: 1, title: "Header plan", markdown: "## Goal\n\n![The board today](assets/board.png)" },
          steps, messages, hunk_notes: [], findings: [], plan_requests: [],
        }],
        steps, messages, hunk_notes: [],
        step_images: { "S-1": [{ name: "before.png", url: BEFORE }, { name: "/tmp/shots/after.png", url: AFTER }] },
        message_images: { 4: [{ name: "now.png", url: NOW }] },
        plan_images: { "rp-1": [{ name: "assets/board.png", url: BOARD }] },
      },
      loading: false, error: null, refresh: vi.fn(),
    }),
  }
})

afterEach(cleanup)
beforeEach(() => {
  mockFetch.mockReset()
  mockFetch.mockResolvedValue({ ok: true, json: async () => [] })
})

function renderPage() {
  return render(
    <QueryClientProvider client={createQueryClient()}>
      <MemoryRouter initialEntries={["/review/rt-task-1"]}><Routes><Route path="/review/:tourId" element={<ReviewTourPage />} /></Routes></MemoryRouter>
    </QueryClientProvider>,
  )
}

describe("ReviewTourPage with image files", () => {
  it("draws a chapter's narration as Markdown, with its images from Legato's copies", () => {
    renderPage()

    expect(screen.getByText("sticky").tagName).toBe("STRONG")
    expect((screen.getByAltText("Before") as HTMLImageElement).src).toBe(at(BEFORE))
    expect((screen.getByAltText("After") as HTMLImageElement).src).toBe(at(AFTER))
  })

  it("opens a narration image full size, with the chapter's other image to step through", () => {
    renderPage()

    fireEvent.click(screen.getByRole("button", { name: "Show After full size" }))

    const dialog = screen.getByRole("dialog", { name: "After, full size" })
    expect((within(dialog).getByAltText("After") as HTMLImageElement).src).toBe(at(AFTER))
    expect(within(dialog).getByText("2 of 2")).toBeTruthy()
  })

  it("draws the images of an answer and of the captured plan", () => {
    renderPage()

    expect((screen.getByAltText("Now") as HTMLImageElement).src).toBe(at(NOW))
    expect((screen.getByAltText("The board today") as HTMLImageElement).src).toBe(at(BOARD))
  })
})
