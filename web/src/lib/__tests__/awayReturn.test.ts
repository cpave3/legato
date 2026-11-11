import { afterEach, describe, expect, it, vi } from "vitest"
import { awayActions, doneOnWorker, recallTask, returnAllDone, returnTask, takeBackTask } from "../awayReturn"
import type { AwayOn, BoardCard } from "../board-types"

function card(id: string, away: Partial<AwayOn>): BoardCard {
  return {
    id, title: id, priority: "", issue_type: "", status: "Ready", provider: "", has_warning: false, agent_active: false, agent_state: "",
    working_seconds: 0, waiting_seconds: 0, error_seconds: 0, workspace_id: null, workspace_name: "", workspace_color: "",
    away: { worker_instance_id: "srv-1", worker: "vm-1", handoff_id: "ho-" + id, worker_task_id: "w" + id, purpose: "make-change", kind: "new-branch", repo: "acme/widget", branch: "legato/" + id, left_column: "Ready", handed_off_at: "2026-11-05T09:00:00Z", ...away },
  }
}

afterEach(() => vi.unstubAllGlobals())

describe("awayActions", () => {
  it("offers only what the away task's state allows", () => {
    const of = (away: Partial<AwayOn>) => awayActions(card("t", away).away!)
    expect(of({ view: "done", state: "done" })).toEqual({ return: true, recall: false, confirmRecall: false, takeBack: false })
    expect(of({ view: "failed", state: "failed" })).toEqual({ return: false, recall: true, confirmRecall: false, takeBack: false })
    expect(of({ view: "working", state: "in_progress" })).toEqual({ return: false, recall: true, confirmRecall: true, takeBack: false })
    expect(of({ view: "needs_you", state: "in_progress" })).toEqual({ return: false, recall: true, confirmRecall: true, takeBack: false })
    expect(of({ view: "starting", state: "pending" })).toEqual({ return: false, recall: true, confirmRecall: true, takeBack: false })
    expect(of({ view: "ended", state: "in_progress" })).toEqual({ return: false, recall: true, confirmRecall: false, takeBack: false })
    expect(of({ view: "interrupted", state: "in_progress" })).toEqual({ return: false, recall: true, confirmRecall: false, takeBack: false })
    // A return under way can be pushed on.
    expect(of({ view: "coming_back", state: "returning" })).toEqual({ return: true, recall: false, confirmRecall: false, takeBack: false })
    // A worker that cannot be reached, refuses this main or lost the task:
    // only Take back, without the results.
    for (const view of ["unreachable", "not_accepted", "lost"]) {
      expect(of({ view, state: "done" })).toEqual({ return: false, recall: false, confirmRecall: false, takeBack: true })
    }
    // An older server sends only the state.
    expect(of({ state: "done" }).return).toBe(true)
    expect(of({ state: "in_progress" })).toEqual({ return: false, recall: true, confirmRecall: true, takeBack: false })
  })
})

describe("doneOnWorker", () => {
  it("lists the cards Return all done brings back", () => {
    const cards = [card("a", { view: "done", state: "done" }), card("b", { view: "working", state: "in_progress" }), card("c", { view: "unreachable", state: "done" }), card("d", { view: "done", state: "done" })]
    expect(doneOnWorker(cards).map(c => c.id)).toEqual(["a", "d"])
  })
})

describe("return calls", () => {
  it("posts to the main's routes and throws the server's words", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ returned: { task_id: "T-1", column: "Done", recalled: false, branches: [] } }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ error: "vm-1 did not answer, so the task is still away; trying again is safe" }), { status: 502 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ returned: [{ task_id: "T-2", column: "Done", recalled: false, branches: [] }], error: "T-3: refused" }), { status: 200 }))
    vi.stubGlobal("fetch", fetchMock)

    expect((await returnTask("", "T-1")).column).toBe("Done")
    expect(String(fetchMock.mock.calls[0][0])).toContain("/api/tasks/T-1/return")
    expect(fetchMock.mock.calls[0][1].method).toBe("POST")

    await expect(recallTask("", "T-1")).rejects.toThrow("vm-1 did not answer")
    expect(String(fetchMock.mock.calls[1][0])).toContain("/api/tasks/T-1/recall")

    const all = await returnAllDone("", "srv-1")
    expect(all.returned.map(r => r.task_id)).toEqual(["T-2"])
    expect(all.error).toBe("T-3: refused")
    expect(String(fetchMock.mock.calls[2][0])).toContain("/api/workers/srv-1/return-done")
  })
})

describe("takeBackTask", () => {
  it("posts to the take-back route and throws the server's words", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ returned: { task_id: "T-1", column: "Ready", recalled: false, taken_back: true, branches: [] } }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ error: "vm-1 can still bring task T-2 back; recall it" }), { status: 409 }))
    vi.stubGlobal("fetch", fetchMock)
    expect((await takeBackTask("", "T-1")).taken_back).toBe(true)
    expect(String(fetchMock.mock.calls[0][0])).toBe("/api/tasks/T-1/take-back")
    await expect(takeBackTask("", "T-2")).rejects.toThrow("recall it")
  })
})

describe("awayActions, review passes", () => {
  it("refuses Return while a review runs, and offers it as is when the review is stuck", () => {
    const of = (away: Partial<AwayOn>) => awayActions(card("t", away).away!)
    const review = { pass: 1, blocking_open: 0, comments_open: 0, checks_open: 0, skipped_comments: 0 }
    expect(of({ view: "in_review", state: "in_progress", review: { ...review, phase: "reviewing" } })).toEqual({ return: false, recall: true, confirmRecall: true, takeBack: false })
    expect(of({ view: "needs_you", state: "in_progress", review: { ...review, phase: "stuck", reason: "x" } })).toEqual({ return: true, recall: true, confirmRecall: true, takeBack: false })
  })
})
