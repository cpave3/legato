import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { AgentSidebar } from "../AgentSidebar"
import type { AgentInfo } from "../../hooks/useWebSocket"

const longAgent: AgentInfo = {
  id: 1,
  task_id: "task-with-an-extremely-long-identifier-that-must-not-expand-the-sidebar",
  task_title: "A very long task title that should remain inside the fixed sidebar width",
  tmux_session: "legato-long-task",
  command: "chimera --with-a-very-long-command-that-should-be-truncated",
  agent_kind: "chimera",
  status: "running",
  activity: "working",
  started_at: "2026-07-27T12:00:00Z",
  working_seconds: 120,
  waiting_seconds: 0,
}

afterEach(cleanup)

describe("AgentSidebar", () => {
  it("renders error activity and timeline in red", () => {
    render(
      <AgentSidebar
        agents={[{ ...longAgent, activity: "error", state_timeline: ["error"] }]}
        selectedId={longAgent.task_id}
        onSelect={() => {}}
        onSpawn={() => {}}
      />,
    )
    expect(screen.getByTitle("Error").className).toContain("bg-red-500")
    expect(document.querySelector('[data-state="error"]')?.getAttribute("style")).toContain("rgb(239, 68, 68)")
  })

  it("constrains long agent metadata to the fixed sidebar width", () => {
    render(
      <AgentSidebar
        agents={[longAgent]}
        selectedId={longAgent.task_id}
        onSelect={vi.fn()}
        onSpawn={vi.fn()}
      />,
    )

    const sidebar = screen.getByLabelText("Agent sessions")
    const agentButton = screen.getByRole("button", { name: new RegExp(longAgent.task_id) })

    expect(sidebar.classList.contains("overflow-x-hidden")).toBe(true)
    expect(agentButton.classList.contains("min-w-0")).toBe(true)
    expect(agentButton.classList.contains("max-w-full")).toBe(true)
  })
})
