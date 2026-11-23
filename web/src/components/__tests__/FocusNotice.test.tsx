import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { FocusNotice, FocusNoticeProvider, FocusStandby } from "../FocusNotice"
import { useClaimFocusNotice } from "../../hooks/useFocusNotice"
import { AgentEnsemble } from "../AgentEnsemble"
import { getFocusMode, setFocusMode } from "../../lib/focusMode"
import type { Approval } from "../../lib/approvals"

let pending: Approval[] = []
vi.mock("../../hooks/useApprovals", () => ({ useApprovals: () => ({ data: pending, error: null, refresh: async () => {} }) }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))

beforeEach(() => {
  pending = []
  act(() => setFocusMode(true))
})
afterEach(() => {
  cleanup()
  act(() => setFocusMode(false))
})

function Docking() {
  useClaimFocusNotice()
  return <p>ensemble</p>
}

describe("the Focus is on notice", () => {
  it("docks in flow at the top of the page, never over it", () => {
    const { container } = render(<FocusNoticeProvider><FocusStandby /><p>page</p></FocusNoticeProvider>)
    const notice = screen.getByRole("status")
    expect(notice.textContent).toContain("Focus is on. It opens again when a question comes in.")
    // In the page's own flow, before its content: not a portal over it.
    expect(container.contains(notice)).toBe(true)
    expect(notice.compareDocumentPosition(screen.getByText("page")) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Leave focus" }))
    expect(getFocusMode()).toBe(false)
    expect(screen.queryByRole("status")).toBeNull()
  })

  it("says nothing while an approval waits, which focus shows itself", () => {
    pending = [{ id: "a", source: "", source_key: "", title: "", details: {}, options: [], blocking: true, status: "pending", revision: 1, created_at: "" }]
    render(<FocusNoticeProvider><FocusStandby /></FocusNoticeProvider>)
    expect(screen.queryByRole("status")).toBeNull()
  })

  it("leaves the page's notice to a page that docks it in its own header", () => {
    render(<FocusNoticeProvider><FocusStandby /><Docking /></FocusNoticeProvider>)
    expect(screen.queryByRole("status")).toBeNull()
  })

  it("is a compact pill in the ensemble's header, beside the layout switch", () => {
    render(<AgentEnsemble agents={[]} onOpen={vi.fn()} onSpawn={vi.fn()} onShowList={vi.fn()} focusNotice={<FocusNotice compact />} />)
    const notice = screen.getByRole("status")
    expect(notice.textContent).toBe("Focus is onLeave")
    expect(notice.closest("header")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Leave focus" }))
    expect(getFocusMode()).toBe(false)
  })
})
