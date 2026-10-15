import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { ResourcesSettings } from "../ResourcesSettings"

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
}

let stored: Array<{ id?: number; name: string; configured: boolean; paths: Array<{ path: string; access: string }> }>
let requests: Array<{ method: string; url: string; body?: unknown }>

beforeEach(() => {
  stored = [
    { name: "Obsidian vault", configured: true, paths: [{ path: "/home/me/My Vault", access: "write" }] },
    { id: 3, name: "Specs", configured: false, paths: [{ path: "/srv/specs", access: "read" }] },
  ]
  requests = []
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    const method = init?.method ?? "GET"
    const body = init?.body ? JSON.parse(String(init.body)) : undefined
    requests.push({ method, url, body })
    if (method === "GET") return json({ resources: stored })
    if (method === "POST") {
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

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("ResourcesSettings", () => {
  it("lists the resources, and leaves the configured ones to the configuration", async () => {
    render(<ResourcesSettings baseUrl="" />)

    const vault = await screen.findByRole("listitem", { name: "Obsidian vault" })
    expect(within(vault).getByText("config.yaml")).toBeTruthy()
    expect(within(vault).queryByRole("button", { name: /Remove/ })).toBeNull()
    const specs = screen.getByRole("listitem", { name: "Specs" })
    expect(within(specs).getByRole("button", { name: "Remove Specs" })).toBeTruthy()
  })

  it("saves a new resource with a path that has spaces in it", async () => {
    render(<ResourcesSettings baseUrl="" />)
    await screen.findByRole("listitem", { name: "Specs" })

    fireEvent.click(screen.getByRole("button", { name: "New resource" }))
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Notes" } })
    fireEvent.change(screen.getByLabelText("Path 1"), { target: { value: "~/Documents/Daily Notes" } })
    fireEvent.click(screen.getByRole("radio", { name: "Write" }))
    fireEvent.click(screen.getByRole("button", { name: "Add a path" }))
    fireEvent.change(screen.getByLabelText("Path 2"), { target: { value: "/srv/templates" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))

    await waitFor(() => expect(requests.find((r) => r.method === "POST")?.body).toEqual({
      name: "Notes",
      paths: [{ path: "~/Documents/Daily Notes", access: "write" }, { path: "/srv/templates", access: "read" }],
    }))
    expect(await screen.findByRole("listitem", { name: "Notes" })).toBeTruthy()
  })

  it("removes a saved resource after asking", async () => {
    render(<ResourcesSettings baseUrl="" />)
    fireEvent.click(await screen.findByRole("button", { name: "Remove Specs" }))
    fireEvent.click(screen.getByRole("button", { name: "Remove" }))

    await waitFor(() => expect(screen.queryByRole("listitem", { name: "Specs" })).toBeNull())
    expect(requests.some((r) => r.method === "DELETE" && r.url.endsWith("/api/resources/3"))).toBe(true)
  })

  it("shows why a resource could not be saved", async () => {
    render(<ResourcesSettings baseUrl="" />)
    await screen.findByRole("listitem", { name: "Specs" })
    vi.stubGlobal("fetch", vi.fn(async () => json({ error: "invalid resource: /nowhere does not exist" }, 400)))

    fireEvent.click(screen.getByRole("button", { name: "New resource" }))
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Bad" } })
    fireEvent.change(screen.getByLabelText("Path 1"), { target: { value: "/nowhere" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))

    expect((await screen.findByRole("alert")).textContent).toContain("/nowhere does not exist")
  })
})
