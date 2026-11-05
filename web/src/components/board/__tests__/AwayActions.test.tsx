import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { AwayRowActions, ReturnAllDoneButton, WorkerGroupMenu } from "../AwayActions"
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

  it("takes back a task lost on its worker, after asking", async () => {
    answer = () => json({ returned: { task_id: "T-4", column: "Doing", recalled: false, taken_back: true, branches: [] } })
    const onNotice = vi.fn()
    render(<AwayRowActions baseUrl="" card={away("T-4", { view: "lost", state: "done", lost: "vm-1 no longer has this task" })} onNotice={onNotice} />)
    expect(screen.queryByRole("button", { name: "Return" })).toBeNull()
    expect(screen.queryByRole("button", { name: "Recall" })).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Take back" }))
    expect(posts).toEqual([])
    expect(screen.getByRole("dialog").textContent).toContain("without its results")
    fireEvent.click(screen.getByRole("button", { name: "Take back now" }))
    await waitFor(() => expect(posts).toEqual(["/api/tasks/T-4/take-back"]))
    await waitFor(() => expect(onNotice).toHaveBeenCalledWith("T-4 is back in Doing, without its results", false))
  })

  it("offers Take back for an unreachable worker and one that no longer accepts this main", () => {
    for (const view of ["unreachable", "not_accepted"]) {
      render(<AwayRowActions baseUrl="" card={away("T-5", { view, state: "in_progress" })} />)
      expect(screen.getByRole("button", { name: "Take back" })).toBeTruthy()
      expect(screen.queryByRole("button", { name: "Recall" })).toBeNull()
      cleanup()
    }
  })

  it("offers no Take back while the worker can bring the task back", () => {
    render(<AwayRowActions baseUrl="" card={away("T-6", { view: "working", state: "in_progress" })} />)
    expect(screen.queryByRole("button", { name: "Take back" })).toBeNull()
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

describe("WorkerGroupMenu", () => {
  it("removes a worker after warning how many tasks it holds", async () => {
    const deletes: string[] = []
    vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "DELETE") deletes.push(String(url))
      return json({ removed: { worker: "vm-1", recalled: [{ task_id: "T-1" }], taken_back: [{ task_id: "T-2" }], left: true } })
    }))
    const onNotice = vi.fn()
    render(<WorkerGroupMenu baseUrl="" worker={vm1} cards={[away("T-1"), away("T-2")]} onNotice={onNotice} />)
    fireEvent.click(screen.getByRole("button", { name: "More for vm-1" }))
    expect(screen.getByRole("menuitem", { name: "Open on worker" }).getAttribute("href")).toBe("https://vm-1.lan:3080")
    fireEvent.click(screen.getByRole("menuitem", { name: "Remove…" }))
    expect(screen.getByRole("dialog").textContent).toContain("vm-1 holds 2 away tasks")
    expect(deletes).toEqual([])
    fireEvent.click(screen.getByRole("button", { name: "Remove vm-1" }))
    await waitFor(() => expect(deletes).toEqual(["/api/settings/workers/srv-1"]))
    await waitFor(() => expect(onNotice).toHaveBeenCalledWith("Removed vm-1: 1 recalled, 1 taken back", false))
  })
})
