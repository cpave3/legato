import { cleanup, render, screen, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { Layout } from "../Layout"

const { state } = vi.hoisted(() => ({ state: { agentCount: 0 } }))

vi.mock("../../hooks/useNavigationSummary", () => ({ useNavigationSummary: () => ({ data: { agents: state.agentCount, attention: 0, review: 0, plans: 0, workflow_attention: 0 } }) }))
vi.mock("../../hooks/useWebSocket", () => ({ useWebSocket: () => ({ connected: true }) }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ servers: [], activeServerName: "Local", baseUrl: "", setActiveServer: vi.fn() }) }))
vi.mock("../../hooks/useReview", () => ({ useReviewQueue: () => ({ data: [] }) }))
vi.mock("../../hooks/usePlan", () => ({ usePlanQueue: () => ({ data: [] }) }))
vi.mock("../../hooks/useAttention", () => ({ useAttention: () => ({ data: [] }) }))
vi.mock("../../hooks/useWorkflow", () => ({ useWorkflowRuns: () => ({ data: [] }) }))
vi.mock("../PlanApprovalModal", () => ({ PlanApprovalModal: () => null }))
vi.mock("../OfflineDialog", () => ({ OfflineDialog: () => null }))
vi.mock("../../hooks/useAgentSelection", () => ({ AgentSelectionProvider: ({ children }: { children: React.ReactNode }) => children }))

function renderLayout() {
  return render(
    <MemoryRouter initialEntries={["/board"]}>
      <Routes>
        <Route element={<Layout />}>
          <Route path="/board" element={<h1>Board route</h1>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  state.agentCount = 0
})

afterEach(cleanup)

// On a wide screen the count is on Ensemble; on a phone, Agents is one tab
// for both and has it.
const counted = (name: string) => [
  ...screen.getAllByRole("link", { name: name.replace("Agents", "Ensemble") }),
  ...screen.getAllByRole("link", { name }),
]

describe("agent navigation", () => {
  it("shows the non-idle count on the desktop Ensemble link and the mobile Agents tab", () => {
    state.agentCount = 3

    renderLayout()

    const links = counted("Agents: 3 non-idle")
    expect(links).toHaveLength(2)
    for (const link of links) {
      expect(within(link).getByText("3")).toBeTruthy()
      expect(link.getAttribute("title") ?? "Agents: 3 non-idle").toMatch(/: 3 non-idle$/)
    }
  })

  it("omits the badges when every agent is idle", () => {
    renderLayout()

    const links = counted("Agents")
    expect(links).toHaveLength(3)
    for (const link of links) {
      expect(within(link).queryByText("0")).toBeNull()
    }
  })

  it("caps the visual badges while exposing the full count", () => {
    state.agentCount = 125

    renderLayout()

    const links = counted("Agents: 125 non-idle")
    expect(links).toHaveLength(2)
    for (const link of links) {
      expect(within(link).getByText("99+")).toBeTruthy()
    }
  })
})
