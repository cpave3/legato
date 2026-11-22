import { afterEach, describe, expect, it } from "vitest"
import { cleanup, render, screen, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { BoardCard } from "../BoardCard"
import type { BoardCard as BoardCardType } from "../../../lib/board-types"

afterEach(cleanup)

const card = (overrides: Partial<BoardCardType>): BoardCardType => ({
  id: "T-1", title: "Fix it", priority: "", issue_type: "", status: "Doing", provider: "",
  has_warning: false, agent_active: true, agent_state: "working", agent_kind: "claude-code",
  working_seconds: 60, waiting_seconds: 0, error_seconds: 0, workspace_id: null, workspace_name: "", workspace_color: "",
  ...overrides,
})

function show(c: BoardCardType) {
  render(<MemoryRouter><BoardCard card={c} selected={false} column="Doing" showWorkspace={false} onClick={() => {}} /></MemoryRouter>)
}

describe("a card's agent stage", () => {
  it("names the main agent's stage on its strip, and the reviewer's on its chip", () => {
    show(card({ stage: "implementing", stage_reason: "6 files changed", slot_agents: [{ slot: "reviewer", state: "working", stage: "reviewing" }] }))
    const strip = screen.getByTestId("agent-strip")
    expect(strip.textContent).toContain("Running")
    expect(within(strip).getByRole("img", { name: "Implementing" })).toBeTruthy()
    expect(within(strip).getByText("Implementing")).toBeTruthy()
    expect(within(strip).getByText("reviewer · Reviewing")).toBeTruthy()
  })

  it("shows a finished agent on a card still in Doing", () => {
    show(card({ agent_state: "", stage: "finished", stage_reason: "PR #212 merged" }))
    expect(within(screen.getByTestId("agent-strip")).getByText("Finished")).toBeTruthy()
  })

  it("shows the worker's stage on an away card", () => {
    show(card({
      agent_active: false,
      away: {
        worker_instance_id: "srv-1", worker: "vm-1", handoff_id: "h", worker_task_id: "w", purpose: "make-change", kind: "new-branch",
        repo: "a/b", branch: "legato/t-1", left_column: "Doing", handed_off_at: "2026-11-05T09:00:00Z",
        state: "in_progress", activity: "working", view: "working", stage: "reviewing", stage_reason: "PR #7 open",
      },
    }))
    const strip = screen.getByTestId("away-strip")
    expect(within(strip).getByText("Reviewing")).toBeTruthy()
  })
})
