import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { HandOffModal } from "../HandOffModal"
import type { BoardCard } from "../../../lib/board-types"

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
}

const card: BoardCard = {
  id: "T7x2", title: "Fix the flaky login test", priority: "", issue_type: "", status: "Ready", provider: "", has_warning: false,
  agent_active: false, agent_state: "", working_seconds: 0, waiting_seconds: 0, error_seconds: 0,
  workspace_id: null, workspace_name: "", workspace_color: "",
}

const repositories = [
  { name: "widget", repo: "acme/widget", primary_dir: "/src/widget" },
  { name: "notes", repo: "", primary_dir: "/src/notes" },
  { name: "gadget", repo: "acme/gadget", primary_dir: "" },
]

let posts: { url: string; body: Record<string, unknown> }[]
let handOffAnswer: Response | null

beforeEach(() => {
  posts = []
  handOffAnswer = null
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    const u = String(url)
    if (u.endsWith("/api/settings/workers")) {
      return json({
        workers: [
          { instance_id: "srv-bare", address: "https://vm-2.lan:3080", name: "vm-2", main_name: "workstation", sandbox: false, handoff_version: 2, paired_at: "2026-11-03T09:00:00Z" },
          { instance_id: "srv-vm1", address: "https://vm-1.lan:3080", name: "vm-1", main_name: "workstation", sandbox: true, handoff_version: 2, paired_at: "2026-11-03T09:00:00Z" },
        ],
        instance_id: "srv-main", main_name: "workstation",
      })
    }
    if (u.endsWith("/api/adapters")) return json({ adapters: ["claude-code", "codex", "chimera"], default: "codex" })
    if (u.endsWith(`/api/tasks/${card.id}/handoff`) && init?.method === "POST") {
      posts.push({ url: u, body: JSON.parse(String(init.body)) })
      if (handOffAnswer) return handOffAnswer
      return json({ away: { worker_instance_id: "srv-vm1", worker: "vm-1", handoff_id: "ho-1", worker_task_id: "w1", purpose: "find-out", kind: "new-branch", repo: "acme/gadget", branch: "legato/t7x2", left_column: "Ready", handed_off_at: "2026-11-05T09:00:00Z", state: "pending" } }, 201)
    }
    return json({ error: "nope" }, 404)
  }))
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function renderForm(props: Partial<Parameters<typeof HandOffModal>[0]> = {}) {
  const onClose = vi.fn()
  const onHandedOff = vi.fn()
  render(<HandOffModal baseUrl="" open card={card} repositories={repositories} onClose={onClose} onHandedOff={onHandedOff} {...props} />)
  return { onClose, onHandedOff }
}

