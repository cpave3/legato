import { QueryClientProvider } from "@tanstack/react-query"
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { createQueryClient } from "../../lib/query"
import type { SessionDetail, SessionEntry } from "../../lib/sessionHistory"
import { SessionHistoryPage } from "../SessionHistoryPage"

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "", activeServerName: "Local" }) }))

const ended: SessionEntry = {
  id: 7, agent_kind: "claude-code", provider_session_id: "conv-1", task_id: "bk50d3h5", task_title: "scn testing gh app",
  task_exists: true, task_archived: false, slot: "main", ephemeral: false, reeve: false, cwd: "/home/me/scn",
  repo: "acme/scn", branch: "gh-app", state: "ended", end_reason: "dismissed",
  first_message: "set up the <b>GitHub</b> app", last_message: "The app is installed.",
  first_seen_at: "2026-11-20T10:00:00Z", last_seen_at: "2026-11-20T12:00:00Z", run_count: 2,
  source: "recorded", transcript_state: "unknown", restorable: true, can_start_fresh: false,
}
const recovered: SessionEntry = { ...ended, id: 8, task_id: "t-2", task_title: "Older work", source: "recovered", first_message: "", last_message: "" }
const detail: SessionDetail = { ...ended, runs: [{ execution_id: "ex-1", started_at: "2026-11-20T10:00:00Z", ended_at: "2026-11-20T12:00:00Z", end_reason: "dismissed", sandboxed: true, resources: [] }] }

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
}

function Where() {
  const location = useLocation()
  return <p data-testid="where">{location.pathname + location.search}</p>
}

function page(path = "/agents/history") {
  return render(<QueryClientProvider client={createQueryClient()}>
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/agents/history" element={<SessionHistoryPage />} />
        <Route path="/agents" element={<Where />} />
      </Routes>
    </MemoryRouter>
  </QueryClientProvider>)
}

let restoreAnswer: Response
beforeEach(() => {
  restoreAnswer = json({ status: "ok" })
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    if (url.includes("/restore")) return restoreAnswer
    if (init?.method === "DELETE") return json({ status: "ok" })
    if (/\/api\/sessions\/\d+$/.test(url)) return json(detail)
    return json({ sessions: [ended, recovered] })
  }))
})
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks() })

function urls() {
  return vi.mocked(fetch).mock.calls.map(([input]) => String(input))
}

it("lists sessions with where they ran, how they ended and what was said", async () => {
  page()
  const row = await screen.findByRole("listitem", { name: "scn testing gh app" })
  expect(within(row).getByText("Ended · dismissed")).toBeTruthy()
  expect(within(row).getByText("acme/scn · gh-app")).toBeTruthy()
  expect(within(row).getByText("The app is installed.")).toBeTruthy()
  expect(within(screen.getByRole("listitem", { name: "Older work" })).getByText("found by folder and time")).toBeTruthy()
})

it("searches as one types, after a pause, and keeps the task filter of the address", async () => {
  page("/agents/history?task=bk50d3h5")
  await screen.findByRole("listitem", { name: "scn testing gh app" })
  expect(urls().some((u) => u.includes("task=bk50d3h5"))).toBe(true)
  fireEvent.change(screen.getByRole("searchbox", { name: "Search sessions" }), { target: { value: "gh app" } })
  await waitFor(() => expect(urls().some((u) => u.includes("q=gh+app") && u.includes("task=bk50d3h5"))).toBe(true))
})

it("shows a session's messages as text, its runs, and opens the agent once restored", async () => {
  page("/agents/history?session=7")
  const panel = await screen.findByRole("complementary", { name: "Session" })
  expect(await within(panel).findByText("set up the <b>GitHub</b> app")).toBeTruthy()
  expect(within(panel).getByText("Runs · 1")).toBeTruthy()
  fireEvent.click(within(panel).getByRole("button", { name: "Restore" }))
  expect((await screen.findByTestId("where")).textContent).toBe("/agents?task=bk50d3h5")
  const restore = vi.mocked(fetch).mock.calls.find(([input]) => String(input).endsWith("/api/sessions/7/restore"))
  expect(JSON.parse(String(restore?.[1]?.body))).toEqual({ target: "task" })
})

it("says why a restore was refused", async () => {
  restoreAnswer = json({ error: "the task's agent is running: stop it first, or restore into a new task", new_task_possible: true }, 409)
  page("/agents/history?session=7")
  const panel = await screen.findByRole("complementary", { name: "Session" })
  fireEvent.click(await within(panel).findByRole("button", { name: "Restore" }))
  expect((await within(panel).findByRole("alert")).textContent).toContain("the task's agent is running")
})

it("asks before forgetting, saying what is left alone", async () => {
  page("/agents/history?session=7")
  const panel = await screen.findByRole("complementary", { name: "Session" })
  fireEvent.click(await within(panel).findByRole("button", { name: "Forget" }))
  const dialog = screen.getByRole("dialog")
  expect(within(dialog).getByText(/transcript, and the task's Resume, are left as they are/)).toBeTruthy()
  fireEvent.click(within(dialog).getByRole("button", { name: "Forget" }))
  await waitFor(() => expect(vi.mocked(fetch).mock.calls.some(([input, init]) => String(input).endsWith("/api/sessions/7") && init?.method === "DELETE")).toBe(true))
})
