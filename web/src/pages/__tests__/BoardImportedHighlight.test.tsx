import { act, cleanup, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { MemoryRouter, useLocation } from "react-router-dom"
import { AgentSelectionProvider } from "../../hooks/useAgentSelection"
import { BoardPage } from "../Board"

const importedCard = {
  id: "task-imported", title: "Imported Jira ticket", status: "Doing", priority: "High", issue_type: "Task", provider: "jira",
  has_warning: false, agent_active: false, agent_state: "", working_seconds: 0, waiting_seconds: 0, error_seconds: 0,
  workspace_id: 1, workspace_name: "Platform", workspace_color: "#123456", pr_check_status: "", pr_review_decision: "",
  pr_comment_count: 0, pr_is_draft: false, pr_number: 0,
}
const otherCard = { ...importedCard, id: "task-other", title: "Other task", status: "Backlog", provider: "" }

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useSettings", () => ({ useSettings: () => ({ data: { repositories: [], working_dir: "", worktrees_enabled: false } }) }))
vi.mock("../../hooks/useBoard", () => ({
  useBoard: () => ({
    columns: [{ name: "Backlog", cards: [otherCard] }, { name: "Doing", cards: [importedCard] }],
    workspaces: [{ id: 1, name: "Platform", color: "#123456" }], loading: false, error: null, refresh: vi.fn(),
  }),
}))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast: vi.fn() }) }))
vi.mock("../../hooks/useWorkflow", () => ({ useWorkflows: () => ({ data: { definitions: [], errors: [], repositories: [] }, loading: false, error: null }) }))

function LocationProbe() {
  const location = useLocation()
  return <output data-testid="location">{location.pathname}{location.search}</output>
}

describe("imported board task highlight", () => {
  beforeEach(() => {
    vi.useFakeTimers()
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value: vi.fn() })
  })
  afterEach(() => {
    cleanup()
    vi.useRealTimers()
  })

  it("removes the transient query after eight seconds when the task is not visible", async () => {
    render(
      <MemoryRouter initialEntries={["/board?highlight_task=archived-task&source=jira"]}>
        <AgentSelectionProvider><BoardPage /><LocationProbe /></AgentSelectionProvider>
      </MemoryRouter>,
    )

    expect(document.querySelector('[data-highlighted-task="true"]')).toBeNull()
    await act(async () => { await vi.advanceTimersByTimeAsync(8_000) })
    expect(screen.getByTestId("location").textContent).toBe("/board?source=jira")
  })

  it("selects and highlights the imported task, then removes only its query parameter after eight seconds", async () => {
    render(
      <MemoryRouter initialEntries={["/board?highlight_task=task-imported&source=jira"]}>
        <AgentSelectionProvider><BoardPage /><LocationProbe /></AgentSelectionProvider>
      </MemoryRouter>,
    )

    const highlighted = screen.getAllByText("Imported Jira ticket")
    expect(highlighted).toHaveLength(2)
    expect(document.querySelectorAll('[data-highlighted-task="true"]')).toHaveLength(2)
    expect(screen.getByTestId("location").textContent).toBe("/board?highlight_task=task-imported&source=jira")
    const doingHeader = screen.getAllByText("Doing")[0].parentElement!
    expect(doingHeader.className).toContain("bg-indigo-950/35")

    await act(async () => { await vi.advanceTimersByTimeAsync(7_999) })
    expect(document.querySelectorAll('[data-highlighted-task="true"]')).toHaveLength(2)

    await act(async () => { await vi.advanceTimersByTimeAsync(1) })
    expect(document.querySelector('[data-highlighted-task="true"]')).toBeNull()
    expect(screen.getByTestId("location").textContent).toBe("/board?source=jira")
  })
})
