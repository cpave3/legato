import "@testing-library/jest-dom/vitest"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"
import { BoardCard } from "../BoardCard"
import { BoardMacrosContext } from "../PRMacros"
import type { BoardCard as BoardCardType, PRMetaView } from "../../../lib/board-types"
import type { Macro } from "../../../lib/macros"

afterEach(cleanup)

const pr = (n: number, pinned: boolean): PRMetaView => ({ repo: "acme/web", branch: "b", pr_number: n, pr_url: `https://github.com/acme/web/pull/${n}`, state: "OPEN", is_draft: false, review_decision: "", check_status: "", comment_count: 0, pinned, match_method: "", sync_outcome: "", sync_error: "" })
const card = { id: "T-1", title: "Card", status: "Doing", priority: "", issue_type: "", provider: "", has_warning: false, agent_active: false, agent_state: "", working_seconds: 0, waiting_seconds: 0, error_seconds: 0, workspace_name: "", workspace_color: "", pull_requests: [pr(4, false), pr(9, true)] } as unknown as BoardCardType
const macros: Macro[] = [
  { name: "Plan", keys: "p", applies_to: "task" },
  { name: "Review", keys: "r", applies_to: "pr" },
  { name: "Address feedback", keys: "a", applies_to: "pr" },
  { name: "tests", keys: "t" },
]

function show(run = vi.fn()) {
  render(<BoardMacrosContext.Provider value={{ list: { macros, suggested: { task: "Plan", my_pr: "Address feedback", others_pr: "Review" }, tiers: {} }, run }}>
    <BoardCard card={card} selected={false} column="Doing" showWorkspace={false} onClick={() => {}} />
  </BoardMacrosContext.Provider>)
  return run
}

it("starts the suggested macro on a pull request line with one press", () => {
  const run = show()
  fireEvent.click(screen.getByRole("button", { name: "Start Address feedback on #4" }))
  expect(run).toHaveBeenCalledWith(card, macros[2], { repo: "acme/web", pr_number: 4, url: "https://github.com/acme/web/pull/4" })
  fireEvent.click(screen.getByRole("button", { name: "Start Review on #9" }))
  expect(run).toHaveBeenLastCalledWith(card, macros[1], { repo: "acme/web", pr_number: 9, url: "https://github.com/acme/web/pull/9" })
})

it("lists a pull request's macros, the suggested first, and no task-only ones", () => {
  const run = show()
  fireEvent.click(screen.getByRole("button", { name: "Macros for #9" }))
  const items = screen.getAllByRole("menuitem").map(item => item.textContent)
  expect(items).toEqual(["Review · suggested", "Address feedback", "tests"])
  fireEvent.click(screen.getByRole("menuitem", { name: "tests" }))
  expect(run).toHaveBeenCalledWith(card, macros[3], expect.objectContaining({ pr_number: 9 }))
})

it("shows nothing on a pull request line without a board to start macros", () => {
  render(<BoardCard card={card} selected={false} column="Doing" showWorkspace={false} onClick={() => {}} />)
  expect(screen.queryByRole("button", { name: /Start .* on #4/ })).toBeNull()
})
