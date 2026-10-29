import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { AgentSidebar } from "../AgentSidebar"
import type { AgentInfo } from "../../hooks/useWebSocket"

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast: vi.fn() }) }))

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
  it("marks an agent that showed artifacts the person has not seen", () => {
    const quiet: AgentInfo = { ...longAgent, id: 2, task_id: "quiet-task", task_title: "Quiet" }
    render(
      <AgentSidebar
        agents={[{ ...longAgent, task_id: "busy-task" }, quiet]}
        selectedId="quiet-task"
        onSelect={() => {}}
        onSpawn={() => {}}
        unseenArtifacts={{ "busy-task": 2 }}
      />,
    )

    expect(screen.getByLabelText("2 new artifacts").textContent).toBe("2")
    expect(screen.getAllByLabelText(/new artifact/)).toHaveLength(1)
  })

  it("lists agents under what they are doing, those that need the person first", () => {
    render(
      <AgentSidebar
        agents={[
          { ...longAgent, id: 1, task_id: "idle-one", activity: "" },
          { ...longAgent, id: 2, task_id: "busy-one", activity: "working" },
          { ...longAgent, id: 3, task_id: "asks-one", activity: "waiting", waiting_seconds: 300 },
          { ...longAgent, id: 4, task_id: "failed-one", activity: "error" },
        ]}
        selectedId={null}
        onSelect={() => {}}
        onSpawn={() => {}}
      />,
    )
    const headings = screen.getAllByRole("heading").map((heading) => heading.textContent)
    expect(headings).toEqual(["Needs you · 2", "Running · 1", "Idle · 1"])
    const order = screen.getAllByRole("button", { name: /-one/ }).map((button) => button.getAttribute("data-agent"))
    expect(order).toEqual(["asks-one", "failed-one", "busy-one", "idle-one"])
  })

  it("keeps a swarm's agents together, the conductor first, inside a state", () => {
    render(
      <AgentSidebar
        agents={[
          { ...longAgent, id: 1, task_id: "solo", activity: "working" },
          { ...longAgent, id: 2, task_id: "worker-a", activity: "working", parent_task_id: "P", role: "backend" },
          { ...longAgent, id: 3, task_id: "conductor", activity: "working", parent_task_id: "P", role: "conductor" },
        ]}
        selectedId={null}
        onSelect={() => {}}
        onSpawn={() => {}}
      />,
    )
    const order = screen.getAllByRole("button").map((button) => button.getAttribute("data-agent")).filter(Boolean)
    expect(order).toEqual(["conductor", "worker-a", "solo"])
    expect(screen.getByTitle("Swarm conductor")).toBeTruthy()
    expect(screen.getByTitle("Worker role: backend")).toBeTruthy()
  })

  it("shows how long an agent has been in its state, its tool, and how full its context is", () => {
    render(
      <AgentSidebar
        agents={[{ ...longAgent, task_id: "busy", activity: "working", working_seconds: 5 * 3600 + 44 * 60, agent_kind: "claude-code", has_chimera_endpoint: true }]}
        contexts={{ busy: { used: 730, window: 1000, share: 73 } }}
        selectedId={null}
        onSelect={() => {}}
        onSpawn={() => {}}
      />,
    )
    const entry = screen.getByRole("button", { name: /busy/ })
    expect(within(entry).getByText("5h 44m")).toBeTruthy()
    expect(within(entry).getByLabelText("Context 73% full").textContent).toContain("73%")
  })

  it("draws its recent activity as a row of slanted cells, one for each stretch of time", () => {
    render(
      <AgentSidebar
        agents={[{ ...longAgent, activity: "error", state_timeline: ["working", "waiting", "error", ""] }]}
        selectedId={longAgent.task_id}
        onSelect={() => {}}
        onSpawn={() => {}}
      />,
    )
    const bar = screen.getByLabelText("Recent activity")
    expect([...bar.querySelectorAll("[data-state]")].map((cell) => cell.getAttribute("data-state"))).toEqual(["working", "waiting", "error", ""])
    expect(bar.querySelector('[data-state="working"]')!.className).toContain("-skew-x")
  })

  it("marks an agent outside the sandbox after its recent activity, apart from its tool", () => {
    render(
      <AgentSidebar
        agents={[{ ...longAgent, agent_kind: "claude-code", has_chimera_endpoint: true, unsandboxed: true, state_timeline: ["working", ""] }]}
        selectedId={longAgent.task_id}
        onSelect={() => {}}
        onSpawn={() => {}}
      />,
    )
    const mark = screen.getByText("unsandboxed")
    expect(screen.getByLabelText("Claude Code, native session").contains(mark)).toBe(false)
    expect(screen.getByLabelText("Recent activity").nextElementSibling).toBe(mark)
  })

  it("shows no sandbox mark for an agent inside the sandbox", () => {
    render(<AgentSidebar agents={[{ ...longAgent, state_timeline: ["working"] }]} selectedId={longAgent.task_id} onSelect={() => {}} onSpawn={() => {}} />)
    expect(screen.queryByText("unsandboxed")).toBeNull()
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
    expect(screen.getByRole("heading", { name: "Background · 1" })).toBeTruthy()
    expect(screen.queryByRole("heading", { name: /Idle/ })).toBeNull()
  })

  it("numbers the agents for switching in the order it lists them", () => {
    render(
      <AgentSidebar
        agents={[{ ...longAgent, id: 1, task_id: "idle-one", activity: "" }, { ...longAgent, id: 2, task_id: "busy-one", activity: "working" }]}
        selectedId={null}
        onSelect={() => {}}
        onSpawn={() => {}}
        modifierHeld
      />,
    )
    expect(within(screen.getByRole("button", { name: /busy-one/ })).getByText("1")).toBeTruthy()
    expect(within(screen.getByRole("button", { name: /idle-one/ })).getByText("2")).toBeTruthy()
  })
})
