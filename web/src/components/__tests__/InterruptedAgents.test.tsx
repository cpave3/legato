import { QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { createQueryClient } from "../../lib/query"
import { InterruptedAgents } from "../InterruptedAgents"

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
}

let interrupted: Array<Record<string, unknown>>
let posted: Array<{ url: string; body?: string }>
let failResume: string | null

beforeEach(() => {
  interrupted = [
    { task_id: "T-1", title: "Fix the sync", agent_kind: "claude-code", working_dir: "/src/app", resources: [], resumable: true, can_start_new: false, sandboxed: true, ephemeral: false },
    { task_id: "T-2", title: "Write the docs", agent_kind: "codex", resources: [], resumable: false, can_start_new: true, reason: "it never reported its conversation, so there is nothing to resume", sandboxed: false, ephemeral: false },
    { task_id: "T-3", title: "Swarm worker", agent_kind: "claude-code", resources: [], resumable: false, can_start_new: false, reason: "a swarm runs this agent; it starts its agents itself", sandboxed: true, ephemeral: false },
  ]
  posted = []
  failResume = null
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method === "POST") {
      posted.push({ url, body: init.body as string | undefined })
      if (url === "/api/agents/interrupted/resume-all") {
        interrupted = interrupted.filter((a) => !a.resumable)
        return json({ results: [{ task_id: "T-1" }] })
      }
      const id = decodeURIComponent(url.split("/")[3])
      if (url.endsWith("/resume") && failResume) return json({ error: failResume }, 409)
      interrupted = interrupted.filter((a) => a.task_id !== id)
      return json({ task_id: id })
    }
    if (url === "/api/agents/interrupted") return json({ agents: interrupted })
    return json({}, 404)
  }))
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function renderIt() {
  render(<QueryClientProvider client={createQueryClient()}><InterruptedAgents /></QueryClientProvider>)
}

describe("InterruptedAgents", () => {
  it("shows nothing when no agent was interrupted", async () => {
    interrupted = []
    renderIt()
    await waitFor(() => expect(vi.mocked(fetch)).toHaveBeenCalled())
    expect(screen.queryByText(/Interrupted/)).toBeNull()
  })

  it("lists each interrupted agent with what can be done with it", async () => {
    renderIt()
    expect(await screen.findByText("Interrupted · 3")).toBeTruthy()

    const sync = screen.getByRole("listitem", { name: "Fix the sync" })
    expect(within(sync).getByRole("button", { name: "Resume" })).toBeTruthy()
    expect(within(sync).getByRole("button", { name: "Dismiss" })).toBeTruthy()

    const docs = screen.getByRole("listitem", { name: "Write the docs" })
    expect(within(docs).queryByRole("button", { name: "Resume" })).toBeNull()
    expect(within(docs).getByRole("button", { name: "Start new" })).toBeTruthy()
    expect(within(docs).getByText(/never reported its conversation/)).toBeTruthy()

    const worker = screen.getByRole("listitem", { name: "Swarm worker" })
    expect(within(worker).queryByRole("button", { name: "Resume" })).toBeNull()
    expect(within(worker).queryByRole("button", { name: "Start new" })).toBeNull()
    expect(within(worker).getByText(/a swarm runs this agent/)).toBeTruthy()
  })

  it("resumes one, starts a new one in place of another, and dismisses a third", async () => {
    renderIt()
    fireEvent.click(within(await screen.findByRole("listitem", { name: "Fix the sync" })).getByRole("button", { name: "Resume" }))
    await waitFor(() => expect(posted[0]).toEqual({ url: "/api/agents/T-1/resume", body: undefined }))
    await waitFor(() => expect(screen.queryByRole("listitem", { name: "Fix the sync" })).toBeNull())

    fireEvent.click(within(screen.getByRole("listitem", { name: "Write the docs" })).getByRole("button", { name: "Start new" }))
    await waitFor(() => expect(posted[1]).toEqual({ url: "/api/agents/T-2/resume", body: JSON.stringify({ fresh: true }) }))

    fireEvent.click(within(await screen.findByRole("listitem", { name: "Swarm worker" })).getByRole("button", { name: "Dismiss" }))
    await waitFor(() => expect(posted[2]?.url).toBe("/api/agents/T-3/dismiss"))
    await waitFor(() => expect(screen.queryByText(/Interrupted/)).toBeNull())
  })

  it("says why a resume failed", async () => {
    failResume = "interrupted agent cannot be resumed: its folder /src/app is gone"
    renderIt()
    fireEvent.click(within(await screen.findByRole("listitem", { name: "Fix the sync" })).getByRole("button", { name: "Resume" }))
    expect((await screen.findByRole("alert")).textContent).toContain("its folder /src/app is gone")
  })

  it("resumes every resumable agent at once", async () => {
    renderIt()
    fireEvent.click(await screen.findByRole("button", { name: "Resume all" }))
    await waitFor(() => expect(posted.map((p) => p.url)).toEqual(["/api/agents/interrupted/resume-all"]))
    expect(await screen.findByText("Interrupted · 2")).toBeTruthy()
  })
})
