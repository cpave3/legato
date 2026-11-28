import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { QueryClientProvider } from "@tanstack/react-query"
import { RepositoriesSettings } from "../RepositoriesSettings"
import { createQueryClient } from "../../lib/query"

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
}

type Stored = { id?: number; name: string; repo: string; primary_dir: string; base_branch?: string; configured: boolean; clone_problem?: string
  cache?: { repo: string; state: string; percent?: number; received?: string; rate?: string; error?: string; fetched_at?: string } }
let stored: Stored[]
let requests: Array<{ method: string; url: string; body?: Record<string, unknown> }>

beforeEach(() => {
  stored = [
    { name: "Legato", repo: "cpave3/legato", primary_dir: "", base_branch: "main", configured: true,
      cache: { repo: "cpave3/legato", state: "copying", phase: "Receiving objects", percent: 38, received: "310.00 MiB", rate: "3.00 MiB/s" } as Stored["cache"] },
    { id: 4, name: "Widget", repo: "acme/widget", primary_dir: "", configured: false,
      clone_problem: "acme/widget is not one of the repositories of github (settings.repositories in config.yaml; acme/* lists every repository of acme)" },
  ]
  requests = []
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    const method = init?.method ?? "GET"
    const body = init?.body ? JSON.parse(String(init.body)) : undefined
    requests.push({ method, url, body })
    if (method === "GET") return json({ repositories: stored })
    if (method === "POST") {
      if (body.name === "Legato") return json({ error: "invalid repository: Legato is already a repository of config.yaml" }, 400)
      const created = { id: 9, configured: false, ...body }
      stored = [...stored, created]
      return json(created, 201)
    }
    if (method === "DELETE") {
      stored = stored.filter((r) => !url.endsWith(`/${r.id}`))
      return new Response(null, { status: 204 })
    }
    return json({ error: "nope" }, 400)
  }))
})

