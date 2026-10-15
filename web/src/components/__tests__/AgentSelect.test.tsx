import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { AgentPickerBar, AgentSelect } from "../AgentSelect"
import type { AgentInfo } from "../../hooks/useWebSocket"

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
    fireEvent.click(screen.getByRole("button", { name: /^Agent:/ }))
    const list = screen.getByRole("group", { name: "Running agents" })
    expect(within(list).getByText("Error").className).toContain("text-lw-error")
  })

  it("tells an agent waiting on background work from an idle one", () => {
    // Scoped to this render: the file's tests share one document.
    const view = within(render(<AgentSelect agents={[{ ...errorAgent, activity: "background" }]} selectedId="task-error" onSelect={() => {}} />).container)
    expect(view.getByTitle("Background work running").className).toContain("bg-lw-done")
    fireEvent.click(view.getByRole("button", { name: /^Agent:/ }))
    const list = view.getByRole("group", { name: "Running agents" })
    expect(within(list).getByText("Background").className).toContain("text-lw-done")
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
    expect(picker.getAttribute("aria-label")).toBe(`Agent: ${longAgent.task_title}, running 4h 4m`)
    expect(picker.getAttribute("aria-expanded")).toBe("false")
    expect(within(picker).getByText("Running 4h 4m")).toBeTruthy()
    expect(within(picker).getByText("9oacf5cf")).toBeTruthy()
    expect(within(picker).getByText("Claude Code").parentElement!.getAttribute("title")).toBe("Claude Code, native session")
  })

  it("leaves out the tool, then the ID, where they do not fit whole; never the state", () => {
    render(<AgentPickerBar agents={[longAgent]} selectedId="9oacf5cf" onSelect={() => {}} onEnsemble={() => {}} onSpawn={() => {}} />)

    const picker = screen.getByRole("button", { name: /^Agent:/ })
    const classes = (text: string) => within(picker).getByText(text).className.split(" ")
    expect(classes("Running 4h 4m")).toContain("shrink-0")
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
