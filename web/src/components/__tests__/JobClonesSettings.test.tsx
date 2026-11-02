import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { JobClonesSettings } from "../JobClonesSettings"

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
}

let clones: Array<Record<string, unknown>>
let deleted: string[]

beforeEach(() => {
  clones = [
    { id: 3, repo: "cpave3/legato", path: "/data/jobs/ex-1/legato", branch: "stack/two", task_ids: ["t1", "t2"], busy: false, last_used_at: "2026-11-02T10:00:00Z", unpushed: 2 },
    { id: 5, repo: "acme/widget", path: "/data/jobs/ex-2/widget", branch: "fix/x", task_ids: ["t3"], busy: true, last_used_at: "2026-11-02T11:00:00Z", unpushed: 0 },
  ]
  deleted = []
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    if ((init?.method ?? "GET") === "DELETE") {
      deleted.push(url)
      clones = clones.filter((clone) => !url.endsWith(`/${clone.id}`))
      return new Response(null, { status: 204 })
    }
    return json({ clones })
  }))
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("JobClonesSettings", () => {
  it("lists each clone with its branch, tasks and what is unpushed", async () => {
    render(<JobClonesSettings baseUrl="" />)
    const legato = await screen.findByRole("listitem", { name: "cpave3/legato" })
    expect(within(legato).getByText("stack/two")).toBeTruthy()
    expect(within(legato).getByText(/t1, t2/)).toBeTruthy()
    expect(within(legato).getByText(/2 unpushed/)).toBeTruthy()
    const widget = screen.getByRole("listitem", { name: "acme/widget" })
    expect(within(widget).getByText(/agent working/)).toBeTruthy()
    expect((within(widget).getByRole("button", { name: "Remove acme/widget" }) as HTMLButtonElement).disabled).toBe(true)
  })

  it("removes a clone once confirmed, warning of unpushed work", async () => {
    render(<JobClonesSettings baseUrl="" />)
    const legato = await screen.findByRole("listitem", { name: "cpave3/legato" })
    fireEvent.click(within(legato).getByRole("button", { name: "Remove cpave3/legato" }))
    expect(within(legato).getByText(/2 unpushed branch tips are lost/)).toBeTruthy()
    fireEvent.click(within(legato).getByRole("button", { name: "Remove" }))
    await waitFor(() => expect(deleted).toEqual(["/api/job-clones/3"]))
    await waitFor(() => expect(screen.queryByRole("listitem", { name: "cpave3/legato" })).toBeNull())
  })

  it("shows nothing for a server with no clones", async () => {
    clones = []
    const { container } = render(<JobClonesSettings baseUrl="" />)
    await waitFor(() => expect(fetch).toHaveBeenCalled())
    expect(container.textContent).toBe("")
  })
})
