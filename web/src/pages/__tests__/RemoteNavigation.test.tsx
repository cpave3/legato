import { cleanup, render, screen } from "@testing-library/react"
import { Outlet } from "react-router-dom"
import { afterEach, expect, it, vi } from "vitest"
import App from "../../App"

vi.mock("../../components/Layout", () => ({ Layout: () => <Outlet /> }))
vi.mock("../../hooks/useSettings", () => ({
  useSettings: () => ({ isSuccess: true }),
  SettingsRequestError: class extends Error {},
}))
vi.mock("../../pages/RemotePage", () => ({ RemotePage: () => <h1>Managed clones route</h1> }))
vi.mock("../../pages/Agents", () => ({ AgentsPage: () => null }))
vi.mock("../../pages/Board", () => ({ BoardPage: () => null }))
vi.mock("../../pages/Settings", () => ({ SettingsPage: () => null }))
vi.mock("../../pages/ReviewQueuePage", () => ({ ReviewQueuePage: () => null }))
vi.mock("../../pages/ReviewTourPage", () => ({ ReviewTourPage: () => null }))
vi.mock("../../pages/PlanQueuePage", () => ({ PlanQueuePage: () => null }))
vi.mock("../../pages/PlanPage", () => ({ PlanPage: () => null }))
vi.mock("../../pages/WorkflowsPage", () => ({ WorkflowsPage: () => null }))
vi.mock("../../pages/WorkflowRunPage", () => ({ WorkflowRunPage: () => null }))
vi.mock("../../pages/AttentionPage", () => ({ AttentionPage: () => null }))
vi.mock("../../pages/LearningsPage", () => ({ LearningsPage: () => null }))
vi.mock("../../pages/LearningDetailPage", () => ({ LearningDetailPage: () => null }))
vi.mock("../../pages/ReportsPage", () => ({ ReportsPage: () => null }))
vi.mock("../../pages/AuditPage", () => ({ AuditPage: () => null }))

afterEach(() => {
  cleanup()
  window.history.replaceState({}, "", "/")
})

it("renders RemotePage on direct /remote access through App", async () => {
  window.history.replaceState({}, "", "/remote")
  render(<App />)
  expect(await screen.findByRole("heading", { name: "Managed clones route" })).toBeTruthy()
  expect(window.location.pathname).toBe("/remote")
})
