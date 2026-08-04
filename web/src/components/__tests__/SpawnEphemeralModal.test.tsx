import { fireEvent, render, screen } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { SpawnEphemeralModal } from "../SpawnEphemeralModal"

const { apiFetch } = vi.hoisted(() => ({ apiFetch: vi.fn() }))

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useSettings", () => {
  const data = { working_dir: "/default", repositories: [{ name: "Legato", repo: "cpave3/legato", primary_dir: "/projects/legato" }] }
  return { useSettings: () => ({ data }) }
})
vi.mock("../../lib/api", () => ({ apiFetch }))

describe("SpawnEphemeralModal", () => {
  beforeEach(() => apiFetch.mockReset())

  it("prefills from a repository preset and submits a manual override", async () => {
    apiFetch.mockImplementation((_: string, path: string) => Promise.resolve(
      path === "/api/adapters"
        ? new Response(JSON.stringify({ adapters: ["chimera"], default: "chimera" }), { status: 200 })
        : path === "/api/settings"
          ? new Response(JSON.stringify({
              working_dir: "/default",
              repositories: [{ name: "Legato", repo: "cpave3/legato", primary_dir: "/projects/legato" }],
            }), { status: 200 })
          : new Response("{}", { status: 201 }),
    ))

    render(<SpawnEphemeralModal open onClose={() => {}} />)

    const preset = await screen.findByRole("combobox", { name: "Repository preset" })
    fireEvent.change(preset, { target: { value: "Legato" } })
    const workingDir = screen.getByRole("textbox", { name: "Working Directory" }) as HTMLInputElement
    expect(workingDir.value).toBe("/projects/legato")

    fireEvent.change(workingDir, { target: { value: "/projects/legato-worktree" } })
    fireEvent.click(screen.getByRole("button", { name: "Spawn" }))

    await vi.waitFor(() => expect(apiFetch).toHaveBeenCalledWith("", "/api/agents/spawn", expect.objectContaining({
      method: "POST",
      body: JSON.stringify({
        title: "Ephemeral session",
        agent_kind: "",
        working_dir: "/projects/legato-worktree",
      }),
    })))
  })
})
