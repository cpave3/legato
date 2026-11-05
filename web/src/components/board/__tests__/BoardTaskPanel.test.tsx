import { afterEach, describe, expect, it, vi } from "vitest"
import { cleanup, render, screen, within } from "@testing-library/react"
import { BoardTaskPanel } from "../BoardTaskPanel"
import type { BoardCard, PRMetaView } from "../../../lib/board-types"
vi.mock("../TaskAgentAction", () => ({ TaskAgentAction: () => null }))

afterEach(cleanup)

function pr(number: number, more: Partial<PRMetaView> = {}): PRMetaView {
  return { repo: "acme/widget", branch: `stack-${number}`, pr_number: number, pr_url: `https://github.com/acme/widget/pull/${number}`, state: "OPEN", is_draft: false, review_decision: "", check_status: "", comment_count: 0, pinned: false, match_method: "branch", sync_outcome: "linked", sync_error: "", ...more }
}

const card: BoardCard = {
  id: "T-1", title: "Stacked", priority: "", issue_type: "", status: "Doing", provider: "", has_warning: false,
  agent_active: false, agent_state: "", working_seconds: 0, waiting_seconds: 0, error_seconds: 0,
  workspace_id: null, workspace_name: "", workspace_color: "",
  pull_requests: [pr(11, { check_status: "fail" }), pr(12, { is_draft: true })],
}

describe("BoardTaskPanel", () => {
  it("lists every pull request of the task in stack order", () => {
    render(<BoardTaskPanel card={card} column="Doing" detail={null} onClose={() => {}} onOpenDetails={() => {}} onMove={() => {}} onOpenURL={() => {}} />)
    const list = screen.getByRole("list", { name: "Pull requests" })
    const links = within(list).getAllByRole("link")
    expect(links.map(link => link.textContent)).toEqual(["#11", "#12"])
    expect(links[1].getAttribute("href")).toBe("https://github.com/acme/widget/pull/12")
    expect(within(list).getByText("draft")).toBeTruthy()
  })
})
