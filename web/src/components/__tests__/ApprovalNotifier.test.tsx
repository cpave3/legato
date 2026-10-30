import { act, cleanup, render, screen } from "@testing-library/react"
import { MemoryRouter, useLocation } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import type { Approval } from "../../lib/approvals"
import { ApprovalNotifier } from "../ApprovalNotifier"
import { setApprovalNotificationsEnabled } from "../../lib/approvalNotifications"

const { current } = vi.hoisted(() => ({ current: { data: [] as Approval[] } }))
vi.mock("../../hooks/useApprovals", () => ({ useApprovals: () => ({ data: current.data, refresh: vi.fn() }) }))

const approval = (id: string): Approval => ({
  id, source: "credential", source_key: id, task_id: "AG-1", title: `Push ${id}`, summary: "git.push", details: {}, options: [],
  blocking: true, status: "pending", revision: 1, created_at: "2026-10-30T01:00:00Z",
} as Approval)

class FakeNotification {
  static permission = "granted"
  static requestPermission = vi.fn(async () => "granted")
  static shown: FakeNotification[] = []
  onclick: (() => void) | null = null
  closed = false
  constructor(public title: string, public options: { body?: string; tag?: string }) { FakeNotification.shown.push(this) }
  close() { this.closed = true }
}

function Where() {
  const location = useLocation()
  return <p data-testid="where">{location.pathname + location.search}</p>
}

function renderNotifier() {
  return render(<MemoryRouter initialEntries={["/board"]}><ApprovalNotifier /><Where /></MemoryRouter>)
}

describe("ApprovalNotifier", () => {
  beforeEach(() => {
    FakeNotification.shown = []
    vi.stubGlobal("Notification", FakeNotification)
    Object.defineProperty(document, "hidden", { value: true, configurable: true })
    current.data = [approval("ap-1")]
  })
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    localStorage.clear()
    Object.defineProperty(document, "hidden", { value: false, configurable: true })
  })

  it("notifies of an approval that arrives while the tab is in the background, and opens it on a click", () => {
    const view = renderNotifier()
    expect(FakeNotification.shown).toHaveLength(0) // ap-1 was waiting already
    current.data = [approval("ap-1"), approval("ap-2")]
    view.rerender(<MemoryRouter initialEntries={["/board"]}><ApprovalNotifier /><Where /></MemoryRouter>)
    expect(FakeNotification.shown.map((n) => n.title)).toEqual(["Approval needed: Push ap-2"])
    act(() => FakeNotification.shown[0].onclick?.())
    expect(screen.getByTestId("where").textContent).toBe("/board?approval=ap-2")
  })

  it("closes the notification once the approval is decided", () => {
    const view = renderNotifier()
    current.data = [approval("ap-1"), approval("ap-2")]
    view.rerender(<MemoryRouter initialEntries={["/board"]}><ApprovalNotifier /><Where /></MemoryRouter>)
    current.data = [approval("ap-1")]
    view.rerender(<MemoryRouter initialEntries={["/board"]}><ApprovalNotifier /><Where /></MemoryRouter>)
    expect(FakeNotification.shown[0].closed).toBe(true)
  })

  it("stays quiet when turned off, or while the tab is in front", () => {
    setApprovalNotificationsEnabled(false)
    const view = renderNotifier()
    current.data = [approval("ap-1"), approval("ap-2")]
    view.rerender(<MemoryRouter initialEntries={["/board"]}><ApprovalNotifier /><Where /></MemoryRouter>)
    expect(FakeNotification.shown).toHaveLength(0)

    setApprovalNotificationsEnabled(true)
    Object.defineProperty(document, "hidden", { value: false, configurable: true })
    vi.spyOn(document, "hasFocus").mockReturnValue(true)
    current.data = [approval("ap-1"), approval("ap-2"), approval("ap-3")]
    view.rerender(<MemoryRouter initialEntries={["/board"]}><ApprovalNotifier /><Where /></MemoryRouter>)
    expect(FakeNotification.shown).toHaveLength(0)
  })
})
