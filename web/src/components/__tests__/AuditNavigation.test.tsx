import { fireEvent, render, screen } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { describe, expect, it, vi } from "vitest"
import { Layout } from "../Layout"

vi.mock("../../hooks/useNavigationSummary", () => ({ useNavigationSummary: () => ({ data: { agents: 0, attention: 0, review: 0, plans: 0, workflow_attention: 0 } }) }))
vi.mock("../../hooks/useWebSocket", () => ({ useWebSocket: () => ({ connected: true }) }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ servers: [], activeServerName: "Local", baseUrl: "", setActiveServer: vi.fn() }) }))
vi.mock("../../hooks/useReview", () => ({ useReviewQueue: () => ({ data: [] }) }))
vi.mock("../../hooks/usePlan", () => ({ usePlanQueue: () => ({ data: [] }) }))
vi.mock("../../hooks/useAttention", () => ({ useAttention: () => ({ data: [] }) }))
vi.mock("../../hooks/useWorkflow", () => ({ useWorkflowRuns: () => ({ data: [] }) }))
vi.mock("../PlanApprovalModal", () => ({ PlanApprovalModal: () => null }))
vi.mock("../OfflineDialog", () => ({ OfflineDialog: () => null }))
vi.mock("../../hooks/useAgentSelection", () => ({ AgentSelectionProvider: ({ children }: { children: React.ReactNode }) => children }))

describe("audit navigation", () => {
  it("exposes the audit log in desktop navigation and the mobile More sheet", () => {
    render(<MemoryRouter initialEntries={["/audit"]}><Routes><Route element={<Layout />}><Route path="/audit" element={<h1>Audit route</h1>} /></Route></Routes></MemoryRouter>)
    const desktop = screen.getByRole("link", { name: "Audit" })
    expect(desktop.getAttribute("href")).toBe("/audit")
    expect(desktop.getAttribute("aria-current")).toBe("page")

    fireEvent.click(screen.getByRole("button", { name: "More" }))
    const reportLinks = screen.getAllByRole("link", { name: /^Audit( log)?$/ })
    expect(reportLinks.length).toBeGreaterThan(1)
    expect(reportLinks.some(link => link.getAttribute("href") === "/audit")).toBe(true)
    expect(screen.getByRole("heading", { name: "Audit route" })).toBeTruthy()
  })
})