function renderWith(ui: React.ReactElement) {
  return render(<QueryClientProvider client={createQueryClient()}>{ui}</QueryClientProvider>)
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("RepositoriesSettings", () => {
  it("lists the repositories, leaves the configured ones to the configuration, and says which cannot be cloned", async () => {
    renderWith(<RepositoriesSettings baseUrl="" />)

    const legato = await screen.findByRole("listitem", { name: "Legato" })
    expect(within(legato).getByText("config.yaml")).toBeTruthy()
    expect(within(legato).queryByRole("button", { name: /Remove/ })).toBeNull()
    const widget = screen.getByRole("listitem", { name: "Widget" })
    expect(within(widget).getByRole("button", { name: "Remove Widget" })).toBeTruthy()
    expect(within(widget).getByText(/Can't be cloned/)).toBeTruthy()
    expect(within(widget).getByText(/acme\/\* lists every repository/)).toBeTruthy()
    expect(within(legato).queryByText(/Can't be cloned/)).toBeNull()
  })

  it("adds a repository and says so, so the dialogs can offer it", async () => {
    const onChanged = vi.fn()
    renderWith(<RepositoriesSettings baseUrl="" onChanged={onChanged} />)
    fireEvent.click(await screen.findByRole("button", { name: /Add repository/ }))
    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "Chimera" } })
    fireEvent.change(screen.getByRole("textbox", { name: "GitHub repository" }), { target: { value: "cpave3/chimera" } })
    fireEvent.change(screen.getByRole("textbox", { name: "Base branch" }), { target: { value: "main" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))

    await screen.findByRole("listitem", { name: "Chimera" })
    expect(requests.find((r) => r.method === "POST")?.body).toEqual({ name: "Chimera", repo: "cpave3/chimera", base_branch: "main", primary_dir: "" })
    expect(onChanged).toHaveBeenCalled()
  })

  it("shows why a repository was not added and keeps the form", async () => {
    renderWith(<RepositoriesSettings baseUrl="" />)
    fireEvent.click(await screen.findByRole("button", { name: /Add repository/ }))
    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "Legato" } })
    fireEvent.change(screen.getByRole("textbox", { name: "GitHub repository" }), { target: { value: "cpave3/other" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))

    expect((await screen.findByRole("alert")).textContent).toMatch(/already a repository of config.yaml/)
    expect((screen.getByRole("textbox", { name: "Name" }) as HTMLInputElement).value).toBe("Legato")
  })

  it("removes a repository added here after asking", async () => {
    const onChanged = vi.fn()
    renderWith(<RepositoriesSettings baseUrl="" onChanged={onChanged} />)
    fireEvent.click(await screen.findByRole("button", { name: "Remove Widget" }))
    fireEvent.click(screen.getByRole("button", { name: "Remove" }))

    await waitFor(() => expect(screen.queryByRole("listitem", { name: "Widget" })).toBeNull())
    expect(requests.some((r) => r.method === "DELETE" && r.url.endsWith("/api/repositories/4"))).toBe(true)
    expect(onChanged).toHaveBeenCalled()
  })

  it("says how each repository's copy stands", async () => {
    stored.push({ id: 7, name: "Broken", repo: "acme/broken", primary_dir: "", configured: false, cache: { repo: "acme/broken", state: "failed", error: "repository not found" } })
    stored.push({ id: 5, name: "Gadget", repo: "acme/gadget", primary_dir: "", configured: false, cache: { repo: "acme/gadget", state: "ready", fetched_at: "2026-11-03T01:00:00Z" } })
    stored.push({ id: 6, name: "Notes", repo: "", primary_dir: "/srv/notes", configured: false })
    renderWith(<RepositoriesSettings baseUrl="" />)
    const legato = await screen.findByRole("listitem", { name: "Legato" })
    expect(await within(legato).findByText(/Copying · Receiving objects · 38% · 310.00 MiB · 3.00 MiB\/s/)).toBeTruthy()
    expect(within(legato).getByRole("progressbar").getAttribute("aria-valuenow")).toBe("38")
    expect(within(screen.getByRole("listitem", { name: "Broken" })).getByText(/Copy failed: repository not found/)).toBeTruthy()
    // One the credential does not list says that instead.
    expect(within(screen.getByRole("listitem", { name: "Widget" })).queryByText(/Cop/)).toBeNull()
    expect(within(screen.getByRole("listitem", { name: "Gadget" })).getByText(/^Copied/)).toBeTruthy()
    expect(within(screen.getByRole("listitem", { name: "Notes" })).queryByText(/Cop/)).toBeNull()
  })

  it("names the base branch and folder, and copies not yet made or being fetched", async () => {
    stored = [
      { id: 1, name: "Fresh", repo: "acme/fresh", primary_dir: "/srv/fresh", base_branch: "dev", configured: false, cache: { repo: "acme/fresh", state: "missing" } },
      { id: 2, name: "Busy", repo: "acme/busy", primary_dir: "", configured: false, cache: { repo: "acme/busy", state: "fetching" } },
      { id: 3, name: "Stale", repo: "acme/stale", primary_dir: "", configured: false, cache: { repo: "acme/stale", state: "ready", fetched_at: "not a date" } },
    ]
    renderWith(<RepositoriesSettings baseUrl="" />)
    const fresh = await screen.findByRole("listitem", { name: "Fresh" })
    expect(within(fresh).getByText("from dev")).toBeTruthy()
    expect(within(fresh).getByTitle("/srv/fresh").textContent).toBe("/srv/fresh")
    expect(within(fresh).getByText("Not copied yet")).toBeTruthy()
    const busy = screen.getByRole("listitem", { name: "Busy" })
    expect(within(busy).getByText("Fetching")).toBeTruthy()
    expect(within(busy).queryByRole("progressbar")).toBeNull()
    expect(within(screen.getByRole("listitem", { name: "Stale" })).getByText("Copied").textContent).toBe("Copied")
  })

  it("keeps a repository when removing it is not confirmed", async () => {
    renderWith(<RepositoriesSettings baseUrl="" />)
    fireEvent.click(await screen.findByRole("button", { name: "Remove Widget" }))
    fireEvent.click(screen.getByRole("button", { name: "Keep" }))
    expect(screen.getByRole("button", { name: "Remove Widget" })).toBeTruthy()
    expect(requests.some((r) => r.method === "DELETE")).toBe(false)
  })

  it("shows why a repository was not removed", async () => {
    stored = [{ id: 8, name: "Gone", repo: "acme/gone", primary_dir: "", configured: false }]
    renderWith(<RepositoriesSettings baseUrl="" />)
    vi.mocked(fetch).mockImplementationOnce(async () => json({ error: "in use" }, 409))
    fireEvent.click(await screen.findByRole("button", { name: "Remove Gone" }))
    fireEvent.click(screen.getByRole("button", { name: "Remove" }))
    expect((await screen.findByRole("alert")).textContent).toMatch(/in use/)
    expect(screen.getByRole("button", { name: "Keep" })).toBeTruthy()
  })

  it("saves only with a name and a repository or a folder, trimmed", async () => {
    renderWith(<RepositoriesSettings baseUrl="" />)
    fireEvent.click(await screen.findByRole("button", { name: /Add repository/ }))
    const save = screen.getByRole("button", { name: "Save" })
    expect(save).toHaveProperty("disabled", true)
    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: " Notes " } })
    expect(save).toHaveProperty("disabled", true)
    fireEvent.change(screen.getByRole("textbox", { name: "Folder on this server" }), { target: { value: " /srv/notes " } })
    expect(save).toHaveProperty("disabled", false)
    fireEvent.click(save)
    await screen.findByRole("listitem", { name: "Notes" })
    expect(requests.find((r) => r.method === "POST")?.body).toEqual({ name: "Notes", repo: "", base_branch: "", primary_dir: "/srv/notes" })
    expect(screen.queryByRole("textbox", { name: "Name" })).toBeNull()
  })

  it("cancels a new repository and its error", async () => {
    renderWith(<RepositoriesSettings baseUrl="" />)
    fireEvent.click(await screen.findByRole("button", { name: /Add repository/ }))
    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "Legato" } })
    fireEvent.change(screen.getByRole("textbox", { name: "GitHub repository" }), { target: { value: "x/y" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await screen.findByRole("alert")
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    expect(screen.queryByRole("alert")).toBeNull()
    expect(screen.queryByRole("textbox", { name: "Name" })).toBeNull()
    expect(screen.getByRole("button", { name: /Add repository/ })).toBeTruthy()
  })

  it("shows a failed read and lists nothing", async () => {
    vi.mocked(fetch).mockImplementation(async () => json({ error: "offline" }, 500))
    renderWith(<RepositoriesSettings baseUrl="" />)
    expect((await screen.findByRole("alert")).textContent).toMatch(/offline/)
    expect(screen.queryByRole("list")).toBeNull()
  })
})
