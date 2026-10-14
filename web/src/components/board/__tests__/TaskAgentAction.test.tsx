import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { MemoryRouter, useLocation } from "react-router-dom"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { TaskAgentAction } from "../TaskAgentAction"

const { apiFetch, select, startSwarmProps, runningAgentState } = vi.hoisted(() => ({
  apiFetch: vi.fn(),
  select: vi.fn(),
  startSwarmProps: { current: null as null | Record<string, unknown> },
  runningAgentState: {
    agent: null as null | { task_id: string; task_title: string; status: string },
    agents: [] as { task_id: string; task_title: string; status: string }[],
    loading: false,
  },
}))

vi.mock("../../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../../hooks/useAgentSelection", () => ({ useAgentSelection: () => ({ setSelectedAgentId: select }) }))
vi.mock("../../../hooks/useRunningAgent", () => ({ useRunningAgent: () => runningAgentState }))
vi.mock("../../../hooks/useSettings", () => {
  const data = { working_dir: "/default", repositories: [{ name: "Legato", repo: "cpave3/legato", primary_dir: "/projects/legato" }] }
  return { useSettings: () => ({ data }) }
})
vi.mock("../../../lib/api", () => ({ apiFetch }))
vi.mock("../../StartSwarmModal", () => ({ StartSwarmModal: (props: Record<string, unknown>) => { startSwarmProps.current = props; return null } }))

function pathField() {
  return screen.getByRole("combobox", { name: "Path" }) as HTMLInputElement
}

// startsIn waits for the dialog to say where the agent starts: the path is
// filled in once the dialog has asked the server for its presets.
async function startsIn(directory: string) {
  await vi.waitFor(() => expect(pathField().value).toBe(directory))
}

// confirm presses the dialog's button, which waits for the adapters.
async function confirm() {
  const button = await screen.findByRole("button", { name: /^Open agent in / }) as HTMLButtonElement
  await vi.waitFor(() => expect(button.disabled).toBe(false))
  fireEvent.click(button)
}

function pickAgent(name: string) {
  fireEvent.click(screen.getByRole("radio", { name }))
}

function CurrentPath() {
  return <span data-testid="current-path">{useLocation().pathname}</span>
}

