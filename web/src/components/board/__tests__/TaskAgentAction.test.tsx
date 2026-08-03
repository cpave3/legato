import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { TaskAgentAction } from "../TaskAgentAction"

const { apiFetch, select } = vi.hoisted(() => ({ apiFetch: vi.fn(), select: vi.fn() }))

vi.mock("../../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../../hooks/useAgentSelection", () => ({ useAgentSelection: () => ({ setSelectedAgentId: select }) }))
vi.mock("../../../lib/api", () => ({ apiFetch }))

describe("TaskAgentAction", () => {
  beforeEach(() => {
    cleanup()
    apiFetch.mockReset()
    select.mockReset()
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
    expect((await screen.findByLabelText("Working directory") as HTMLInputElement).value).toBe("/trees/t-1")
    fireEvent.click(screen.getAllByRole("button", { name: "Open agent" })[1])
    await vi.waitFor(() => expect(apiFetch).toHaveBeenCalledWith("", "/api/tasks/T-1/agent", expect.objectContaining({
      body: JSON.stringify({ agent_kind: "", working_dir: "/trees/t-1" }),
    })))
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

    await screen.findByRole("option", { name: "chimera" })
    fireEvent.change(screen.getByLabelText("Agent"), { target: { value: "chimera" } })
    fireEvent.change(screen.getByRole("combobox", { name: "Repository preset" }), { target: { value: "Legato" } })
    const workingDir = screen.getByLabelText("Working directory") as HTMLInputElement
    expect(workingDir.value).toBe("/projects/legato")
    fireEvent.change(workingDir, { target: { value: "/projects/task-worktree" } })
    fireEvent.click(screen.getAllByRole("button", { name: "Open agent" })[1])

    await vi.waitFor(() => expect(select).toHaveBeenCalledWith("T-1"))
    expect(apiFetch).toHaveBeenCalledWith("", "/api/tasks/T-1/agent", expect.objectContaining({
      method: "POST",
      body: JSON.stringify({ agent_kind: "chimera", working_dir: "/projects/task-worktree" }),
    }))
  })
})
