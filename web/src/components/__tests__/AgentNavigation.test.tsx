import { render, screen, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { Layout } from "../Layout"

const { state } = vi.hoisted(() => ({ state: { agentCount: 0 } }))

vi.mock("../../hooks/useAgentCount", () => ({ useAgentCount: () => state.agentCount }))
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

describe("agent navigation", () => {
  it("shows the non-idle count on desktop and mobile Agents links", () => {
    state.agentCount = 3

    renderLayout()

    const links = screen.getAllByRole("link", { name: "Agents: 3 non-idle" })
    expect(links).toHaveLength(2)
    for (const link of links) {
      expect(within(link).getByText("3")).toBeTruthy()
    }
    expect(links.some((link) => link.getAttribute("title") === "Agents: 3 non-idle")).toBe(true)
  })

  it("omits both badges when every agent is idle", () => {
    renderLayout()

    const links = screen.getAllByRole("link", { name: "Agents" })
    expect(links).toHaveLength(2)
    for (const link of links) {
      expect(within(link).queryByText("0")).toBeNull()
    }
    expect(links.some((link) => link.getAttribute("title") === "Agents")).toBe(true)
  })

  it("caps both visual badges while exposing the full count", () => {
    state.agentCount = 125

    renderLayout()

    const links = screen.getAllByRole("link", { name: "Agents: 125 non-idle" })
    expect(links).toHaveLength(2)
    for (const link of links) {
      expect(within(link).getByText("99+")).toBeTruthy()
    }
    expect(links.some((link) => link.getAttribute("title") === "Agents: 125 non-idle")).toBe(true)
  })
})
