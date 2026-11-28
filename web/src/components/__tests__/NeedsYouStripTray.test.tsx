import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { Layout } from "../Layout"
import { BoardCard } from "../board/BoardCard"
import { resetRailState } from "../../lib/railState"
import type { BoardCard as BoardCardType } from "../../lib/board-types"

vi.mock("../../hooks/useNavigationSummary", () => ({ useNavigationSummary: () => ({ data: {} }) }))
vi.mock("../../hooks/useWebSocket", () => ({ useWebSocket: () => ({ connected: true, lost: false }) }))
vi.mock("../../hooks/useSettings", () => ({ useSettings: () => ({ data: undefined }) }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ servers: [], activeServerName: "Local", baseUrl: "", setActiveServer: () => {} }) }))
vi.mock("../needsYou/NeedsYouSheet", () => ({ NeedsYouSheet: ({ onClose }: { onClose: () => void }) => <div role="dialog" aria-label="Needs you"><button onClick={onClose}>Close</button></div> }))
vi.mock("../needsYou/NeedsYouRail", () => ({ NeedsYouRail: () => null }))
vi.mock("../PlanApprovalModal", () => ({ PlanApprovalModal: () => null }))
vi.mock("../OfflineDialog", () => ({ OfflineDialog: () => null }))
vi.mock("../approvals/ApprovalTray", () => ({ ApprovalTray: () => null, ApprovalFocus: () => null, useApprovalLink: () => {} }))
vi.mock("../../hooks/useAgentSelection", () => ({ AgentSelectionProvider: ({ children }: { children: React.ReactNode }) => children }))
vi.mock("../../hooks/useSheetAutoOpen", () => ({ NeedsYouAutoOpen: () => null }))

function card(needs: BoardCardType["needs_you"]): BoardCardType {
  return {
    id: "T-1", title: "Card", priority: "", issue_type: "", status: "Doing", provider: "", has_warning: false, agent_active: true, agent_state: "",
    working_seconds: 0, waiting_seconds: 0, error_seconds: 0, workspace_id: null, workspace_name: "", workspace_color: "", needs_you: needs,
  }
}

function Where() {
  return <span data-testid="where">{useLocation().pathname}</span>
}

function renderBoard(c: BoardCardType) {
  render(
    <MemoryRouter initialEntries={["/board"]}>
      <Routes>
        <Route element={<Layout />}>
          <Route path="*" element={<><Where /><BoardCard card={c} selected={false} column="Doing" showWorkspace={false} onClick={() => {}} /></>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  )
}

const sheet = () => screen.queryByRole("dialog", { name: "Needs you" })

beforeEach(() => {
  resetRailState()
  vi.stubGlobal("matchMedia", (query: string) => ({ matches: query.includes("max-width"), media: query, addEventListener: () => {}, removeEventListener: () => {} }))
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("a card's needs-you strip on a phone", () => {
  it("opens the Needs you sheet on Answer, and again on the same Answer after it was closed", () => {
    renderBoard(card({ approvals: { count: 1, id: "ap-1", label: "permission to run a command" } }))
    const answer = within(screen.getByTestId("needs-you-strip")).getByRole("button", { name: "Answer" })
    fireEvent.click(answer)
    expect(sheet()).toBeTruthy()
    fireEvent.click(within(sheet()!).getByRole("button", { name: "Close" }))
    expect(sheet()).toBeNull()
    fireEvent.click(answer)
    expect(sheet()).toBeTruthy()
  })

  it("opens the Needs you sheet on Open, where it is, for the agent's items", () => {
    renderBoard(card({ items: { question: 1 } }))
    fireEvent.click(within(screen.getByTestId("needs-you-strip")).getByRole("link", { name: "Open" }))
    expect(sheet()).toBeTruthy()
    expect(screen.getByTestId("where").textContent).toBe("/board")
  })
})
