import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { SpawnEphemeralModal } from "../SpawnEphemeralModal"

const { apiFetch, addToast } = vi.hoisted(() => ({ apiFetch: vi.fn(), addToast: vi.fn() }))

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useSettings", () => ({
  useSettings: () => ({ data: { working_dir: "/default", sandbox_enabled: false, fresh_clones_enabled: false, repositories: [] } }),
}))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast }) }))
vi.mock("../../lib/api", () => ({ apiFetch }))

const presets = {
  presets: [{ name: "legato", path: "/home/me/Projects/legato", display: "~/Projects/legato", saved: true }],
  default: { path: "/home/me/Projects/legato", display: "~/Projects/legato" },
}

const enabled = {
  enabled: true,
  engrams: [
    { name: "argus", summary: "Watches pull requests" },
    { name: "mneme", summary: "Keeps the project's notes" },
  ],
}

const bound = {
  enabled: true,
  place: { key: "repo:github.com/cpave3/legato", label: "github.com/cpave3/legato", kind: "repo", default: "argus" },
  engrams: [
    { name: "mneme", summary: "Keeps the project's notes", mark: "last" },
    { name: "argus", summary: "Watches pull requests", mark: "default", bound: true },
    { name: "hestia", summary: "Keeps the house", bound: true },
    { name: "clio", summary: "" },
  ],
  preselect: "mneme",
}

function json(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status }))
}

function serve(engrams: unknown, spawned: unknown = { status: "ok" }) {
  apiFetch.mockImplementation((_: string, path: string, init?: RequestInit) => {
    const key = `${init?.method ?? "GET"} ${path.split("?")[0]}`
    if (key === "GET /api/adapters") return json({ adapters: ["claude-code", "codex"], default: "claude-code" })
    if (key === "GET /api/directories/presets") return json(presets)
    if (key === "GET /api/directories") return json({ query: "", resolved: "", exists: false, base: "", prefix: "", total: 0, items: [] })
    if (key === "GET /api/engrams") return json(engrams)
    if (key === "POST /api/agents/spawn") return json(spawned, 201)
    return json({}, 200)
  })
}

function spawnBodies() {
  return apiFetch.mock.calls
    .filter(([, path, init]) => path === "/api/agents/spawn" && init?.method === "POST")
    .map(([, , init]) => JSON.parse(init.body))
}

async function openModal() {
  render(<SpawnEphemeralModal open onClose={() => {}} />)
  await screen.findByRole("dialog", { name: "Spawn agent" })
  await vi.waitFor(() => expect((screen.getByRole("button", { name: /^Spawn in/ }) as HTMLButtonElement).disabled).toBe(false))
}

