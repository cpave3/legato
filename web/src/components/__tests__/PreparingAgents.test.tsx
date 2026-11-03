import { QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { createQueryClient, queryKeys } from "../../lib/query"
import { PreparingAgents } from "../PreparingAgents"
import type { RepositoryCacheStatus } from "../../lib/preparingAgents"

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
}

let preparing: Array<Record<string, unknown>>
let deleted: string[]

beforeEach(() => {
  preparing = [
    { task_id: "eph1", title: "Fix the flaky test", agent_kind: "claude-code", repo: "acme/widget", started_at: "2026-11-03T01:00:00Z" },
    { task_id: "T-2", title: "Part two", agent_kind: "codex", clone_id: 7, started_at: "2026-11-03T01:01:00Z" },
    { task_id: "T-3", title: "Broken", repo: "acme/gone", started_at: "2026-11-03T01:02:00Z", error: "cloning acme/gone: repository not found" },
  ]
  deleted = []
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method === "DELETE") {
      deleted.push(url)
      preparing = preparing.filter((p) => !url.endsWith(`/${p.task_id}`))
      return new Response(null, { status: 204 })
    }
    if (url === "/api/preparing-agents") return json(preparing)
    if (url === "/api/repositories") return json({ repositories: [
      { name: "Widget", repo: "acme/widget", primary_dir: "", configured: true,
        cache: { repo: "acme/widget", state: "copying", phase: "Receiving objects", percent: 38, received: "310.00 MiB", rate: "3.00 MiB/s" } },
    ] })
    return json({}, 404)
  }))
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function renderWith() {
  const client = createQueryClient()
  render(<QueryClientProvider client={client}><PreparingAgents /></QueryClientProvider>)
  return client
}

describe("PreparingAgents", () => {
  it("shows each agent being started, with its repository's copy", async () => {
    renderWith()
    const fix = await screen.findByRole("listitem", { name: "Fix the flaky test" })
    expect(within(fix).getByText(/Copying acme\/widget/)).toBeTruthy()
    expect(within(fix).getByText(/38% · 310.00 MiB · 3.00 MiB\/s/)).toBeTruthy()
    expect(within(fix).getByRole("progressbar").getAttribute("aria-valuenow")).toBe("38")

    const join = screen.getByRole("listitem", { name: "Part two" })
    expect(within(join).getByText(/Getting its clone ready/)).toBeTruthy()

    const broken = screen.getByRole("listitem", { name: "Broken" })
    expect(within(broken).getByText("cloning acme/gone: repository not found")).toBeTruthy()
    fireEvent.click(within(broken).getByRole("button", { name: "Dismiss" }))
    await waitFor(() => expect(deleted).toEqual(["/api/preparing-agents/T-3"]))
    await waitFor(() => expect(screen.queryByRole("listitem", { name: "Broken" })).toBeNull())
  })

  it("follows the copy as the server reports it", async () => {
    const client = renderWith()
    const fix = await screen.findByRole("listitem", { name: "Fix the flaky test" })
    await within(fix).findByText(/Copying acme\/widget/)
    act(() => {
      client.setQueryData<Record<string, RepositoryCacheStatus>>(queryKeys.repositoryCache(""), (current) => ({
        ...current,
        "acme/widget": { repo: "acme/widget", state: "copying", phase: "Resolving deltas", percent: 90 },
      }))
    })
    expect(await within(fix).findByText(/Resolving deltas · 90%/)).toBeTruthy()
  })

  it("says when the clone is being set up, whatever its copy says", async () => {
    preparing = [{ task_id: "eph1", title: "Fix the flaky test", repo: "acme/widget", started_at: "2026-11-03T01:00:00Z", stage: "setting_up" }]
    renderWith()
    const fix = await screen.findByRole("listitem", { name: "Fix the flaky test" })
    expect(within(fix).getByText(/Running setup/)).toBeTruthy()
    expect(within(fix).queryByText(/Copying/)).toBeNull()
  })

  it("shows nothing when no agent is being started", async () => {
    preparing = []
    const { container } = render(<QueryClientProvider client={createQueryClient()}><PreparingAgents /></QueryClientProvider>)
    await waitFor(() => expect(fetch).toHaveBeenCalled())
    expect(container.textContent).toBe("")
  })
})
