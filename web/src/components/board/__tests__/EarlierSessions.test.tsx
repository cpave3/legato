import { QueryClientProvider } from "@tanstack/react-query"
import { render, screen, waitFor } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, expect, it, vi } from "vitest"
import { createQueryClient } from "../../../lib/query"
import { EarlierSessions } from "../EarlierSessions"

vi.mock("../../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
afterEach(() => { vi.unstubAllGlobals() })

function renderIt(sessions: unknown[]) {
  vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ sessions }), { headers: { "Content-Type": "application/json" } })))
  return render(<QueryClientProvider client={createQueryClient()}><MemoryRouter><EarlierSessions taskId="T-1" /></MemoryRouter></QueryClientProvider>)
}

it("links each of a task's sessions into the history", async () => {
  renderIt([{ id: 4, agent_kind: "codex", state: "ended", end_reason: "stopped", last_seen_at: "2026-11-20T10:00:00Z" }])
  const link = await screen.findByRole("link", { name: /Codex · Ended · stopped/ })
  expect(link.getAttribute("href")).toBe("/agents/history?task=T-1&session=4")
  expect(screen.getByRole("link", { name: "In Session history" }).getAttribute("href")).toBe("/agents/history?task=T-1")
  expect(String(vi.mocked(fetch).mock.calls[0][0])).toContain("task=T-1")
})

it("shows nothing for a task with no session", async () => {
  const { container } = renderIt([])
  await waitFor(() => expect(vi.mocked(fetch)).toHaveBeenCalled())
  expect(container.textContent).toBe("")
})