describe("the Engram field", () => {
  beforeEach(() => {
    apiFetch.mockReset()
    addToast.mockReset()
  })
  afterEach(cleanup)

  it("is not shown when engram is off or absent", async () => {
    serve({ enabled: false, engrams: [] })
    await openModal()
    expect(screen.queryByRole("combobox", { name: "Engram" })).toBeNull()
  })

  it("lists No engram first, then the engrams, and starts with none", async () => {
    serve(enabled)
    await openModal()
    const select = await screen.findByRole("combobox", { name: "Engram" }) as HTMLSelectElement
    expect([...select.options].map((option) => option.textContent)).toEqual(["No engram", "argus", "mneme"])
    expect(select.value).toBe("")
  })

  it("shows the chosen engram's summary and starts the agent as it", async () => {
    serve(enabled)
    await openModal()
    fireEvent.change(await screen.findByRole("combobox", { name: "Engram" }), { target: { value: "mneme" } })
    expect(screen.getByText("Keeps the project's notes")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: /^Spawn in/ }))
    await vi.waitFor(() => expect(spawnBodies()).toHaveLength(1))
    expect(spawnBodies()[0].engram).toBe("mneme")
  })

  it("sends no engram for a normal session", async () => {
    serve(enabled)
    await openModal()
    await screen.findByRole("combobox", { name: "Engram" })
    fireEvent.click(screen.getByRole("button", { name: /^Spawn in/ }))
    await vi.waitFor(() => expect(spawnBodies()).toHaveLength(1))
    expect(spawnBodies()[0]).not.toHaveProperty("engram")
  })

  it("is not offered for a shell, which takes no launch prompt", async () => {
    serve(enabled)
    await openModal()
    await screen.findByRole("combobox", { name: "Engram" })
    fireEvent.click(screen.getByRole("radio", { name: "Shell" }))
    expect(screen.queryByRole("combobox", { name: "Engram" })).toBeNull()
  })

  it("warns when the engram did not boot", async () => {
    serve(enabled, { status: "ok", engram_warning: "mneme did not boot, so the agent started without it: disk is full" })
    await openModal()
    fireEvent.change(await screen.findByRole("combobox", { name: "Engram" }), { target: { value: "mneme" } })
    fireEvent.click(screen.getByRole("button", { name: /^Spawn in/ }))
    await vi.waitFor(() => expect(addToast).toHaveBeenCalledWith("mneme did not boot, so the agent started without it: disk is full", "error"))
  })

  it("groups this repo's engrams, marks them, and picks the pre-selection", async () => {
    serve(bound)
    await openModal()
    const select = await screen.findByRole("combobox", { name: "Engram" }) as HTMLSelectElement
    await vi.waitFor(() => expect(select.value).toBe("mneme"))
    const groups = [...select.querySelectorAll("optgroup")].map((group) => [group.label, [...group.querySelectorAll("option")].map((option) => option.textContent)])
    expect(groups).toEqual([
      ["This repo", ["mneme · last on this task", "argus · default", "hestia"]],
      ["Other engrams", ["clio"]],
    ])
  })

  it("asks for the engrams of the place the agent starts in", async () => {
    serve(bound)
    await openModal()
    await screen.findByRole("combobox", { name: "Engram" })
    const asked = apiFetch.mock.calls.map(([, path]) => path as string).filter((path) => path.startsWith("/api/engrams"))
    expect(asked.at(-1)).toBe("/api/engrams?dir=%7E%2FProjects%2Flegato")
  })

  it("offers to remember a pick that is not the default, saying what it does", async () => {
    serve(bound)
    await openModal()
    const select = await screen.findByRole("combobox", { name: "Engram" })
    await vi.waitFor(() => expect((select as HTMLSelectElement).value).toBe("mneme"))
    const box = screen.getByRole("checkbox", { name: /Remember for this repo/ }) as HTMLInputElement
    expect(screen.getByText("Bind mneme to github.com/cpave3/legato and make it the default.")).toBeTruthy()
    fireEvent.click(box)
    fireEvent.click(screen.getByRole("button", { name: /^Spawn in/ }))
    await vi.waitFor(() => expect(spawnBodies()).toHaveLength(1))
    expect(spawnBodies()[0]).toMatchObject({ engram: "mneme", remember_engram: true })
  })

  it("does not offer to remember the default, which would change nothing", async () => {
    serve(bound)
    await openModal()
    const select = await screen.findByRole("combobox", { name: "Engram" })
    await vi.waitFor(() => expect((select as HTMLSelectElement).value).toBe("mneme"))
    fireEvent.change(select, { target: { value: "argus" } })
    expect(screen.queryByRole("checkbox", { name: /Remember/ })).toBeNull()
  })

  it("offers to clear the default when No engram is picked", async () => {
    serve(bound)
    await openModal()
    const select = await screen.findByRole("combobox", { name: "Engram" })
    await vi.waitFor(() => expect((select as HTMLSelectElement).value).toBe("mneme"))
    fireEvent.change(select, { target: { value: "" } })
    fireEvent.click(screen.getByRole("checkbox", { name: "Clear this repo's default" }))
    fireEvent.click(screen.getByRole("button", { name: /^Spawn in/ }))
    await vi.waitFor(() => expect(spawnBodies()).toHaveLength(1))
    expect(spawnBodies()[0].remember_engram).toBe(true)
    expect(spawnBodies()[0]).not.toHaveProperty("engram")
  })

  it("unticks Remember when the pick changes", async () => {
    serve(bound)
    await openModal()
    const select = await screen.findByRole("combobox", { name: "Engram" })
    await vi.waitFor(() => expect((select as HTMLSelectElement).value).toBe("mneme"))
    fireEvent.click(screen.getByRole("checkbox", { name: /Remember for this repo/ }))
    fireEvent.change(select, { target: { value: "hestia" } })
    expect((screen.getByRole("checkbox", { name: /Remember for this repo/ }) as HTMLInputElement).checked).toBe(false)
  })

  it("says folder for a place outside git", async () => {
    serve({ ...bound, place: { key: "dir:/notes", label: "/notes", kind: "folder", default: "" } })
    await openModal()
    const select = await screen.findByRole("combobox", { name: "Engram" }) as HTMLSelectElement
    await vi.waitFor(() => expect(select.querySelector("optgroup")?.label).toBe("This folder"))
    expect(screen.getByRole("checkbox", { name: /Remember for this folder/ })).toBeTruthy()
  })
})
