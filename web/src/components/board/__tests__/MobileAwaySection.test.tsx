import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { MobileAwaySection } from "../MobileAwaySection"
import type { AwayOn, BoardCard, BoardWorker } from "../../../lib/board-types"

afterEach(cleanup)

function away(id: string, title: string, more: Partial<AwayOn>): BoardCard {
  return {
    id, title, priority: "", issue_type: "", status: "Doing", provider: "burnlite", remote_id: "LEG-390", has_warning: false, agent_active: false, agent_state: "",
    working_seconds: 0, waiting_seconds: 0, error_seconds: 0, workspace_id: null, workspace_name: "", workspace_color: "",
    away: {
      worker_instance_id: "srv-1", worker: "vm-1", handoff_id: "ho-" + id, worker_task_id: "w-" + id, purpose: "make-change", kind: "new-branch",
      repo: "acme/widget", branch: "legato/" + id, left_column: "Doing", handed_off_at: "2026-11-05T09:00:00Z", state: "in_progress", ...more,
    },
  }
}

const vm1: BoardWorker = { instance_id: "srv-1", name: "vm-1", address: "https://vm-1.lan:3080" }
const stuck = { pass: 3, phase: "stuck", reason: "same finding three passes", blocking_open: 1, comments_open: 0, checks_open: 0, skipped_comments: 0 }

function open(cards: BoardCard[]) {
  render(<MemoryRouter><MobileAwaySection workers={[vm1]} cards={cards} onRowClick={() => {}} baseUrl="" /></MemoryRouter>)
  fireEvent.click(screen.getByRole("button", { name: /^vm-1 · / }))
  return screen.getByRole("group", { name: "vm-1" })
}

describe("MobileAwaySection", () => {
  it("draws an away task as the board's card: the worker in its header, a stuck review and its answers", () => {
    const group = open([away("T-1", "Sync claims", { view: "needs_you", review: stuck })])
    const card = within(group).getByText("Sync claims").closest("[data-card]") as HTMLElement
    expect(within(card).getByTestId("card-source").textContent).toContain("Burnlite · LEG-390 → vm-1")
    expect(within(card).getByTestId("away-strip").textContent).toContain("Review stuck after pass 3: same finding three passes")
    for (const name of ["Return as is", "Run another pass", "Message the author"]) expect(within(card).getByRole("button", { name })).toBeTruthy()
    expect(within(card).queryByRole("button", { name: "Recall" })).toBeNull()
  })

  it("offers only Take back while the worker is unreachable", () => {
    const group = open([away("T-1", "Sync claims", { view: "unreachable", worker_unreachable_since: "2026-11-05T21:40:00Z", review: stuck })])
    const card = within(group).getByText("Sync claims").closest("[data-card]") as HTMLElement
    expect(within(card).getByTestId("away-strip").textContent).toMatch(/vm-1 unreachable since /)
    const buttons = within(card).getAllByRole("button").map(button => button.textContent).filter(text => text && !text.includes("Sync claims"))
    expect(buttons).toEqual(["Take back"])
  })

  it("offers Return once the review is approved", () => {
    const group = open([away("T-1", "Sync claims", { state: "done", view: "done", review: { ...stuck, phase: "clean", reason: undefined } })])
    const card = within(group).getByText("Sync claims").closest("[data-card]") as HTMLElement
    expect(within(card).getByTestId("away-strip").textContent).toContain("Review done · approved")
    expect(within(card).getByRole("button", { name: "Return" })).toBeTruthy()
  })
})
