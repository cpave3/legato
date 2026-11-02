import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { SpawnEphemeralModal } from "../SpawnEphemeralModal"

const { apiFetch } = vi.hoisted(() => ({ apiFetch: vi.fn() }))

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
const { settingsData } = vi.hoisted(() => ({
  settingsData: {
    working_dir: "/default", sandbox_enabled: false, fresh_clones_enabled: false,
    repositories: [
      { name: "Legato", repo: "cpave3/legato", primary_dir: "/projects/legato", base_branch: "main" },
      { name: "Autopilot", repo: "autopilot-dev/autopilot", primary_dir: "", base_branch: "develop" },
      { name: "Notes", repo: "", primary_dir: "/projects/notes", base_branch: "" },
    ],
  },
}))
vi.mock("../../hooks/useSettings", () => ({ useSettings: () => ({ data: settingsData }) }))
vi.mock("../../lib/api", () => ({ apiFetch }))

const presets = {
  presets: [
    { name: "autopilot", path: "/home/me/Development/autopilot", display: "~/Development/autopilot", saved: false },
    { name: "legato", path: "/home/me/Projects/legato", display: "~/Projects/legato", saved: true },
  ],
  default: { path: "/home/me/Projects/legato", display: "~/Projects/legato" },
}

const autoMatches = {
  query: "~/Development/auto", resolved: "/home/me/Development/auto", exists: false,
  base: "~/Development/", prefix: "auto", total: 2,
  items: [
    { name: "autopilot", path: "/home/me/Development/autopilot", display: "~/Development/autopilot/", git: true, preset: 1 },
    { name: "automation", path: "/home/me/Development/automation", display: "~/Development/automation/", git: false, preset: 0 },
  ],
}

function json(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status }))
}

// serve answers the modal's requests: adapters, presets, suggestions for the
// one path that has any, and a spawn that works unless told otherwise.
function serve(overrides: Record<string, () => Promise<Response>> = {}) {
  apiFetch.mockImplementation((_: string, path: string, init?: RequestInit) => {
    const key = `${init?.method ?? "GET"} ${path.split("?")[0]}`
    if (overrides[key]) return overrides[key]()
    if (key === "GET /api/job-clones") return json({ clones: [] })
    if (key === "GET /api/adapters") return json({ adapters: ["chimera", "claude-code", "codex"], default: "chimera" })
    if (key === "GET /api/directories/presets") return json(presets)
    if (key === "GET /api/directories") {
      const query = new URLSearchParams(path.split("?")[1]).get("q") ?? ""
      return json(query === autoMatches.query ? autoMatches : { query, resolved: "", exists: false, base: "", prefix: "", total: 0, items: [] })
    }
    if (key === "POST /api/directories/presets") return json({ name: "x", path: "/x", display: "/x", saved: true }, 201)
    return json({ status: "ok" }, 201)
  })
}

function requests(method: string, path: string) {
  return apiFetch.mock.calls.filter(([, called, init]) => called.split("?")[0] === path && (init?.method ?? "GET") === method)
}

async function openModal(onClose = () => {}) {
  render(<SpawnEphemeralModal open onClose={onClose} />)
  const dialog = await screen.findByRole("dialog", { name: "Spawn agent" })
  await screen.findByRole("button", { name: /autopilot/ })
  await vi.waitFor(() => expect((screen.getByRole("button", { name: /^Spawn in/ }) as HTMLButtonElement).disabled).toBe(false))
  return dialog
}

function pathField() {
  return screen.getByRole("combobox", { name: "Path" }) as HTMLInputElement
}

