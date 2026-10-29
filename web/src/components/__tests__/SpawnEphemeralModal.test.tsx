import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { SpawnEphemeralModal } from "../SpawnEphemeralModal"

const { apiFetch } = vi.hoisted(() => ({ apiFetch: vi.fn() }))

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
const { settingsData } = vi.hoisted(() => ({
  settingsData: { working_dir: "/default", sandbox_enabled: false, repositories: [{ name: "Legato", repo: "cpave3/legato", primary_dir: "/projects/legato" }] },
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
})
