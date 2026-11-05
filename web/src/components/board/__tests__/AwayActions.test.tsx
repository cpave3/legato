import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { AwayRowActions, ReturnAllDoneButton } from "../AwayActions"
import type { AwayOn, BoardCard, BoardWorker } from "../../../lib/board-types"

function away(id: string, more: Partial<AwayOn> = {}): BoardCard {
  return {
    id, title: "Task " + id, priority: "", issue_type: "", status: "Doing", provider: "", has_warning: false, agent_active: false, agent_state: "",
    working_seconds: 0, waiting_seconds: 0, error_seconds: 0, workspace_id: null, workspace_name: "", workspace_color: "",
    away: {
      worker_instance_id: "srv-1", worker: "vm-1", handoff_id: "ho-" + id, worker_task_id: "w-" + id, purpose: "make-change", kind: "new-branch",
      repo: "acme/widget", branch: "legato/" + id, left_column: "Doing", handed_off_at: "2026-11-05T09:00:00Z", ...more,
    },
  }
}

const vm1: BoardWorker = { instance_id: "srv-1", name: "vm-1", address: "https://vm-1.lan:3080" }

let posts: string[]
let answer: () => Response

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
}

beforeEach(() => {
  posts = []
  answer = () => json({ returned: { task_id: "T-1", column: "Done", recalled: false, branches: [] } })
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method === "POST") posts.push(String(url))
    return answer()
  }))
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("AwayRowActions", () => {
  it("returns a done task", async () => {
    const onNotice = vi.fn()
    render(<AwayRowActions baseUrl="" card={away("T-1", { view: "done", state: "done" })} onNotice={onNotice} />)
    expect(screen.queryByRole("button", { name: "Recall" })).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Return" }))
    await waitFor(() => expect(posts).toEqual(["/api/tasks/T-1/return"]))
    await waitFor(() => expect(onNotice).toHaveBeenCalledWith("T-1 is back in Done", false))
  })

  it("asks before recalling a task whose agent may be at work", async () => {
    answer = () => json({ returned: { task_id: "T-2", column: "Doing", recalled: true, branches: [] } })
    render(<AwayRowActions baseUrl="" card={away("T-2", { view: "working", state: "in_progress" })} />)
    expect(screen.queryByRole("button", { name: "Return" })).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Recall" }))
    expect(posts).toEqual([])
    const dialog = screen.getByRole("dialog")
    expect(dialog.textContent).toContain("vm-1")
    fireEvent.click(screen.getByRole("button", { name: "Recall now" }))
    await waitFor(() => expect(posts).toEqual(["/api/tasks/T-2/recall"]))
  })

  it("recalls a failed handoff without asking, and says why one was refused", async () => {
    answer = () => json({ error: "vm-1 did not answer, so the task is still away; trying again is safe" }, 502)
    render(<AwayRowActions baseUrl="" card={away("T-3", { view: "failed", state: "failed", failure: "the clone failed" })} />)
    expect(screen.queryByRole("button", { name: "Return" })).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Recall" }))
    await waitFor(() => expect(posts).toEqual(["/api/tasks/T-3/recall"]))
    expect((await screen.findByRole("alert")).textContent).toContain("vm-1 did not answer")
  })

  it("offers nothing for a task lost on its worker", () => {
    const { container } = render(<AwayRowActions baseUrl="" card={away("T-4", { view: "lost", state: "done" })} />)
    expect(container.textContent).toBe("")
  })
})

describe("ReturnAllDoneButton", () => {
  it("returns every done task of the worker", async () => {
    answer = () => json({ returned: [{ task_id: "T-1", column: "Done", recalled: false, branches: [] }, { task_id: "T-3", column: "Done", recalled: false, branches: [] }] })
    const onNotice = vi.fn()
    const cards = [away("T-1", { view: "done", state: "done" }), away("T-2", { view: "working" }), away("T-3", { view: "done", state: "done" })]
    render(<ReturnAllDoneButton baseUrl="" worker={vm1} cards={cards} onNotice={onNotice} />)
    fireEvent.click(screen.getByRole("button", { name: "Return all done (2)" }))
    await waitFor(() => expect(posts).toEqual(["/api/workers/srv-1/return-done"]))
    await waitFor(() => expect(onNotice).toHaveBeenCalledWith("2 tasks back from vm-1", false))
  })

  it("is not there when nothing is done", () => {
    const { container } = render(<ReturnAllDoneButton baseUrl="" worker={vm1} cards={[away("T-2", { view: "working" })]} />)
    expect(container.textContent).toBe("")
  })
})
