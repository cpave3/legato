import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { AgentPickerBar, AgentSelect } from "../AgentSelect"
import type { AgentInfo } from "../../lib/agentWire"

const errorAgent = {
  task_id: "task-error",
  task_title: "Broken agent",
  command: "chimera",
  activity: "error",
  working_seconds: 0,
  waiting_seconds: 0,
} as AgentInfo

describe("AgentSelect", () => {
  afterEach(cleanup)

  it("renders an error dot and label", () => {
    render(<AgentSelect agents={[errorAgent]} selectedId="task-error" onSelect={() => {}} />)
    expect(screen.getByTitle("Error").className).toContain("bg-lw-error")
    const trigger = screen.getByRole("button", { name: /^Agent:/ })
    expect(within(trigger).getByText("Error").className).toContain("text-lw-error")
    fireEvent.click(trigger)
    const list = screen.getByRole("group", { name: "Running agents" })
    expect(within(list).getByRole("heading").textContent).toBe("Needs you · 1")
  })

  it("tells an agent waiting on background work from an idle one", () => {
    // Scoped to this render: the file's tests share one document.
    const view = within(render(<AgentSelect agents={[{ ...errorAgent, activity: "background" }]} selectedId="task-error" onSelect={() => {}} />).container)
    // Drawn as Working is: done blue is Finished's alone.
    expect(view.getByTitle("Background work running").className).not.toContain("lw-done")
    fireEvent.click(view.getByRole("button", { name: /^Agent:/ }))
    const list = view.getByRole("group", { name: "Running agents" })
    // Not idle: it is working on what it started.
    expect(within(list).getByRole("heading").textContent).toBe("Working · 1")
  })
})

const longAgent = {
  task_id: "9oacf5cf",
  task_title: "Record a walk-through of the new board into libretto and narrate every column",
  command: "claude",
  agent_kind: "claude-code",
  has_chimera_endpoint: true,
  activity: "working",
  working_seconds: 4 * 3600 + 4 * 60,
  waiting_seconds: 0,
} as AgentInfo

describe("AgentPickerBar", () => {
  afterEach(cleanup)

  it("names the agent in view, with what it is doing, its time and its ID under the title", () => {
    render(<AgentPickerBar agents={[longAgent]} selectedId="9oacf5cf" onSelect={() => {}} onEnsemble={() => {}} onSpawn={() => {}} />)

    const picker = screen.getByRole("button", { name: /^Agent: Record a walk-through/ })
    expect(picker.getAttribute("aria-label")).toBe(`Agent: ${longAgent.task_title}, working, 4h 4m`)
    expect(picker.getAttribute("aria-expanded")).toBe("false")
    expect(within(picker).getByText("Working")).toBeTruthy()
    expect(within(picker).getByText("4h 4m")).toBeTruthy()
    expect(within(picker).getByText("9oacf5cf")).toBeTruthy()
    expect(within(picker).getByText("Claude Code").parentElement!.getAttribute("title")).toBe("Claude Code, native session")
  })

  it("leaves out the tool, then the ID, where they do not fit whole; never the state", () => {
    render(<AgentPickerBar agents={[longAgent]} selectedId="9oacf5cf" onSelect={() => {}} onEnsemble={() => {}} onSpawn={() => {}} />)

    const picker = screen.getByRole("button", { name: /^Agent:/ })
    const classes = (text: string) => within(picker).getByText(text).className.split(" ")
    expect(classes("Working")).toContain("shrink-0")
    // Each goes with the dot before it, so nothing is left hanging; the
    // widths are of the line, which is a container.
    expect(within(picker).getByText("9oacf5cf").parentElement!.className.split(" ")).toEqual(expect.arrayContaining(["hidden", "@min-[12.5rem]:inline"]))
    expect(within(picker).getByText("Claude Code").parentElement!.className.split(" ")).toEqual(expect.arrayContaining(["hidden", "@min-[18rem]:inline"]))
    expect(within(picker).getByTitle("Working").className.split(" ")).toContain("shrink-0")
  })

  it("lets a long title shrink so the buttons beside it stay on screen", () => {
    render(<AgentPickerBar agents={[longAgent]} selectedId="9oacf5cf" onSelect={() => {}} onEnsemble={() => {}} onSpawn={() => {}} />)

    // A flex item is never narrower than its content unless it is told so.
    const picker = screen.getByRole("button", { name: /^Agent:/ })
    expect(picker.parentElement!.className.split(" ")).toContain("min-w-0")
    for (const name of ["Ensemble", "Spawn agent"]) {
      expect(screen.getByRole("button", { name }).className.split(" ")).toContain("shrink-0")
    }
  })

  it("goes to the ensemble and spawns an agent from its buttons", () => {
    const onEnsemble = vi.fn()
    const onSpawn = vi.fn()
    render(<AgentPickerBar agents={[longAgent]} selectedId="9oacf5cf" onSelect={() => {}} onEnsemble={onEnsemble} onSpawn={onSpawn} />)

    fireEvent.click(screen.getByRole("button", { name: "Ensemble" }))
    fireEvent.click(screen.getByRole("button", { name: "Spawn agent" }))
    expect(onEnsemble).toHaveBeenCalledOnce()
    expect(onSpawn).toHaveBeenCalledOnce()
  })

  it("asks for an agent when none is chosen", () => {
    render(<AgentPickerBar agents={[longAgent]} selectedId={null} onSelect={() => {}} onEnsemble={() => {}} onSpawn={() => {}} />)
    expect(screen.getByRole("button", { name: "Choose an agent" }).textContent).toContain("Choose an agent")
  })
})

