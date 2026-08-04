import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { MobileBoardList } from "../MobileBoardList"

const columns = [
  { name: "Todo", cards: [{ id: "task-1", title: "Mobile task", status: "Todo", priority: "High", issue_type: "Task", provider: "", has_warning: false, agent_active: true, agent_state: "working", working_seconds: 1, waiting_seconds: 0, error_seconds: 0, workspace_name: "Personal", workspace_color: "#fff", pr_check_status: "", pr_review_decision: "", pr_comment_count: 0, pr_is_draft: false, pr_number: 0 }] },
  { name: "Done", cards: [] },
]

afterEach(() => {
  cleanup()
  localStorage?.clear()
})

describe("MobileBoardList", () => {
  it("renders grouped tasks with counts and a touch action", () => {
    render(<MobileBoardList columns={columns} showWorkspace onCardClick={() => undefined} />)
    expect(screen.getByRole("heading", { name: "Todo" })).toBeTruthy()
    expect(screen.getByText("Mobile task")).toBeTruthy()
    expect(screen.getByRole("button", { name: "Open actions for Mobile task" })).toBeTruthy()
  })

  it("persists collapsed groups", () => {
    const { unmount } = render(<MobileBoardList columns={columns} showWorkspace onCardClick={() => undefined} />)
    fireEvent.click(screen.getByRole("button", { name: /Todo/ }))
    expect(screen.queryByText("Mobile task")).toBeNull()
    unmount()
    render(<MobileBoardList columns={columns} showWorkspace onCardClick={() => undefined} />)
    expect(screen.queryByText("Mobile task")).toBeNull()
  })
})