describe("TaskAgentAction", () => {
  beforeEach(() => {
    cleanup()
    apiFetch.mockReset()
    select.mockReset()
    runningAgentState.agent = null
    runningAgentState.agents = []
    runningAgentState.loading = false
    startSwarmProps.current = null
  })

  it("goes directly to the running agent without opening or launching", () => {
    runningAgentState.agent = { task_id: "running-T-1", task_title: "Running task", status: "running" }

    render(<MemoryRouter initialEntries={["/board"]}>
      <TaskAgentAction taskId="T-1" />
      <CurrentPath />
    </MemoryRouter>)

    fireEvent.click(screen.getByRole("button", { name: "Go to agent" }))

    expect(select).toHaveBeenCalledWith("running-T-1")
    expect(screen.getByTestId("current-path").textContent).toBe("/agents")
    expect(screen.queryByRole("dialog", { name: "Open task agent" })).toBeNull()
    expect(apiFetch).not.toHaveBeenCalled()
  })

  it("opens the agents page without arbitrary selection for multiple secondary agents", () => {
    runningAgentState.agents = [
      { task_id: "workflow-1", task_title: "Triage", status: "running" },
      { task_id: "workflow-2", task_title: "Review", status: "running" },
    ]

    render(<MemoryRouter initialEntries={["/board"]}>
      <TaskAgentAction taskId="T-1" />
      <CurrentPath />
    </MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: "Go to agents" }))

    expect(select).toHaveBeenCalledWith(null)
    expect(screen.getByTestId("current-path").textContent).toBe("/agents")
  })

  it("does not offer agent launch while running state is loading", () => {
    runningAgentState.loading = true

    render(<MemoryRouter><TaskAgentAction taskId="T-1" /></MemoryRouter>)

    const action = screen.getByRole("button", { name: "Checking agent…" }) as HTMLButtonElement
    expect(action.disabled).toBe(true)
    fireEvent.click(action)
    expect(screen.queryByRole("dialog", { name: "Open task agent" })).toBeNull()
  })

  it("updates the action when running agent state changes", () => {
    const view = render(<MemoryRouter><TaskAgentAction taskId="T-1" /></MemoryRouter>)
    expect(screen.getByRole("button", { name: "Open agent" })).toBeTruthy()

    runningAgentState.agent = { task_id: "running-T-1", task_title: "Running task", status: "running" }
    view.rerender(<MemoryRouter><TaskAgentAction taskId="T-1" /></MemoryRouter>)
    expect(screen.getByRole("button", { name: "Go to agent" })).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Open agent" })).toBeNull()

    runningAgentState.agent = null
    view.rerender(<MemoryRouter><TaskAgentAction taskId="T-1" /></MemoryRouter>)
    expect(screen.getByRole("button", { name: "Open agent" })).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Go to agent" })).toBeNull()
  })

  it("opens a task-scoped swarm launch with the resolved directory", () => {
    render(<MemoryRouter><TaskAgentAction taskId="T-1" worktreePath="/trees/t-1" rememberedWorkingDir="/remembered" /></MemoryRouter>)

    fireEvent.click(screen.getByRole("button", { name: "Start swarm" }))

    expect(startSwarmProps.current).toEqual(expect.objectContaining({
      open: true,
      preSelectedParentId: "T-1",
      initialWorkingDir: "/trees/t-1",
    }))
  })

  it("goes directly to a task swarm conductor", () => {
    runningAgentState.agents = [{
      task_id: "T-1",
      task_title: "Conductor",
      status: "running",
      role: "conductor",
      parent_task_id: "T-1",
    } as typeof runningAgentState.agents[number]]
    render(<MemoryRouter initialEntries={["/board"]}>
      <TaskAgentAction taskId="T-1" />
      <CurrentPath />
    </MemoryRouter>)

    fireEvent.click(screen.getByRole("button", { name: "Go to swarm" }))

    expect(select).toHaveBeenCalledWith("T-1")
    expect(screen.getByTestId("current-path").textContent).toBe("/agents")
  })

  it("defaults agent launch to the task worktree", async () => {
    apiFetch.mockImplementation((_: string, path: string) => Promise.resolve(
      path === "/api/adapters"
        ? new Response(JSON.stringify({ adapters: [] }), { status: 200 })
        : path === "/api/settings"
          ? new Response(JSON.stringify({ working_dir: "/default", repositories: [] }), { status: 200 })
          : new Response(JSON.stringify({ task_id: "T-1", created: true }), { status: 201 }),
    ))

    render(<MemoryRouter><TaskAgentAction taskId="T-1" worktreePath="/trees/t-1" /></MemoryRouter>)
    const trigger = screen.getByRole("button", { name: "Open agent" })
    expect(trigger.getAttribute("data-task-primary-action")).not.toBeNull()
    expect(trigger.className).toContain("min-h-9")
    fireEvent.click(trigger)
    expect(screen.getByRole("dialog", { name: "Open task agent" })).toBeTruthy()
    await startsIn("/trees/t-1")
    await confirm()
    await vi.waitFor(() => expect(apiFetch).toHaveBeenCalledWith("", "/api/tasks/T-1/agent", expect.objectContaining({
      body: JSON.stringify({ agent_kind: "", working_dir: "/trees/t-1" }),
    })))
  })

  it("defaults agent launch to the remembered directory when there is no worktree", async () => {
    apiFetch.mockImplementation((_: string, path: string) => Promise.resolve(
      path === "/api/adapters"
        ? new Response(JSON.stringify({ adapters: [] }), { status: 200 })
        : new Response(JSON.stringify({ task_id: "T-1", created: true }), { status: 201 }),
    ))

    render(<MemoryRouter><TaskAgentAction taskId="T-1" rememberedWorkingDir="/repos/remembered" /></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: "Open agent" }))
    await startsIn("/repos/remembered")
    await confirm()

    await vi.waitFor(() => expect(apiFetch).toHaveBeenCalledWith("", "/api/tasks/T-1/agent", expect.objectContaining({
      body: JSON.stringify({ agent_kind: "", working_dir: "/repos/remembered" }),
    })))
  })

  it("keeps the task worktree ahead of the remembered directory", async () => {
    apiFetch.mockResolvedValue(new Response(JSON.stringify({ adapters: [] }), { status: 200 }))

    render(<MemoryRouter><TaskAgentAction taskId="T-1" worktreePath="/trees/t-1" rememberedWorkingDir="/repos/remembered" /></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: "Open agent" }))

    await startsIn("/trees/t-1")
  })

  it("offers persisted Chimera sessions and resumes by default", async () => {
    apiFetch.mockImplementation((_: string, path: string) => Promise.resolve(
      path === "/api/adapters"
        ? new Response(JSON.stringify({ adapters: ["chimera"], default: "claude" }), { status: 200 })
        : path === "/api/settings"
          ? new Response(JSON.stringify({ working_dir: "/default", repositories: [] }), { status: 200 })
          : new Response(JSON.stringify({ task_id: "T-1", created: true }), { status: 201 }),
    ))

    render(<MemoryRouter><TaskAgentAction taskId="T-1" resumableAgentKinds={["chimera"]} /></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: "Open agent" }))
    await screen.findByRole("radio", { name: "Chimera" })
    pickAgent("Chimera")

    expect((screen.getByRole("radio", { name: "Resume previous session" }) as HTMLInputElement).checked).toBe(true)
    expect(screen.getByRole("radio", { name: "Start new session" })).toBeTruthy()
    await confirm()

    await vi.waitFor(() => expect(apiFetch).toHaveBeenCalledWith("", "/api/tasks/T-1/agent", expect.objectContaining({
      body: JSON.stringify({ agent_kind: "chimera", working_dir: "/default", session_policy: "resume" }),
    })))
  })

  it.each([
    ["claude-code", "Claude Code"],
    ["codex", "Codex"],
  ])("offers %s history through the default adapter and submits Resume/New", async (kind, label) => {
    apiFetch.mockImplementation((_: string, path: string) => Promise.resolve(
      path === "/api/adapters"
        ? new Response(JSON.stringify({ adapters: [kind], default: kind }), { status: 200 })
        : new Response("saved conversation is unavailable", { status: 409 }),
    ))
    render(<MemoryRouter><TaskAgentAction taskId="T-1" resumableAgentKinds={[kind]} /></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: "Open agent" }))

    const resume = await screen.findByRole("radio", { name: "Resume previous session" }) as HTMLInputElement
    expect(resume.checked).toBe(true)
    expect(screen.getByRole("group", { name: `Existing ${label} session` })).toBeTruthy()
    expect(screen.getByText(`Continue with the previous ${label} context.`)).toBeTruthy()
    await confirm()
    expect(await screen.findByText("saved conversation is unavailable")).toBeTruthy()
    expect(apiFetch).toHaveBeenLastCalledWith("", "/api/tasks/T-1/agent", expect.objectContaining({
      body: JSON.stringify({ agent_kind: "", working_dir: "/default", session_policy: "resume" }),
    }))

    fireEvent.click(screen.getByRole("radio", { name: "Start new session" }))
    await confirm()
    await vi.waitFor(() => expect(apiFetch).toHaveBeenLastCalledWith("", "/api/tasks/T-1/agent", expect.objectContaining({
      body: JSON.stringify({ agent_kind: "", working_dir: "/default", session_policy: "new" }),
    })))
  })

  it("matches availability on provider switches while preserving policy and directory overrides", async () => {
    apiFetch.mockImplementation((_: string, path: string) => Promise.resolve(
      path === "/api/adapters"
        ? new Response(JSON.stringify({ adapters: ["chimera", "claude-code", "codex"], default: "chimera" }), { status: 200 })
        : new Response("launch failed", { status: 409 }),
    ))
    render(<MemoryRouter><TaskAgentAction taskId="T-1" resumableAgentKinds={["chimera", "claude-code"]} /></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: "Open agent" }))
    await screen.findByRole("radio", { name: "Resume previous session" })
    fireEvent.click(screen.getByRole("radio", { name: "Start new session" }))
    pickAgent("Claude Code")
    expect(screen.getByRole("group", { name: "Existing Claude Code session" })).toBeTruthy()
    expect((screen.getByRole("radio", { name: "Start new session" }) as HTMLInputElement).checked).toBe(true)

    const preset = await screen.findByRole("button", { name: /Legato/ })
    fireEvent.click(preset)
    expect(screen.getByRole("radio", { name: "Claude Code" }).getAttribute("aria-checked")).toBe("true")
    expect(screen.getByRole("group", { name: "Existing Claude Code session" })).toBeTruthy()
    expect(pathField().value).toBe("/projects/legato")
    expect(preset.getAttribute("aria-pressed")).toBe("true")
    fireEvent.change(pathField(), { target: { value: "/manual" } })
    expect(preset.getAttribute("aria-pressed")).toBe("false")
    await confirm()
    await screen.findByText("launch failed")
    expect(apiFetch).toHaveBeenLastCalledWith("", "/api/tasks/T-1/agent", expect.objectContaining({
      body: JSON.stringify({ agent_kind: "claude-code", working_dir: "/manual", session_policy: "new" }),
    }))

    pickAgent("Codex")
    expect(screen.queryByRole("radio", { name: "Resume previous session" })).toBeNull()
    await confirm()
    await vi.waitFor(() => expect(apiFetch).toHaveBeenLastCalledWith("", "/api/tasks/T-1/agent", expect.objectContaining({
      body: JSON.stringify({ agent_kind: "codex", working_dir: "/manual" }),
    })))

    pickAgent("Chimera default")
    expect(screen.getByRole("group", { name: "Existing Chimera session" })).toBeTruthy()
    expect((screen.getByRole("radio", { name: "Start new session" }) as HTMLInputElement).checked).toBe(true)
  })

  it("updates availability when board data changes and resets to Resume when reopened", async () => {
    apiFetch.mockImplementation(() => Promise.resolve(
      new Response(JSON.stringify({ adapters: ["chimera", "codex"], default: "chimera" }), { status: 200 }),
    ))
    const view = render(<MemoryRouter><TaskAgentAction taskId="T-1" resumableAgentKinds={[]} /></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: "Open agent" }))
    await screen.findByRole("radio", { name: "Codex" })
    pickAgent("Codex")
    expect(screen.queryByRole("radio", { name: "Resume previous session" })).toBeNull()
    view.rerender(<MemoryRouter><TaskAgentAction taskId="T-1" resumableAgentKinds={["codex"]} /></MemoryRouter>)
    expect((screen.getByRole("radio", { name: "Resume previous session" }) as HTMLInputElement).checked).toBe(true)
    fireEvent.click(screen.getByRole("radio", { name: "Start new session" }))
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    fireEvent.click(screen.getByRole("button", { name: "Open agent" }))
    expect((await screen.findByRole("radio", { name: "Resume previous session" }) as HTMLInputElement).checked).toBe(true)
    // The agent tool that was picked is kept from one opening to the next.
    expect(screen.getByRole("radio", { name: "Codex" }).getAttribute("aria-checked")).toBe("true")
  })

  it("starts a new persisted Chimera session when selected", async () => {
    apiFetch.mockImplementation((_: string, path: string) => Promise.resolve(
      path === "/api/adapters"
        ? new Response(JSON.stringify({ adapters: ["chimera"], default: "chimera" }), { status: 200 })
        : path === "/api/settings"
          ? new Response(JSON.stringify({ working_dir: "/default", repositories: [] }), { status: 200 })
          : new Response(JSON.stringify({ task_id: "T-1", created: true }), { status: 201 }),
    ))

    render(<MemoryRouter><TaskAgentAction taskId="T-1" resumableAgentKinds={["chimera"]} /></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: "Open agent" }))
    fireEvent.click(await screen.findByRole("radio", { name: "Start new session" }))
    await confirm()

    await vi.waitFor(() => expect(apiFetch).toHaveBeenCalledWith("", "/api/tasks/T-1/agent", expect.objectContaining({
      body: JSON.stringify({ agent_kind: "", working_dir: "/default", session_policy: "new" }),
    })))
  })

  it("hides the session choice when switching away from Chimera", async () => {
    apiFetch.mockImplementation((_: string, path: string) => Promise.resolve(
      path === "/api/adapters"
        ? new Response(JSON.stringify({ adapters: ["chimera", "claude"], default: "chimera" }), { status: 200 })
        : new Response(JSON.stringify({ working_dir: "/default", repositories: [] }), { status: 200 }),
    ))

    render(<MemoryRouter><TaskAgentAction taskId="T-1" resumableAgentKinds={["chimera"]} /></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: "Open agent" }))
    expect(await screen.findByRole("radio", { name: "Resume previous session" })).toBeTruthy()
    pickAgent("claude")
    expect(screen.queryByRole("radio", { name: "Resume previous session" })).toBeNull()
  })

  it("does not offer a session choice for a first Chimera launch", async () => {
    apiFetch.mockImplementation((_: string, path: string) => Promise.resolve(
      path === "/api/adapters"
        ? new Response(JSON.stringify({ adapters: ["chimera"], default: "chimera" }), { status: 200 })
        : new Response(JSON.stringify({ working_dir: "/default", repositories: [] }), { status: 200 }),
    ))

    render(<MemoryRouter><TaskAgentAction taskId="T-1" /></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: "Open agent" }))
    await screen.findByRole("radio", { name: "Chimera default" })
    expect(screen.queryByRole("radio", { name: "Resume previous session" })).toBeNull()
  })

  it("keeps a failed session launch open with the server error", async () => {
    apiFetch.mockImplementation((_: string, path: string) => Promise.resolve(
      path === "/api/adapters"
        ? new Response(JSON.stringify({ adapters: ["chimera"], default: "chimera" }), { status: 200 })
        : path === "/api/settings"
          ? new Response(JSON.stringify({ working_dir: "/default", repositories: [] }), { status: 200 })
          : new Response("the task no longer has a Chimera session to resume", { status: 409 }),
    ))

    render(<MemoryRouter><TaskAgentAction taskId="T-1" resumableAgentKinds={["chimera"]} /></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: "Open agent" }))
    await screen.findByRole("radio", { name: "Resume previous session" })
    await confirm()

    expect(await screen.findByText("the task no longer has a Chimera session to resume")).toBeTruthy()
    expect(screen.getByRole("dialog", { name: "Open task agent" })).toBeTruthy()
  })

  it("creates or resumes the task agent with a manually overridden repository preset", async () => {
    apiFetch.mockImplementation((_: string, path: string) => Promise.resolve(
      path === "/api/adapters"
        ? new Response(JSON.stringify({ adapters: ["chimera"] }), { status: 200 })
        : path === "/api/settings"
          ? new Response(JSON.stringify({
              working_dir: "/default",
              repositories: [{ name: "Legato", repo: "cpave3/legato", primary_dir: "/projects/legato" }],
            }), { status: 200 })
          : new Response(JSON.stringify({ task_id: "T-1", created: true }), { status: 201 }),
    ))

    render(<MemoryRouter><TaskAgentAction taskId="T-1" /></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: "Open agent" }))

    await screen.findByRole("radio", { name: "Chimera" })
    pickAgent("Chimera")
    fireEvent.click(await screen.findByRole("button", { name: /Legato/ }))
    expect(pathField().value).toBe("/projects/legato")
    fireEvent.change(pathField(), { target: { value: "/projects/task-worktree" } })
    await confirm()

    await vi.waitFor(() => expect(select).toHaveBeenCalledWith("T-1"))
    expect(apiFetch).toHaveBeenCalledWith("", "/api/tasks/T-1/agent", expect.objectContaining({
      method: "POST",
      body: JSON.stringify({ agent_kind: "chimera", working_dir: "/projects/task-worktree" }),
    }))
  })
})
