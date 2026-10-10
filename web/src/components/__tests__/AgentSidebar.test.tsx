import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
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
  error_seconds: 0,
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

  it("shows which agent tool each session runs and how it is connected", () => {
    render(
      <AgentSidebar
        agents={[
          { ...longAgent, id: 1, task_id: "task-claude", agent_kind: "claude-code", command: "claude", has_chimera_endpoint: true },
          { ...longAgent, id: 2, task_id: "task-codex", agent_kind: "codex", command: "codex" },
        ]}
        selectedId={null}
        onSelect={vi.fn()}
        onSpawn={vi.fn()}
      />,
    )

    expect(within(screen.getByRole("button", { name: /task-claude/ })).getByLabelText("Claude Code, native session")).not.toBeNull()
    expect(within(screen.getByRole("button", { name: /task-codex/ })).getByLabelText("Codex, terminal session")).not.toBeNull()
  })

  it("names the foreground process only when it is not the agent tool itself", () => {
    render(
      <AgentSidebar
        agents={[
          { ...longAgent, id: 1, task_id: "task-running", agent_kind: "claude-code", command: "claude" },
          { ...longAgent, id: 2, task_id: "task-exited", agent_kind: "claude-code", command: "zsh" },
        ]}
        selectedId={null}
        onSelect={vi.fn()}
        onSpawn={vi.fn()}
      />,
    )

    // "claude" beside a Claude Code badge says nothing; "zsh" says the tool
    // is not what is running in that pane.
    expect(within(screen.getByRole("button", { name: /task-running/ })).queryByText("claude")).toBeNull()
    expect(within(screen.getByRole("button", { name: /task-exited/ })).getByText("zsh")).not.toBeNull()
  })

  it("reports the right-clicked agent without changing left-click selection", () => {
    const onSelect = vi.fn()
    const onAgentContextMenu = vi.fn()
    render(
      <AgentSidebar
        agents={[longAgent]}
        selectedId={null}
        onSelect={onSelect}
        onSpawn={vi.fn()}
        onAgentContextMenu={onAgentContextMenu}
      />,
    )

    const agentButton = screen.getByRole("button", { name: new RegExp(longAgent.task_id) })
    fireEvent.contextMenu(agentButton, { clientX: 41, clientY: 73 })

    expect(onAgentContextMenu).toHaveBeenCalledWith(longAgent, { x: 41, y: 73 })
    expect(onSelect).not.toHaveBeenCalled()

    fireEvent.click(agentButton)
    expect(onSelect).toHaveBeenCalledWith(longAgent.task_id)
  })

  it("marks an agent that is waiting on its own background work", () => {
    render(
      <AgentSidebar
        agents={[{ ...longAgent, activity: "background" }]}
        selectedId={longAgent.task_id}
        onSelect={() => {}}
        onSpawn={() => {}}
      />,
    )
    // Neither working nor idle: a shell it started is still running.
    expect(screen.getByTitle("Background work running").className).toContain("bg-sky-400")
    expect(screen.queryByTitle("Idle")).toBeNull()
  })
})