describe("AgentSelect grouped", () => {
  afterEach(cleanup)

  const grouped = (task_id: string, activity: string, group?: string) =>
    ({ ...errorAgent, task_id, task_title: `Task ${task_id}`, activity, ...(group ? { group } : {}) }) as AgentInfo

  it("lists the agents by group, a collapsed group with its count and those that need the person", () => {
    const toggle = vi.fn()
    render(
      <AgentSelect
        agents={[grouped("a", "waiting", "Tooling"), grouped("b", "working", "Tooling"), grouped("c", "working")]}
        selectedId="c"
        onSelect={() => {}}
        grouping={{
          mode: "group", setMode: vi.fn(), known: [], defaults: [],
          isCollapsed: (name) => name === "Tooling", toggle, collapseOthers: vi.fn(), move: vi.fn(),
          newGroup: vi.fn(), renameGroup: vi.fn(), deleteGroup: vi.fn(),
        }}
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: /^Agent:/ }))
    const tooling = screen.getByRole("button", { name: "Tooling, 2, 1 need you" })
    expect(tooling.getAttribute("aria-expanded")).toBe("false")
    expect(screen.queryByText("Task a")).toBeNull()
    expect(screen.getByRole("button", { name: "Ungrouped, 1" }).getAttribute("aria-expanded")).toBe("true")

    fireEvent.click(tooling)
    expect(toggle).toHaveBeenCalledWith("Tooling")
  })
})

describe("AgentSelect rows", () => {
  afterEach(cleanup)

  const row = (task_id: string, fields: Partial<AgentInfo> = {}) =>
    ({ ...errorAgent, task_id, task_title: `Task ${task_id}`, activity: "working", ...fields }) as AgentInfo

  it("marks a conductor, a worker's role and its sandbox, and names a command other than its tool", () => {
    render(
      <AgentSelect
        agents={[
          row("cond", { role: "conductor" }),
          row("work", { role: "builder", parent_task_id: "cond", unsandboxed: true, docker_host_network: true }),
          row("edit", { agent_kind: "claude-code", command: "vim" }),
          row("solo", { agent_kind: "claude-code", command: "claude" }),
        ]}
        selectedId="cond"
        onSelect={() => {}}
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: /^Agent:/ }))
    const list = screen.getByRole("group", { name: "Running agents" })
    expect(within(list).getByText("4 agents")).toBeTruthy()
    const conductor = within(list).getByRole("button", { name: /Task cond/ })
    expect(conductor.getAttribute("aria-current")).toBe("true")
    expect(within(conductor).getByText("Conductor")).toBeTruthy()
    const worker = within(list).getByRole("button", { name: /Task work/ })
    expect(worker.getAttribute("aria-current")).toBeNull()
    expect(within(worker).getByText("builder")).toBeTruthy()
    expect(within(worker).queryByText("Conductor")).toBeNull()
    expect(within(worker).getByText("unsandboxed")).toBeTruthy()
    expect(within(worker).getByText("host network")).toBeTruthy()
    expect(within(within(list).getByRole("button", { name: /Task edit/ })).getByText("vim")).toBeTruthy()
    expect(within(within(list).getByRole("button", { name: /Task solo/ })).queryByText("claude")).toBeNull()
  })

  it("selects an agent and closes; a click outside closes too", () => {
    const onSelect = vi.fn()
    render(<div><span>outside</span><AgentSelect agents={[row("a"), row("b")]} selectedId="a" onSelect={onSelect} /></div>)
    const trigger = screen.getByRole("button", { name: /^Agent:/ })
    fireEvent.click(trigger)
    expect(trigger.getAttribute("aria-expanded")).toBe("true")
    fireEvent.click(screen.getByRole("button", { name: /Task b/ }))
    expect(onSelect).toHaveBeenCalledWith("b")
    expect(screen.queryByRole("group", { name: "Running agents" })).toBeNull()
    fireEvent.click(trigger)
    expect(screen.getByRole("group", { name: "Running agents" })).toBeTruthy()
    fireEvent.click(screen.getByText("outside"))
    expect(screen.queryByRole("group", { name: "Running agents" })).toBeNull()
  })

  it("says when no agents run", () => {
    render(<AgentSelect agents={[]} selectedId={null} onSelect={() => {}} />)
    fireEvent.click(screen.getByRole("button", { name: "Choose an agent" }))
    expect(screen.getByText("No active agents")).toBeTruthy()
  })

  it("leads with the Reeve's agent when it has no entry of its own, and counts without it", () => {
    render(<AgentSelect agents={[row("a"), row("reeve-1", { reeve: true, task_title: "The Reeve" })]} selectedId="a" onSelect={() => {}} />)
    fireEvent.click(screen.getByRole("button", { name: /^Agent:/ }))
    const list = screen.getByRole("group", { name: "Running agents" })
    expect(within(list).getByText("1 agent")).toBeTruthy()
    expect(within(list).getAllByRole("button")[0].textContent).toContain("The Reeve")
  })

  it("says no agent is at the stage filtered to, and shows all again", () => {
    const onStageFilter = vi.fn()
    render(<AgentSelect agents={[row("a", { stage: "planning" })]} selectedId="a" onSelect={() => {}} stageFilter="reviewing" onStageFilter={onStageFilter} />)
    fireEvent.click(screen.getByRole("button", { name: /^Agent:/ }))
    expect(screen.getByText(/No agents are reviewing\./)).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Show all" }))
    expect(onStageFilter).toHaveBeenCalledWith(null)
  })
})
