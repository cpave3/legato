import { describe, expect, it } from "vitest"
import { awayGroups, awayLook, withAwayLane } from "../away"
import type { AwayOn, BoardCard, BoardColumn, BoardWorker } from "../board-types"

function card(id: string, status: string, away?: Partial<AwayOn>): BoardCard {
  return {
    id, title: id, priority: "", issue_type: "", status, provider: "", has_warning: false, agent_active: false, agent_state: "",
    working_seconds: 0, waiting_seconds: 0, error_seconds: 0, workspace_id: null, workspace_name: "", workspace_color: "",
    ...(away ? { away: { worker_instance_id: "srv-1", worker: "vm-1", handoff_id: "ho-" + id, worker_task_id: "w" + id, purpose: "make-change", kind: "new-branch", repo: "acme/widget", branch: "legato/" + id, left_column: status, handed_off_at: "2026-11-05T09:00:00Z", ...away } } : {}),
  }
}

const vm1: BoardWorker = { instance_id: "srv-1", name: "vm-1", address: "https://vm-1.lan:3080" }
const vm2: BoardWorker = { instance_id: "srv-2", name: "vm-2", address: "https://vm-2.lan:3080" }

describe("withAwayLane", () => {
  const columns: BoardColumn[] = [
    { name: "Ready", cards: [card("a", "Ready"), card("b", "Ready", {})] },
    { name: "Doing", cards: [card("c", "Doing", { worker_instance_id: "srv-2", worker: "vm-2" })] },
  ]

  it("takes away tasks out of their columns into an Away lane at the end", () => {
    const lanes = withAwayLane(columns, [vm1, vm2])
    expect(lanes.map(c => [c.name, c.cards.map(x => x.id), Boolean(c.away)])).toEqual([
      ["Ready", ["a"], false],
      ["Doing", [], false],
      ["Away", ["b", "c"], true],
    ])
  })

  it("orders the lane by worker, as its groups are drawn", () => {
    const lanes = withAwayLane(columns, [vm2, vm1])
    expect(lanes[2].cards.map(c => c.id)).toEqual(["c", "b"])
  })

  it("shows the lane once a worker is paired, with nothing away", () => {
    const home = [{ name: "Ready", cards: [card("a", "Ready")] }]
    expect(withAwayLane(home, [vm1]).map(c => c.name)).toEqual(["Ready", "Away"])
    expect(withAwayLane(home, []).map(c => c.name)).toEqual(["Ready"])
  })
})

describe("awayGroups", () => {
  it("groups away tasks under each paired worker, and under the worker they name when it is gone", () => {
    const groups = awayGroups([vm1], [card("b", "Ready", {}), card("c", "Doing", { worker_instance_id: "srv-9", worker: "" })])
    expect(groups.map(g => [g.worker.name, g.cards.map(c => c.id)])).toEqual([["vm-1", ["b"]], ["srv-9", ["c"]]])
  })
})

describe("awayLook", () => {
  const away = card("b", "Ready", {}).away!
  it("names the worker's state as last heard", () => {
    expect(awayLook({ ...away, state: "pending" }).label).toBe("Starting")
    expect(awayLook({ ...away, state: "in_progress" }).label).toBe("Working")
    expect(awayLook({ ...away, state: "done" }).label).toBe("Done")
    expect(awayLook({ ...away, state: "failed", failure: "the clone failed" }).label).toBe("Failed: the clone failed")
    expect(awayLook({ ...away, state: "returning" }).label).toBe("Coming back")
    expect(awayLook(away).label).toBe("Handed off")
  })
})
