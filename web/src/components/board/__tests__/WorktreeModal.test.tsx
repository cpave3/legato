import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { WorktreeModal } from "../WorktreeModal"

const { apiFetch } = vi.hoisted(() => ({ apiFetch: vi.fn() }))
vi.mock("../../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "https://legato.test" }) }))
vi.mock("../../../lib/api", () => ({ apiFetch }))

afterEach(() => cleanup())

describe("WorktreeModal", () => {
  it("creates a task worktree with TUI-equivalent defaults", async () => {
    apiFetch.mockResolvedValue(new Response(JSON.stringify({ primary_dir: "/projects/app", path: "/trees/t-1", branch: "t-1", base_branch: "main" }), { status: 201 }))
    const onCreated = vi.fn()
    render(<WorktreeModal open taskId="T_1" defaultPrimaryDir="/projects/app" repositories={[]} onClose={vi.fn()} onCreated={onCreated} />)

    expect((screen.getByLabelText("Primary directory") as HTMLInputElement).value).toBe("/projects/app")
    expect((screen.getByLabelText("Branch") as HTMLInputElement).value).toBe("t-1")
    expect((screen.getByLabelText("Base branch") as HTMLInputElement).value).toBe("main")
    fireEvent.click(screen.getByRole("button", { name: "Create worktree" }))

    await vi.waitFor(() => expect(onCreated).toHaveBeenCalledWith(expect.objectContaining({ path: "/trees/t-1" })))
    expect(apiFetch).toHaveBeenCalledWith("https://legato.test", "/api/tasks/T_1/worktree", expect.objectContaining({
      body: JSON.stringify({ primary_dir: "/projects/app", branch: "t-1", base_branch: "main" }),
    }))
  })

  it("preserves editable values and shows server errors", async () => {
    apiFetch.mockResolvedValue(new Response("yg failed", { status: 500 }))
    render(<WorktreeModal open taskId="T-1" defaultPrimaryDir="/default" repositories={[]} onClose={vi.fn()} onCreated={vi.fn()} />)
    fireEvent.change(screen.getByLabelText("Primary directory"), { target: { value: "/custom" } })
    fireEvent.click(screen.getByRole("button", { name: "Create worktree" }))
    expect((await screen.findByRole("alert")).textContent).toContain("yg failed")
    expect((screen.getByLabelText("Primary directory") as HTMLInputElement).value).toBe("/custom")
    expect((screen.getByRole("button", { name: "Create worktree" }) as HTMLButtonElement).disabled).toBe(false)
  })
})