describe("HandOffModal", () => {
  it("asks for the worker, the purpose, where the work starts, the repository and the agent tool", async () => {
    renderForm()
    const dialog = screen.getByRole("dialog", { name: "Hand off “Fix the flaky login test”" })

    // The workers, with the one without the sandbox refused up front.
    const worker = await within(dialog).findByRole("combobox", { name: "Worker" }) as HTMLSelectElement
    await waitFor(() => expect(worker.value).toBe("srv-vm1"))
    const bare = within(worker).getByRole("option", { name: /vm-2/ }) as HTMLOptionElement
    expect(bare.disabled).toBe(true)
    expect(bare.textContent).toMatch(/sandbox off/i)

    // Purpose, make a change by default.
    expect((within(dialog).getByRole("radio", { name: /Make a change/ }) as HTMLInputElement).checked).toBe(true)
    expect(within(dialog).getByRole("radio", { name: /Find out/ })).toBeTruthy()

    // Where the work starts: a new branch, named after the task.
    const fresh = within(dialog).getByRole("radio", { name: /A new branch/ }) as HTMLInputElement
    expect(fresh.checked).toBe(true)
    expect(within(dialog).getByText("legato/t7x2")).toBeTruthy()

    // The repositories with a GitHub name, as the spawn dialog offers them,
    // after a placeholder: none is chosen for the person.
    const repo = within(dialog).getByRole("combobox", { name: "Repository" }) as HTMLSelectElement
    expect(Array.from(repo.options).map((o) => o.value)).toEqual(["", "acme/widget", "acme/gadget"])
    expect(repo.value).toBe("")
    expect(repo.required).toBe(true)

    // The agent tools, the default first; never a plain shell.
    const tool = await within(dialog).findByRole("combobox", { name: "Agent tool" }) as HTMLSelectElement
    await waitFor(() => expect(tool.options.length).toBe(3))
    expect(Array.from(tool.options).map((o) => o.value)).toEqual(["codex", "claude-code", "chimera"])
  })

  it("hands the task off with what was chosen", async () => {
    const { onHandedOff, onClose } = renderForm()
    const worker = await screen.findByRole("combobox", { name: "Worker" }) as HTMLSelectElement
    await waitFor(() => expect(worker.value).toBe("srv-vm1"))
    fireEvent.click(screen.getByRole("radio", { name: /Find out/ }))
    fireEvent.change(screen.getByRole("combobox", { name: "Repository" }), { target: { value: "acme/gadget" } })
    const tool = screen.getByRole("combobox", { name: "Agent tool" }) as HTMLSelectElement
    await waitFor(() => expect(tool.options.length).toBe(3))
    fireEvent.change(tool, { target: { value: "claude-code" } })
    fireEvent.click(screen.getByRole("button", { name: "Hand off" }))

    await waitFor(() => expect(onHandedOff).toHaveBeenCalledTimes(1))
    expect(posts[0].body).toEqual({ worker: "srv-vm1", purpose: "find-out", kind: "new-branch", repo: "acme/gadget", agent_kind: "claude-code" })
    expect(onHandedOff.mock.calls[0][0].worker).toBe("vm-1")
    expect(onClose).toHaveBeenCalled()
  })

  it("starts from a branch already pushed, named in a field of its own", async () => {
    const { onHandedOff } = renderForm()
    const worker = await screen.findByRole("combobox", { name: "Worker" }) as HTMLSelectElement
    await waitFor(() => expect(worker.value).toBe("srv-vm1"))
    expect(screen.queryByRole("textbox", { name: "Branch" })).toBeNull()

    fireEvent.click(screen.getByRole("radio", { name: /A pushed branch/ }))
    const branch = screen.getByRole("textbox", { name: "Branch" }) as HTMLInputElement
    const send = screen.getByRole("button", { name: "Hand off" }) as HTMLButtonElement
    // Not without the branch's name.
    expect(send.disabled).toBe(true)
    fireEvent.change(branch, { target: { value: "  fix/login-timeout " } })
    fireEvent.change(screen.getByRole("combobox", { name: "Repository" }), { target: { value: "acme/widget" } })
    expect(send.disabled).toBe(false)
    fireEvent.click(send)

    await waitFor(() => expect(onHandedOff).toHaveBeenCalledTimes(1))
    expect(posts[0].body).toEqual({ worker: "srv-vm1", purpose: "make-change", kind: "pushed-branch", branch: "fix/login-timeout", repo: "acme/widget", agent_kind: "codex" })
  })

  it("says why the worker or the main refused, in the server's words", async () => {
    handOffAnswer = json({ error: `vm-1 refused the handoff: this worker does not know the agent tool "codex" (it knows claude-code)` }, 422)
    const { onHandedOff } = renderForm()
    const worker = await screen.findByRole("combobox", { name: "Worker" }) as HTMLSelectElement
    await waitFor(() => expect(worker.value).toBe("srv-vm1"))
    fireEvent.change(screen.getByRole("combobox", { name: "Repository" }), { target: { value: "acme/widget" } })
    fireEvent.click(screen.getByRole("button", { name: "Hand off" }))
    const alert = await screen.findByRole("alert")
    expect(alert.textContent).toBe(`vm-1 refused the handoff: this worker does not know the agent tool "codex" (it knows claude-code)`)
    expect(onHandedOff).not.toHaveBeenCalled()
    expect(screen.getByRole("dialog")).toBeTruthy()
  })

  it("does not hand off until a repository is chosen, and never picks one itself", async () => {
    const { onHandedOff } = renderForm()
    const worker = await screen.findByRole("combobox", { name: "Worker" }) as HTMLSelectElement
    await waitFor(() => expect(worker.value).toBe("srv-vm1"))
    const send = screen.getByRole("button", { name: "Hand off" }) as HTMLButtonElement
    expect(send.disabled).toBe(true)
    expect(screen.getByText(/none is picked for you/i)).toBeTruthy()
    fireEvent.submit(send.closest("form")!)
    expect(posts.length).toBe(0)
    expect(onHandedOff).not.toHaveBeenCalled()

    fireEvent.change(screen.getByRole("combobox", { name: "Repository" }), { target: { value: "acme/gadget" } })
    expect(send.disabled).toBe(false)
  })

  it("does not fill in a repository when the list arrives after the form opened", async () => {
    const view = render(<HandOffModal baseUrl="" open card={card} repositories={[]} onClose={vi.fn()} onHandedOff={vi.fn()} />)
    view.rerender(<HandOffModal baseUrl="" open card={card} repositories={repositories} onClose={vi.fn()} onHandedOff={vi.fn()} />)
    const repo = await screen.findByRole("combobox", { name: "Repository" }) as HTMLSelectElement
    expect(repo.value).toBe("")
  })

  it("lays the choices out to fit a phone: stacked, wrapping, nothing cut off", async () => {
    renderForm()
    const dialog = screen.getByRole("dialog")
    for (const name of [/Make a change/, /A new branch/]) {
      const group = within(dialog).getByRole("radio", { name }).closest("fieldset")!
      const row = group.querySelector("label")!.parentElement!
      expect(row.className).toMatch(/\bflex-col\b/)
      expect(row.className).toMatch(/\bsm:flex-row\b/)
      expect(row.querySelector(".truncate")).toBeNull()
    }
    // The panel scrolls inside the screen instead of running off it.
    expect(dialog.className).toMatch(/max-h-/)
    expect(dialog.querySelector("form")!.className).toMatch(/\boverflow-hidden\b|\bmin-h-0\b/)
  })

  it("says to stop the task's agent here first", async () => {
    renderForm({ card: { ...card, agent_active: true, agent_state: "working" } })
    expect(screen.getByText(/stop this task's agent before handing it off/i)).toBeTruthy()
    expect((screen.getByRole("button", { name: "Hand off" }) as HTMLButtonElement).disabled).toBe(true)
  })

  it("asks for a repository by name when none is offered", async () => {
    renderForm({ repositories: [] })
    const worker = await screen.findByRole("combobox", { name: "Worker" }) as HTMLSelectElement
    await waitFor(() => expect(worker.value).toBe("srv-vm1"))
    const repo = screen.getByRole("textbox", { name: "Repository" }) as HTMLInputElement
    expect(repo.value).toBe("")
    expect((screen.getByRole("button", { name: "Hand off" }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.change(repo, { target: { value: "acme/other" } })
    fireEvent.click(screen.getByRole("button", { name: "Hand off" }))
    await waitFor(() => expect(posts.length).toBe(1))
    expect(posts[0].body.repo).toBe("acme/other")
  })
})
