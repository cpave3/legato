import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { EngramBindingsSettings } from "../EngramBindingsSettings"

const { apiFetch } = vi.hoisted(() => ({ apiFetch: vi.fn() }))
vi.mock("../../lib/api", () => ({ apiFetch }))

const found = {
  status: { enabled: true, found: true, binary: "/opt/engram/bin/engram", version: "v0.2.0", store: "/home/me/.local/share/engram", engrams: [] },
  places: [
    { key: "dir:/home/me/notes", label: "/home/me/notes", kind: "folder", engrams: [{ name: "mneme" }] },
    { key: "repo:github.com/cpave3/legato", label: "github.com/cpave3/legato", kind: "repo", engrams: [{ name: "argus", default: true }, { name: "ghost", missing: true }] },
  ],
}

function json(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status }))
}

function serve(view: unknown) {
  apiFetch.mockImplementation((_: string, path: string, init?: RequestInit) => {
    if ((init?.method ?? "GET") === "GET" && path === "/api/engram-bindings") return json(view)
    return Promise.resolve(new Response(null, { status: 204 }))
  })
}

function calls(method: string, prefix: string) {
  return apiFetch.mock.calls.filter(([, path, init]) => (path as string).startsWith(prefix) && (init?.method ?? "GET") === method)
}

describe("EngramBindingsSettings", () => {
  beforeEach(() => apiFetch.mockReset())
  afterEach(cleanup)

  it("lists each place by its key without the prefix, with its engrams and the default marked", async () => {
    serve(found)
    render(<EngramBindingsSettings baseUrl="" />)
    const repo = await screen.findByRole("listitem", { name: "github.com/cpave3/legato" })
    expect(within(repo).getByText("argus")).toBeTruthy()
    expect(within(repo).getByText("default")).toBeTruthy()
    expect(within(repo).getByText("missing")).toBeTruthy()
    expect(screen.getByRole("listitem", { name: "/home/me/notes" })).toBeTruthy()
    expect(screen.getByText(/\/opt\/engram\/bin\/engram/)).toBeTruthy()
  })

  it("says when engram was not found, and still lists the bindings", async () => {
    serve({ ...found, status: { enabled: false, found: false, error: "engram was not found: install it, or set engram.command", engrams: [] } })
    render(<EngramBindingsSettings baseUrl="" />)
    expect(await screen.findByText(/engram was not found/)).toBeTruthy()
    expect(screen.getByRole("listitem", { name: "github.com/cpave3/legato" })).toBeTruthy()
  })

  it("unbinds an engram, makes another the default, and clears the default", async () => {
    serve(found)
    render(<EngramBindingsSettings baseUrl="" />)
    const repo = await screen.findByRole("listitem", { name: "github.com/cpave3/legato" })
    fireEvent.click(within(repo).getByRole("button", { name: "Make ghost the default" }))
    await vi.waitFor(() => expect(calls("PUT", "/api/engram-bindings/default")).toHaveLength(1))
    expect(JSON.parse(calls("PUT", "/api/engram-bindings/default")[0][2].body)).toEqual({ place: "repo:github.com/cpave3/legato", engram: "ghost" })

    fireEvent.click(within(repo).getByRole("button", { name: "Clear the default" }))
    await vi.waitFor(() => expect(calls("PUT", "/api/engram-bindings/default")).toHaveLength(2))
    expect(JSON.parse(calls("PUT", "/api/engram-bindings/default")[1][2].body)).toEqual({ place: "repo:github.com/cpave3/legato", engram: null })

    fireEvent.click(within(repo).getByRole("button", { name: "Unbind argus" }))
    await vi.waitFor(() => expect(calls("DELETE", "/api/engram-bindings")).toHaveLength(1))
    expect(calls("DELETE", "/api/engram-bindings")[0][1]).toBe("/api/engram-bindings?place=repo%3Agithub.com%2Fcpave3%2Flegato&engram=argus")
  })

  it("says so when nothing is bound", async () => {
    serve({ ...found, places: [] })
    render(<EngramBindingsSettings baseUrl="" />)
    expect(await screen.findByText(/No engram is bound yet/)).toBeTruthy()
  })
})
