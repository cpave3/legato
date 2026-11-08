import { describe, expect, it } from "vitest"
import { awayGroups, awayLook, withAwayLane, workerReach } from "../away"
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

describe("awayLook, live", () => {
  const away = card("b", "Ready", {}).away!
  it("shows the view the main has of the task, its own view of the worker first", () => {
    const label = (more: Partial<AwayOn>) => awayLook({ ...away, ...more }).label
    expect(label({ state: "pending", view: "starting" })).toBe("Starting")
    expect(label({ state: "in_progress", activity: "working", view: "working" })).toBe("Working")
    expect(label({ state: "in_progress", activity: "waiting", view: "needs_you" })).toBe("Needs you")
    expect(label({ state: "in_progress", activity: "idle", view: "idle" })).toBe("Idle")
    expect(label({ state: "in_progress", activity: "ended", view: "ended" })).toBe("Agent ended")
    expect(label({ state: "done", view: "done" })).toBe("Done")
    expect(label({ state: "failed", failure: "branch not on GitHub", view: "failed" })).toBe("Failed: branch not on GitHub")
    expect(label({ state: "returning", view: "coming_back" })).toBe("Coming back")
    expect(label({ state: "done", view: "unreachable", worker_unreachable_since: "2026-11-05T21:40:00Z" })).toBe("Unreachable")
    expect(label({ state: "done", view: "lost", lost: "vm-1 no longer has this task" })).toBe("Lost on worker")
    expect(label({ state: "done", view: "not_accepted" })).toBe("Not accepted")
  })
})

describe("workerReach", () => {
  const now = new Date("2026-11-05T22:00:00Z")
  it("says when the main last pulled a reachable worker", () => {
    expect(workerReach({ ...vm1, last_pull_at: "2026-11-05T21:58:00Z" }, now)).toEqual({ text: "pulled 2 min ago", tone: "ok" })
    expect(workerReach({ ...vm1, last_pull_at: "2026-11-05T21:59:40Z" }, now).text).toBe("pulled just now")
    expect(workerReach({ ...vm1, last_pull_at: "2026-11-05T19:00:00Z" }, now).text).toBe("pulled 3 h ago")
    expect(workerReach(vm1, now)).toEqual({ text: "not pulled yet", tone: "quiet" })
  })
  it("says since when a worker is unreachable, and that one refusing this main needs pairing again", () => {
    const since = "2026-11-05T21:40:00Z"
    const time = new Date(since).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })
    expect(workerReach({ ...vm1, last_pull_at: since, unreachable_since: since }, now)).toEqual({ text: `unreachable since ${time}`, tone: "bad" })
    expect(workerReach({ ...vm1, refused_at: since }, now)).toEqual({ text: "no longer accepts this main; pair again", tone: "bad" })
  })
})

describe("awayLook, review passes", () => {
  const away = card("b", "Ready", {}).away!
  it("shows the review's phase and counts, and why a stuck review needs you", () => {
    const look = (more: Partial<AwayOn>) => awayLook({ ...away, state: "in_progress", ...more })
    expect(look({ view: "in_review", review: { pass: 2, phase: "reviewing", blocking_open: 0, comments_open: 0, checks_open: 0, skipped_comments: 0 } }).label).toBe("Reviewing · pass 2")
    expect(look({ view: "in_review", review: { pass: 2, phase: "waiting_for_author", blocking_open: 3, comments_open: 1, checks_open: 0, skipped_comments: 0 } }).label).toBe("Author fixing · 3 blocking, 1 comment")
    expect(look({ view: "needs_you", review: { pass: 3, phase: "stuck", reason: "the review reached its limit of 3 passes", blocking_open: 1, comments_open: 0, checks_open: 0, skipped_comments: 0 } }).label).toBe("Review stuck: the review reached its limit of 3 passes")
    expect(awayLook({ ...away, state: "done", view: "done", review: { pass: 2, phase: "clean", blocking_open: 0, comments_open: 0, checks_open: 0, skipped_comments: 0 } }).label).toBe("Done · approved")
    expect(look({ view: "in_review", review: { pass: 1, phase: "clean", blocking_open: 0, comments_open: 0, checks_open: 0, checks_pending: 2, skipped_comments: 0 } }).label).toBe("Approved · waiting for 2 checks")
    expect(awayLook({ ...away, state: "done", view: "done", review: { pass: 1, phase: "clean", blocking_open: 0, comments_open: 0, checks_open: 0, checks_replied: 1, skipped_comments: 0 } }).label).toBe("Done · approved · 1 check answered by reply")
  })
})