describe("SpawnEphemeralModal", () => {
  beforeEach(() => {
    apiFetch.mockReset()
    serve()
  })
  afterEach(cleanup)

  it("starts in the default directory and spawns there under the default title", async () => {
    await openModal()

    expect(pathField().value).toBe("~/Projects/legato")
    expect(screen.getByRole("button", { name: /legato/, pressed: true })).toBeTruthy()

    fireEvent.click(screen.getByRole("button", { name: "Spawn in legato" }))

    await vi.waitFor(() => expect(requests("POST", "/api/agents/spawn")).toHaveLength(1))
    expect(requests("POST", "/api/agents/spawn")[0][2].body).toBe(JSON.stringify({
      title: "Ephemeral session",
      agent_kind: "",
      working_dir: "~/Projects/legato",
    }))
  })

  it("spawns in the preset that was picked, with the title and agent that were chosen", async () => {
    await openModal()

    fireEvent.change(screen.getByRole("textbox", { name: "Title" }), { target: { value: "legato complete" } })
    fireEvent.click(screen.getByRole("radio", { name: "Codex" }))
    fireEvent.click(screen.getByRole("button", { name: /autopilot/ }))
    expect(pathField().value).toBe("~/Development/autopilot")
    fireEvent.click(screen.getByRole("button", { name: "Spawn in autopilot" }))

    await vi.waitFor(() => expect(requests("POST", "/api/agents/spawn")).toHaveLength(1))
    expect(requests("POST", "/api/agents/spawn")[0][2].body).toBe(JSON.stringify({
      title: "legato complete",
      agent_kind: "codex",
      working_dir: "~/Development/autopilot",
    }))
  })

  it("offers the default adapter first, then the others, then a shell", async () => {
    await openModal()

    const agents = within(screen.getByRole("radiogroup", { name: "Agent" })).getAllByRole("radio")

    expect(agents.map((agent) => agent.textContent)).toEqual(["Chimera default", "Claude Code", "Codex", "Shell"])
    expect(agents[0].getAttribute("aria-checked")).toBe("true")
  })

  it("suggests directories for a typed path and completes it with tab", async () => {
    await openModal()

    fireEvent.focus(pathField())
    fireEvent.change(pathField(), { target: { value: "~/Development/auto" } })

    const list = await screen.findByRole("listbox", { name: "Path suggestions" })
    const options = within(list).getAllByRole("option")
    expect(options.map((option) => option.textContent)).toEqual([
      "~/Development/autopilot/git · preset",
      "~/Development/automation/folder",
    ])

    fireEvent.keyDown(pathField(), { key: "ArrowDown" })
    fireEvent.keyDown(pathField(), { key: "Tab" })
    expect(pathField().value).toBe("~/Development/automation/")

    fireEvent.click(screen.getByRole("button", { name: "Spawn in automation" }))
    await vi.waitFor(() => expect(requests("POST", "/api/agents/spawn")).toHaveLength(1))
    expect(JSON.parse(requests("POST", "/api/agents/spawn")[0][2].body).working_dir).toBe("~/Development/automation/")
  })

  it("keeps the suggestions that still match on screen while the next ones are on their way", async () => {
    await openModal()
    fireEvent.focus(pathField())
    fireEvent.change(pathField(), { target: { value: "~/Development/auto" } })
    await screen.findByRole("listbox", { name: "Path suggestions" })

    // No answer for the longer path has arrived yet.
    fireEvent.change(pathField(), { target: { value: "~/Development/autop" } })

    const options = within(screen.getByRole("listbox", { name: "Path suggestions" })).getAllByRole("option")
    expect(options.map((option) => option.textContent)).toEqual(["~/Development/autopilot/git · preset"])
    fireEvent.keyDown(pathField(), { key: "Tab" })
    expect(pathField().value).toBe("~/Development/autopilot/")
  })

  it("holds the last suggestions, out of reach, until the ones for another directory arrive", async () => {
    await openModal()
    fireEvent.focus(pathField())
    fireEvent.change(pathField(), { target: { value: "~/Development/auto" } })
    await screen.findByRole("listbox", { name: "Path suggestions" })

    fireEvent.change(pathField(), { target: { value: "~/Other/x" } })

    const held = screen.getByRole("listbox", { name: "Path suggestions" })
    expect(held.getAttribute("aria-busy")).toBe("true")
    expect(within(held).getAllByRole("option")).toHaveLength(2)
    fireEvent.keyDown(pathField(), { key: "Tab" })
    expect(pathField().value).toBe("~/Other/x")

    // Nothing is in ~/Other, so the list goes once that is known.
    await vi.waitFor(() => expect(screen.queryByRole("listbox", { name: "Path suggestions" })).toBeNull())
  })

  it("takes a suggestion that is clicked", async () => {
    await openModal()

    fireEvent.focus(pathField())
    fireEvent.change(pathField(), { target: { value: "~/Development/auto" } })
    fireEvent.mouseDown(await screen.findByRole("option", { name: /autopilot/ }))

    expect(pathField().value).toBe("~/Development/autopilot/")
    expect(screen.getByRole("button", { name: /autopilot/, pressed: true })).toBeTruthy()
  })

  it("saves the path as a preset before it spawns, when asked to", async () => {
    await openModal()

    const save = screen.getByRole("checkbox", { name: "Save path as preset" }) as HTMLInputElement
    expect(save.disabled).toBe(true)

    fireEvent.change(pathField(), { target: { value: "~/Development/automation" } })
    expect(save.disabled).toBe(false)
    fireEvent.click(save)
    fireEvent.click(screen.getByRole("button", { name: "Spawn in automation" }))

    await vi.waitFor(() => expect(requests("POST", "/api/agents/spawn")).toHaveLength(1))
    const saved = requests("POST", "/api/directories/presets")
    expect(saved).toHaveLength(1)
    expect(saved[0][2].body).toBe(JSON.stringify({ path: "~/Development/automation" }))
  })

  it("does not spawn when the preset cannot be saved, and says why", async () => {
    serve({ "POST /api/directories/presets": () => json({ error: "~/nowhere is not a directory" }, 400) })
    await openModal()

    fireEvent.change(pathField(), { target: { value: "~/nowhere" } })
    fireEvent.click(screen.getByRole("checkbox", { name: "Save path as preset" }))
    fireEvent.click(screen.getByRole("button", { name: "Spawn in nowhere" }))

    expect((await screen.findByRole("alert")).textContent).toContain("~/nowhere is not a directory")
    expect(requests("POST", "/api/agents/spawn")).toHaveLength(0)
  })

  it("says why an agent was not spawned and stays open", async () => {
    serve({ "POST /api/agents/spawn": () => Promise.resolve(new Response("no such directory", { status: 500 })) })
    const onClose = vi.fn()
    await openModal(onClose)

    fireEvent.click(screen.getByRole("button", { name: "Spawn in legato" }))

    expect((await screen.findByRole("alert")).textContent).toContain("no such directory")
    expect(onClose).not.toHaveBeenCalled()
  })

  it("spawns on ctrl+enter", async () => {
    const onClose = vi.fn()
    const dialog = await openModal(onClose)

    fireEvent.keyDown(dialog, { key: "Enter", ctrlKey: true })

    await vi.waitFor(() => expect(requests("POST", "/api/agents/spawn")).toHaveLength(1))
    await vi.waitFor(() => expect(onClose).toHaveBeenCalledTimes(1))
  })

  it("closes on escape, but not while an agent is being spawned", async () => {
    let answer: (response: Response) => void = () => {}
    serve({ "POST /api/agents/spawn": () => new Promise((resolve) => { answer = resolve }) })
    const onClose = vi.fn()
    const dialog = await openModal(onClose)

    fireEvent.click(screen.getByRole("button", { name: "Spawn in legato" }))
    await vi.waitFor(() => expect(requests("POST", "/api/agents/spawn")).toHaveLength(1))
    fireEvent.keyDown(dialog, { key: "Escape" })
    expect(onClose).not.toHaveBeenCalled()

    answer(new Response("no such directory", { status: 500 }))
    await screen.findByRole("alert")
    fireEvent.keyDown(dialog, { key: "Escape" })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it("offers the configured repositories when the server has no directory presets", async () => {
    serve({
      "GET /api/directories/presets": () => json({ error: "directory service not available" }, 503),
    })
    render(<SpawnEphemeralModal open onClose={() => {}} />)

    fireEvent.click(await screen.findByRole("button", { name: /Legato/ }))
    expect(pathField().value).toBe("/projects/legato")
    expect((screen.getByRole("checkbox", { name: "Save path as preset" }) as HTMLInputElement).disabled).toBe(true)

    fireEvent.change(pathField(), { target: { value: "/projects/legato-worktree" } })
    fireEvent.click(screen.getByRole("button", { name: "Spawn in legato-worktree" }))

    await vi.waitFor(() => expect(requests("POST", "/api/agents/spawn")).toHaveLength(1))
    expect(JSON.parse(requests("POST", "/api/agents/spawn")[0][2].body).working_dir).toBe("/projects/legato-worktree")
    expect(requests("GET", "/api/directories")).toHaveLength(0)
  })

  it("keeps the presets past the third behind a row on a phone", async () => {
    serve({
      "GET /api/directories/presets": () => json({
        ...presets,
        presets: [
          ...presets.presets,
          { name: "chimera", path: "/home/me/Projects/chimera", display: "~/Projects/chimera", saved: false },
          { name: "styles", path: "/home/me/Development/styles", display: "~/Development/styles", saved: false },
        ],
      }),
    })
    await openModal()

    const more = screen.getByRole("button", { name: "1 more · Custom path" })
    expect(more.getAttribute("aria-expanded")).toBe("false")

    fireEvent.click(more)

    expect(screen.queryByRole("button", { name: "1 more · Custom path" })).toBeNull()
    expect(screen.getByRole("button", { name: /styles/ })).toBeTruthy()
  })

  it("gives the agent the resources that were picked", async () => {
    serve({
      "GET /api/resources": () => json({ resources: [
        { name: "Obsidian vault", configured: true, paths: [{ path: "/home/me/My Vault", access: "write" }] },
        { id: 2, name: "Specs", configured: false, paths: [{ path: "/srv/specs", access: "read" }] },
      ] }),
    })
    await openModal()

    const vault = await screen.findByRole("checkbox", { name: /Obsidian vault/ })
    fireEvent.click(vault)
    expect((vault as HTMLInputElement).checked).toBe(true)
    fireEvent.click(screen.getByRole("button", { name: "Spawn in legato" }))

    await vi.waitFor(() => expect(requests("POST", "/api/agents/spawn")).toHaveLength(1))
    expect(JSON.parse(requests("POST", "/api/agents/spawn")[0][2].body)).toEqual({
      title: "Ephemeral session",
      agent_kind: "",
      working_dir: "~/Projects/legato",
      resources: ["Obsidian vault"],
    })
  })

  it("sends a first message when one is written, as it was written", async () => {
    await openModal()

    const message = screen.getByRole("textbox", { name: /First message/ })
    fireEvent.change(message, { target: { value: "  Find why the sync test is flaky.\nStart with the logs.  " } })
    fireEvent.keyDown(message, { key: "Enter" })
    expect(requests("POST", "/api/agents/spawn")).toHaveLength(0)
    fireEvent.keyDown(message, { key: "Enter", ctrlKey: true })

    await vi.waitFor(() => expect(requests("POST", "/api/agents/spawn")).toHaveLength(1))
    expect(JSON.parse(requests("POST", "/api/agents/spawn")[0][2].body).prompt).toBe("Find why the sync test is flaky.\nStart with the logs.")
  })

  it("sends no first message when none is written", async () => {
    await openModal()
    fireEvent.change(screen.getByRole("textbox", { name: /First message/ }), { target: { value: "   " } })
    fireEvent.click(screen.getByRole("button", { name: "Spawn in legato" }))

    await vi.waitFor(() => expect(requests("POST", "/api/agents/spawn")).toHaveLength(1))
    expect(JSON.parse(requests("POST", "/api/agents/spawn")[0][2].body)).not.toHaveProperty("prompt")
  })

  it("asks nothing about resources when there are none", async () => {
    await openModal()
    expect(screen.queryByRole("group", { name: "Resources" })).toBeNull()
  })

  describe("with Legato's sandbox on", () => {
    beforeEach(() => { settingsData.sandbox_enabled = true })
    afterEach(() => { settingsData.sandbox_enabled = false })

    it("spawns in the sandbox unless asked not to, and asks again each time", async () => {
      await openModal()
      const outside = screen.getByRole("checkbox", { name: /Run without sandbox/ }) as HTMLInputElement
      expect(outside.checked).toBe(false)
      fireEvent.click(outside)
      fireEvent.click(screen.getByRole("button", { name: "Spawn in legato" }))
      await vi.waitFor(() => expect(requests("POST", "/api/agents/spawn")).toHaveLength(1))
      expect(JSON.parse(requests("POST", "/api/agents/spawn")[0][2].body).sandbox).toBe(false)
    })

    it("sends nothing about the sandbox when the box is left alone", async () => {
      await openModal()
      fireEvent.click(screen.getByRole("button", { name: "Spawn in legato" }))
      await vi.waitFor(() => expect(requests("POST", "/api/agents/spawn")).toHaveLength(1))
      expect(JSON.parse(requests("POST", "/api/agents/spawn")[0][2].body).sandbox).toBeUndefined()
    })
  })

  it("offers no sandbox choice when the sandbox is off", async () => {
    await openModal()
    expect(screen.queryByRole("checkbox", { name: /Run without sandbox/ })).toBeNull()
  })

  it("offers no fresh clone unless the server does", async () => {
    await openModal()
    expect(screen.queryByRole("radio", { name: "Clone" })).toBeNull()
  })

  describe("with fresh clones on", () => {
    beforeEach(() => { settingsData.fresh_clones_enabled = true })
    afterEach(() => { settingsData.fresh_clones_enabled = false })

    it("shows clone-loading failures and keeps them visible after a failed spawn", async () => {
      serve({
        "GET /api/job-clones": () => json({ error: "clone store unavailable" }, 503),
        "POST /api/agents/spawn": () => Promise.resolve(new Response("spawn refused", { status: 500 })),
      })
      await openModal()

      expect((await screen.findByRole("alert")).textContent).toBe("Could not load clones: clone store unavailable")
      fireEvent.click(screen.getByRole("button", { name: "Spawn in legato" }))
      await screen.findByText(/spawn refused/)
      expect(screen.getByText("Could not load clones: clone store unavailable").getAttribute("role")).toBe("alert")
    })

    it("spawns in a fresh clone on a new branch from the preset's base branch", async () => {
      await openModal()
      fireEvent.click(screen.getByRole("radio", { name: "Clone" }))

      const repository = screen.getByRole("combobox", { name: "Repository" }) as HTMLSelectElement
      // Only repositories with a GitHub name can be cloned.
      expect(Array.from(repository.options).map((option) => option.textContent)).toEqual(["Legato", "Autopilot"])
      fireEvent.change(repository, { target: { value: "Autopilot" } })
      expect((screen.getByRole("textbox", { name: "Base branch" }) as HTMLInputElement).value).toBe("develop")
      fireEvent.change(screen.getByRole("textbox", { name: "Branch" }), { target: { value: "legato/flaky-sync" } })
      fireEvent.click(screen.getByRole("button", { name: "Spawn in Autopilot" }))

      await vi.waitFor(() => expect(requests("POST", "/api/agents/spawn")).toHaveLength(1))
      expect(JSON.parse(requests("POST", "/api/agents/spawn")[0][2].body)).toEqual({
        title: "Ephemeral session",
        agent_kind: "",
        repo: "autopilot-dev/autopilot",
        ref: "develop",
        branch: "legato/flaky-sync",
      })
    })

    it("leaves the branch to the server when none is typed", async () => {
      await openModal()
      fireEvent.click(screen.getByRole("radio", { name: "Clone" }))
      expect((screen.getByRole("textbox", { name: "Branch" }) as HTMLInputElement).placeholder).toMatch(/legato\//)
      fireEvent.click(screen.getByRole("button", { name: "Spawn in Legato" }))
      await vi.waitFor(() => expect(requests("POST", "/api/agents/spawn")).toHaveLength(1))
      const body = JSON.parse(requests("POST", "/api/agents/spawn")[0][2].body)
      expect(body).toMatchObject({ repo: "cpave3/legato", ref: "main" })
      expect(body.branch).toBeUndefined()
      expect(body.working_dir).toBeUndefined()
    })

    it("offers only a new clone when no clone of the repository exists", async () => {
      await openModal()
      fireEvent.click(screen.getByRole("radio", { name: "Clone" }))
      expect(screen.queryByRole("combobox", { name: "Clone" })).toBeNull()
      expect(screen.getByRole("textbox", { name: "Base branch" })).toBeTruthy()
      expect(screen.queryByRole("alert")).toBeNull()
    })

    it("continues in a clone of the repository on a branch stacked on its current one", async () => {
      serve({ "GET /api/job-clones": () => json({ clones: [
        { id: 3, repo: "cpave3/legato", path: "/data/jobs/ex-1/legato", branch: "stack/one", task_ids: ["t1"], busy: false, last_used_at: "2026-11-02T10:00:00Z", unpushed: 1 },
        { id: 4, repo: "autopilot-dev/autopilot", path: "/data/jobs/ex-3/autopilot", branch: "main", task_ids: ["t4"], busy: false, last_used_at: "2026-11-02T09:00:00Z", unpushed: 0 },
      ] }) })
      await openModal()
      fireEvent.click(screen.getByRole("radio", { name: "Clone" }))

      // The clones listed are those of the repository picked.
      const clone = await screen.findByRole("combobox", { name: "Clone" }) as HTMLSelectElement
      expect(Array.from(clone.options).map((option) => option.textContent)).toEqual(["New clone", "stack/one"])
      fireEvent.change(clone, { target: { value: "3" } })
      // A clone that exists is on its own branch already: no base to pick.
      expect(screen.queryByRole("textbox", { name: "Base branch" })).toBeNull()
      const branch = screen.getByRole("textbox", { name: "Branch" }) as HTMLInputElement
      expect(branch.placeholder).toBe("stay on stack/one")
      fireEvent.change(branch, { target: { value: "stack/two" } })
      fireEvent.click(screen.getByRole("button", { name: "Spawn in Legato" }))

      await vi.waitFor(() => expect(requests("POST", "/api/agents/spawn")).toHaveLength(1))
      expect(JSON.parse(requests("POST", "/api/agents/spawn")[0][2].body)).toEqual({
        title: "Ephemeral session",
        agent_kind: "",
        clone_id: 3,
        branch: "stack/two",
      })
    })

    it("joins a clone an agent is working in on its branch", async () => {
      serve({ "GET /api/job-clones": () => json({ clones: [
        { id: 5, repo: "cpave3/legato", path: "/data/jobs/ex-2/legato", branch: "fix/x", task_ids: ["t2"], busy: true, last_used_at: "2026-11-01T10:00:00Z", unpushed: 0 },
      ] }) })
      await openModal()
      fireEvent.click(screen.getByRole("radio", { name: "Clone" }))
      const clone = await screen.findByRole("combobox", { name: "Clone" }) as HTMLSelectElement
      expect(Array.from(clone.options).map((option) => option.textContent)).toEqual(["New clone", "fix/x · agent working"])
      fireEvent.change(clone, { target: { value: "5" } })
      expect((screen.getByRole("textbox", { name: "Branch" }) as HTMLInputElement).disabled).toBe(true)
      expect(screen.getByText(/An agent is working in this clone/)).toBeTruthy()
      fireEvent.click(screen.getByRole("button", { name: "Spawn in Legato" }))
      await vi.waitFor(() => expect(requests("POST", "/api/agents/spawn")).toHaveLength(1))
      const body = JSON.parse(requests("POST", "/api/agents/spawn")[0][2].body)
      expect(body).toEqual({ title: "Ephemeral session", agent_kind: "", clone_id: 5 })
    })

    it("offers a clone of a repository that is no longer listed under its GitHub name", async () => {
      serve({ "GET /api/job-clones": () => json({ clones: [
        { id: 6, repo: "acme/widget", path: "/data/jobs/ex-4/widget", branch: "main", task_ids: ["t6"], busy: false, last_used_at: "2026-11-01T10:00:00Z", unpushed: 0 },
      ] }) })
      await openModal()
      fireEvent.click(screen.getByRole("radio", { name: "Clone" }))
      const repository = screen.getByRole("combobox", { name: "Repository" }) as HTMLSelectElement
      await vi.waitFor(() => expect(Array.from(repository.options).map((option) => option.textContent)).toEqual(["Legato", "Autopilot", "acme/widget"]))
      fireEvent.change(repository, { target: { value: "acme/widget" } })
      // Legato cannot make a new clone of a repository it does not offer.
      const clone = screen.getByRole("combobox", { name: "Clone" }) as HTMLSelectElement
      expect(Array.from(clone.options).map((option) => option.textContent)).toEqual(["main"])
      fireEvent.click(screen.getByRole("button", { name: "Spawn in acme/widget" }))
      await vi.waitFor(() => expect(requests("POST", "/api/agents/spawn")).toHaveLength(1))
      expect(JSON.parse(requests("POST", "/api/agents/spawn")[0][2].body)).toMatchObject({ clone_id: 6 })
    })

    it("goes back to the folder it had when switched back", async () => {
      await openModal()
      fireEvent.click(screen.getByRole("radio", { name: "Clone" }))
      fireEvent.click(screen.getByRole("radio", { name: "Folder" }))
      expect(pathField().value).toBe("~/Projects/legato")
      fireEvent.click(screen.getByRole("button", { name: "Spawn in legato" }))
      await vi.waitFor(() => expect(requests("POST", "/api/agents/spawn")).toHaveLength(1))
      expect(JSON.parse(requests("POST", "/api/agents/spawn")[0][2].body).working_dir).toBe("~/Projects/legato")
    })
  })
})
