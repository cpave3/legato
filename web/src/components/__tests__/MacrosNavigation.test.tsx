import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { afterEach, expect, it, vi } from "vitest"
import { Layout } from "../Layout"

vi.mock("../../hooks/useNavigationSummary", () => ({ useNavigationSummary: () => ({ data: { agents: 0, attention: 0, review: 0, plans: 0, workflow_attention: 0 } }) }))
vi.mock("../../hooks/useWebSocket", () => ({ useWebSocket: () => ({ connected: true }) }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ servers: [], activeServerName: "Local", baseUrl: "", setActiveServer: vi.fn() }) }))
vi.mock("../../hooks/useSettings", () => ({ useSettings: () => ({ data: undefined }) }))
vi.mock("../../hooks/useReview", () => ({ useReviewQueue: () => ({ data: [] }) }))
vi.mock("../../hooks/usePlan", () => ({ usePlanQueue: () => ({ data: [] }) }))
vi.mock("../../hooks/useAttention", () => ({ useAttention: () => ({ data: [] }) }))
vi.mock("../../hooks/useWorkflow", () => ({ useWorkflowRuns: () => ({ data: [] }) }))
vi.mock("../needsYou/NeedsYouRail", () => ({ NeedsYouRail: () => null }))
vi.mock("../PlanApprovalModal", () => ({ PlanApprovalModal: () => null }))
vi.mock("../OfflineDialog", () => ({ OfflineDialog: () => null }))
vi.mock("../../hooks/useAgentSelection", () => ({ AgentSelectionProvider: ({ children }: { children: React.ReactNode }) => children }))

afterEach(() => cleanup())

function layout(entry = "/agents") {
  return render(<MemoryRouter initialEntries={[entry]}><Routes><Route element={<Layout />}><Route path="/agents" element={<h1>Agents route</h1>} /><Route path="/macros" element={<h1>Macros route</h1>} /></Route></Routes></MemoryRouter>)
}

it("puts Macros in the rail after Flows", () => {
  layout()
  const links = within(screen.getByRole("navigation", { name: "Primary" })).getAllByRole("link").map((link) => link.getAttribute("aria-label"))
  expect(links.indexOf("Macros")).toBe(links.indexOf("Flows") + 1)
  const macros = within(screen.getByRole("navigation", { name: "Primary" })).getByRole("link", { name: "Macros" })
  expect(macros.getAttribute("href")).toBe("/macros")
  fireEvent.click(macros)
  expect(screen.getByRole("heading", { name: "Macros route" })).toBeTruthy()
  expect(macros.getAttribute("aria-current")).toBe("page")
})

it("on a phone, puts Macros in More beside Workflows", () => {
  layout()
  fireEvent.click(screen.getByRole("button", { name: "More" }))
  const rows = within(screen.getByRole("group", { name: "Tools" })).getAllByRole("link").map((link) => link.getAttribute("aria-label"))
  expect(rows.indexOf("Macros")).toBe(rows.indexOf("Workflows") + 1)
  fireEvent.click(within(screen.getByRole("group", { name: "Tools" })).getByRole("link", { name: "Macros" }))
  expect(screen.getByRole("heading", { name: "Macros route" })).toBeTruthy()
  expect(screen.queryByRole("dialog", { name: "More" })).toBeNull()
})
