import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { AwayLane, WorkerReach } from "../AwayLane"
import type { AwayOn, BoardCard, BoardWorker } from "../../../lib/board-types"

afterEach(cleanup)

function away(id: string, title: string, more: Partial<AwayOn> = {}): BoardCard {
  return {
    id, title, priority: "", issue_type: "", status: "Doing", provider: "", has_warning: false, agent_active: false, agent_state: "",
    working_seconds: 0, waiting_seconds: 0, error_seconds: 0, workspace_id: null, workspace_name: "", workspace_color: "",
    away: {
      worker_instance_id: "srv-1", worker: "vm-1", handoff_id: "ho-" + id, worker_task_id: "w-" + id, purpose: "make-change", kind: "new-branch",
      repo: "acme/widget", branch: "legato/" + id, left_column: "Doing", handed_off_at: "2026-11-05T09:00:00Z", state: "pending",
      url: "https://vm-1.lan:3080/board?highlight_task=w-" + id, ...more,
    },
  }
}

const vm1: BoardWorker = { instance_id: "srv-1", name: "vm-1", address: "https://vm-1.lan:3080" }
const vm2: BoardWorker = { instance_id: "srv-2", name: "vm-2", address: "https://vm-2.lan:3080" }

describe("AwayLane", () => {
  it("groups away tasks by worker, each under a header that opens the worker", () => {
    const cards = [away("T-1", "Fix login"), away("T-2", "Find the leak", { left_column: "Ready", state: "failed", failure: "the clone failed" })]
    render(<AwayLane workers={[vm1, vm2]} cards={cards} selectedIndex={-1} isActive={false} onRowClick={() => {}} />)
    const lane = screen.getByRole("region", { name: "Away" })
    expect(within(lane).getByText("2")).toBeTruthy()

    const group = within(lane).getByRole("group", { name: "vm-1" })
    expect(within(group).getByRole("link", { name: "Open on vm-1" }).getAttribute("href")).toBe("https://vm-1.lan:3080")

    const row = within(group).getByRole("button", { name: /Fix login/ })
    expect(row.textContent).toContain("T-1")
    expect(row.textContent).toContain("from Doing")
    expect(within(row).getByTestId("away-state").textContent).toContain("Starting")
    expect(row.querySelector("time")?.getAttribute("datetime")).toBe("2026-11-05T09:00:00Z")

    const failed = within(group).getByRole("button", { name: /Find the leak/ })
    expect(failed.textContent).toContain("from Ready")
    expect(within(failed).getByTestId("away-state").textContent).toContain("Failed: the clone failed")

    // A worker with nothing away still has its group.
    const empty = within(lane).getByRole("group", { name: "vm-2" })
    expect(within(empty).getByText(/Nothing away on vm-2/)).toBeTruthy()
  })

  it("selects a row as the board selects a card", () => {
    const onRowClick = vi.fn()
    const cards = [away("T-1", "Fix login"), away("T-2", "Find the leak")]
    render(<AwayLane workers={[vm1]} cards={cards} selectedIndex={1} isActive onRowClick={onRowClick} />)
    expect(screen.getByRole("button", { name: /Find the leak/ }).getAttribute("data-selected")).toBe("true")
    fireEvent.click(screen.getByRole("button", { name: /Fix login/ }))
    expect(onRowClick).toHaveBeenCalledWith(0)
  })

  it("updates a row's pill and its group's reach as the worker's rows arrive", () => {
    const now = new Date("2026-11-05T22:00:00Z")
    const reach = (worker: BoardWorker) => <WorkerReach worker={worker} now={now} />
    const working = [away("T-1", "Fix login", { state: "in_progress", activity: "working", view: "working" })]
    const { rerender } = render(<AwayLane workers={[{ ...vm1, last_pull_at: "2026-11-05T21:58:00Z" }]} cards={working} selectedIndex={-1} isActive={false} onRowClick={() => {}} groupExtras={reach} />)
    const group = screen.getByRole("group", { name: "vm-1" })
    expect(within(group).getByTestId("worker-reach").textContent).toBe("pulled 2 min ago")
    expect(within(group).getByTestId("away-state").textContent).toContain("Working")

    const needsYou = [away("T-1", "Fix login", { state: "in_progress", activity: "waiting", view: "needs_you" })]
    rerender(<AwayLane workers={[{ ...vm1, last_pull_at: "2026-11-05T21:59:00Z" }]} cards={needsYou} selectedIndex={-1} isActive={false} onRowClick={() => {}} groupExtras={reach} />)
    expect(within(group).getByTestId("away-state").textContent).toContain("Needs you")

    const down = [away("T-1", "Fix login", { state: "in_progress", view: "unreachable", worker_unreachable_since: "2026-11-05T21:40:00Z" })]
    rerender(<AwayLane workers={[{ ...vm1, unreachable_since: "2026-11-05T21:40:00Z" }]} cards={down} selectedIndex={-1} isActive={false} onRowClick={() => {}} groupExtras={reach} />)
    expect(within(group).getByTestId("worker-reach").textContent).toMatch(/^unreachable since /)
    expect(within(group).getByTestId("away-state").textContent).toContain("Unreachable")

    const lost = [away("T-1", "Fix login", { state: "in_progress", view: "lost", lost: "vm-1 no longer has this task" })]
    rerender(<AwayLane workers={[vm1]} cards={lost} selectedIndex={-1} isActive={false} onRowClick={() => {}} groupExtras={reach} />)
    const pill = within(group).getByTestId("away-state")
    expect(pill.textContent).toContain("Lost on worker")
    expect(pill.getAttribute("title")).toBe("vm-1 no longer has this task")
  })
})
