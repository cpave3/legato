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

function show(prs: PRMetaView[], run = vi.fn()) {
  render(<BoardMacrosContext.Provider value={{ list: { macros, suggested: { task: "Plan", my_pr: "Address feedback", others_pr: "Review" }, tiers: {} }, run }}>
    <BoardCard card={{ ...card, pull_requests: prs }} selected={false} column="Doing" showWorkspace={false} onClick={() => {}} />
  </BoardMacrosContext.Provider>)
  return run
}

it("offers Address feedback with one press on the operator's own PR with CI failing or changes requested", () => {
  const run = show([{ ...pr(4, false), check_status: "fail" }])
  fireEvent.click(screen.getByRole("button", { name: "Start Address feedback on #4" }))
  expect(run).toHaveBeenCalledWith(expect.objectContaining({ id: "T-1" }), macros[2], { repo: "acme/web", pr_number: 4, url: "https://github.com/acme/web/pull/4" })
  cleanup()
  show([{ ...pr(5, false), review_decision: "CHANGES_REQUESTED", check_status: "pass" }])
  expect(screen.getByRole("button", { name: "Start Address feedback on #5" })).toBeTruthy()
})

it("gives a stack one button, on its lowest layer that needs the operator", () => {
  show([{ ...pr(4, false), branch: "b4", base: "main", check_status: "fail" }, { ...pr(5, false), branch: "b5", base: "b4", check_status: "fail" }])
  expect(screen.getAllByRole("button", { name: /^Start / }).map(button => button.getAttribute("aria-label"))).toEqual(["Start Address feedback on #4"])
})

it("offers Review on someone else's PR awaiting review", () => {
  const run = show([pr(9, true)])
  fireEvent.click(screen.getByRole("button", { name: "Start Review on #9" }))
  expect(run).toHaveBeenCalledWith(expect.objectContaining({ id: "T-1" }), macros[1], { repo: "acme/web", pr_number: 9, url: "https://github.com/acme/web/pull/9" })
})

it.each([
  ["approved and green", { ...pr(1, false), check_status: "pass", review_decision: "APPROVED" }],
  ["awaiting review", { ...pr(2, false), check_status: "pass" }],
  ["a draft", { ...pr(3, false), is_draft: true, check_status: "fail" }],
  ["merged", { ...pr(4, false), state: "MERGED", check_status: "fail" }],
  ["someone else's, approved", { ...pr(5, true), review_decision: "APPROVED" }],
])("offers no button on a PR %s", (_, view) => {
  show([view])
  expect(screen.queryAllByRole("button", { name: /^Start / })).toEqual([])
})

it("shows no button without a board to start macros", () => {
  render(<BoardCard card={{ ...card, pull_requests: [{ ...pr(4, false), check_status: "fail" }] }} selected={false} column="Doing" showWorkspace={false} onClick={() => {}} />)
  expect(screen.queryByRole("button", { name: /Start .* on #4/ })).toBeNull()
})
