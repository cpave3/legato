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
  it("leads with the glance, then the main agent's track, stage and why; the reviewer's chip says what it is doing", () => {
    show(card({ glance: "working", stage: "implementing", stage_reason: "review pass 2", slot_agents: [{ slot: "reviewer", state: "working", stage: "reviewing" }] }))
    const strip = screen.getByTestId("agent-strip")
    expect(strip.textContent).toMatch(/^●Working·Implementing· review pass 2/)
    expect(within(strip).getByText("Implementing")).toBeTruthy()
    expect(strip.querySelector("[data-stage-track]")?.getAttribute("data-stage-track")).toBe("implementing")
    expect(within(strip).getByText("reviewer ● working")).toBeTruthy()
    expect(within(strip).queryByText(/Reviewing/)).toBeNull()
  })

  it("says it is your turn when the agent is idle with its work not finished", () => {
    show(card({ agent_state: "", glance: "your_turn", stage: "reviewing", stage_reason: "PR #214 open" }))
    const strip = screen.getByTestId("agent-strip")
    expect(strip.textContent).toMatch(/^◇Your turn·Reviewing· PR #214 open/)
  })

  it("shows a finished agent on a card still in Doing, with only why", () => {
    show(card({ agent_state: "", glance: "finished", stage: "finished", stage_reason: "PR #212 merged" }))
    const strip = screen.getByTestId("agent-strip")
    expect(strip.textContent).toBe("✓Finished· PR #212 merged1m")
    expect(strip.querySelector("[data-stage-track]")).toBeNull()
  })

  it("says a finished agent left a note", () => {
    show(card({ agent_state: "", glance: "finished", stage: "finished", stage_reason: "PR #212 merged", needs_you: { items: { done: 1 } } }))
    expect(screen.getByTestId("agent-strip").textContent).toContain("PR #212 merged · a note for you")
  })

  it("reads a finished stage as Finished from a server without glances", () => {
    show(card({ agent_state: "", stage: "finished" }))
    expect(within(screen.getByTestId("agent-strip")).getByText("Finished")).toBeTruthy()
  })

  it("names the reviewer by what it does: needs you, done", () => {
    show(card({ slot_agents: [{ slot: "reviewer", state: "idle", stage: "finished" }] }))
    expect(within(screen.getByTestId("agent-strip")).getByText("reviewer ✓ done")).toBeTruthy()
  })

  const away = (fields: Record<string, unknown>) => card({
    agent_active: false,
    away: {
      worker_instance_id: "srv-1", worker: "mini", handoff_id: "h", worker_task_id: "w", purpose: "make-change", kind: "new-branch",
      repo: "a/b", branch: "legato/t-1", left_column: "Doing", handed_off_at: "2026-11-05T09:00:00Z",
      state: "in_progress", activity: "working", view: "working", stage: "reviewing", stage_reason: "PR #7 open",
      ...fields,
    } as BoardCardType["away"],
  })

  it("shows the worker's stage on an away card", () => {
    show(away({}))
    const strip = screen.getByTestId("away-strip")
    expect(within(strip).getByText("Reviewing")).toBeTruthy()
  })

  it("adds only the track to an away card's review pass, named by its stage", () => {
    show(away({ view: "in_review", stage: "implementing", review: { pass: 2, phase: "waiting_for_author", waiting_on: "author_working" } }))
    const strip = screen.getByTestId("away-strip")
    expect(strip.textContent).toContain("Review pass 2 · author fixing")
    expect(within(strip).getByRole("img", { name: "Implementing" }).getAttribute("title")).toBe("Implementing")
    expect(within(strip).queryByText("Implementing")).toBeNull()
  })

  it("reads an idle away card like the others: where it is, and Finished", () => {
    show(away({ view: "idle", activity: "", stage: "finished" }))
    const strip = screen.getByTestId("away-strip")
    expect(strip.textContent).toMatch(/^⇢On mini·✓Finished/)
  })
})
