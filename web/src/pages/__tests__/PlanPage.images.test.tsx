import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { afterEach, describe, expect, it, vi } from "vitest"
import { PlanPage } from "../PlanPage"

const BOARD = "/plan-images/pl-1/" + "a".repeat(64) + ".png"
const AFTER = "/plan-images/pl-1/" + "b".repeat(64) + ".png"
const MOCKUP = "/plan-images/pl-1/" + "c".repeat(64) + ".png"
const at = (url: string) => new URL(url, window.location.origin).href

const planData = {
  plan: { id: "pl-1", task_id: "T-1", name: "", title: "Header", summary: "", status: "proposed", latest_revision: 1, created_at: "", updated_at: "" },
  revision: { id: "rev-1", plan_id: "pl-1", revision: 1, manifest_json: "{}", created_at: "",
    markdown: "# Header\n\nThe board today:\n\n![The board today](assets/board.png)\n\nAnd after: ![After](assets/after.png)\n\n![An old shot](assets/gone.png)\n" },
  images: [{ name: "assets/board.png", url: BOARD }, { name: "assets/after.png", url: AFTER }],
  questions: [], responses: [], comments: [],
  messages: [{ id: 7, thread_id: "th-1", kind: "answer", author: "agent", body: "Like this:\n\n![Mockup](mockup.png)", created_at: "" }],
  message_images: { 7: [{ name: "mockup.png", url: MOCKUP }] },
}

vi.mock("mermaid", () => ({ default: { initialize: vi.fn(), render: vi.fn() } }))
vi.mock("@wiremark/core", () => ({ render: vi.fn() }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useRunningAgent", () => ({ useRunningAgent: () => ({ agent: null, loading: false }) }))
vi.mock("../../hooks/usePlan", () => ({ usePlan: () => ({ data: planData, loading: false, error: null, refresh: vi.fn() }) }))

afterEach(cleanup)

function renderPage() {
  return render(<MemoryRouter initialEntries={["/plans/pl-1"]}><Routes><Route path="/plans/:planId" element={<PlanPage />} /></Routes></MemoryRouter>)
}

describe("PlanPage with image files", () => {
  it("draws the images the plan names from Legato's copies", () => {
    renderPage()

    expect((screen.getByAltText("The board today") as HTMLImageElement).src).toBe(at(BOARD))
    expect((screen.getByAltText("After") as HTMLImageElement).src).toBe(at(AFTER))
  })

  it("opens an image full size, with the plan's other images to step through", () => {
    renderPage()

    fireEvent.click(screen.getByRole("button", { name: "Show The board today full size" }))

    const dialog = screen.getByRole("dialog", { name: "The board today, full size" })
    expect((within(dialog).getByAltText("The board today") as HTMLImageElement).src).toBe(at(BOARD))
    // The answer's image is another document's: the plan has two.
    expect(within(dialog).getByText("1 of 2")).toBeTruthy()
  })

  it("lets the person comment on an image as on any block, and names it by its alt text", () => {
    renderPage()

    const control = screen.getByRole("button", { name: "Comment on image The board today" })
    fireEvent.click(control)

    expect(control.getAttribute("aria-pressed")).toBe("true")
    expect(screen.getByLabelText("Comment on selected block")).toBeTruthy()
  })

  it("names an image whose copy is not stored by its alt text", () => {
    renderPage()

    expect(screen.queryByAltText("An old shot")).toBeNull()
    expect(screen.getByText(/An old shot/).textContent).toContain("not stored")
  })

  it("draws the images of an answer in the conversation", () => {
    renderPage()

    expect((screen.getByAltText("Mockup") as HTMLImageElement).src).toBe(at(MOCKUP))
  })
})
