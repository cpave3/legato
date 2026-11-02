import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { afterEach, describe, expect, it, vi } from "vitest"
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

afterEach(() => cleanup())

describe("remote navigation", () => {
  it("opens Remote from the desktop rail and marks it current", () => {
    render(<MemoryRouter initialEntries={["/agents"]}><Routes><Route element={<Layout />}><Route path="/agents" element={<h1>Agents route</h1>} /><Route path="/remote" element={<h1>Remote route</h1>} /></Route></Routes></MemoryRouter>)
    const desktop = within(screen.getByRole("navigation", { name: "Primary" })).getByRole("link", { name: "Remote" })
    expect(desktop.getAttribute("href")).toBe("/remote")
    fireEvent.click(desktop)
    expect(screen.getByRole("heading", { name: "Remote route" })).toBeTruthy()
    expect(desktop.getAttribute("aria-current")).toBe("page")
  })

  it("opens Remote from mobile More Tools and keeps More active after closing", () => {
    render(<MemoryRouter initialEntries={["/agents"]}><Routes><Route element={<Layout />}><Route path="/agents" element={<h1>Agents route</h1>} /><Route path="/remote" element={<h1>Remote route</h1>} /></Route></Routes></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: "More" }))
    const tools = within(screen.getByRole("group", { name: "Tools" }))
    const remote = tools.getByRole("link", { name: "Remote" })
    expect(remote.getAttribute("href")).toBe("/remote")
    fireEvent.click(remote)
    expect(screen.getByRole("heading", { name: "Remote route" })).toBeTruthy()
    expect(screen.queryByRole("dialog", { name: "More" })).toBeNull()
    const more = screen.getByRole("button", { name: "More" })
    expect(more.className).toContain("text-lw-text")
    expect(more.getAttribute("aria-expanded")).toBe("false")
  })
})

it("closes More when Remote is already the current route", () => {
  render(<MemoryRouter initialEntries={["/remote"]}><Routes><Route element={<Layout />}><Route path="/remote" element={<h1>Remote route</h1>} /></Route></Routes></MemoryRouter>)
  fireEvent.click(screen.getByRole("button", { name: "More" }))
  fireEvent.click(within(screen.getByRole("group", { name: "Tools" })).getByRole("link", { name: "Remote" }))
  expect(screen.queryByRole("dialog", { name: "More" })).toBeNull()
})
