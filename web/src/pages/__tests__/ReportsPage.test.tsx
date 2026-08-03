import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { apiFetch } from "../../lib/api"
import { ReportsPage } from "../ReportsPage"

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "https://remote.example" }) }))
vi.mock("../../lib/api", () => ({ apiFetch: vi.fn() }))

const mockedApiFetch = vi.mocked(apiFetch)

function jsonResponse(body: unknown, ok = true): Response {
  return { ok, status: ok ? 200 : 500, json: async () => body, text: async () => typeof body === "string" ? body : JSON.stringify(body) } as Response
}

function reportFixture(overrides: Record<string, unknown> = {}) {
  return {
    report: {
      period: { kind: "week", offset: 0, label: "This Week", start: "2026-08-03T00:00:00+10:00", end: "2026-08-03T14:30:00+10:00" },
      activity_epoch: "2026-08-03T09:00:00+10:00",
      activity_available: true,
      summary: { working_seconds: 10800, waiting_seconds: 1800, tasks_created: 4, tasks_completed: 3, agent_sessions: 7, prs_merged: 2 },
      by_day: [{ date: "2026-08-03", working_seconds: 10800, waiting_seconds: 1800 }],
      by_task: [
        { task_id: "task-1", display_id: "LEG-42", title: "Build reports", workspace_name: "Legato", provider: "jira", working_seconds: 7200, waiting_seconds: 600 },
        { task_id: "task-2", display_id: "task-2", title: "Write docs", workspace_name: "Legato", provider: "", working_seconds: 1800, waiting_seconds: 900 },
      ],
      by_workspace: [{ workspace_id: 1, workspace_name: "Legato", workspace_color: "#6366f1", working_seconds: 9000, waiting_seconds: 1500, task_count: 2 }],
      by_directory: [{ directory: "/home/dev/very/long/path/to/legato", working_seconds: 9000, waiting_seconds: 1500, task_count: 2 }],
      by_swarm: [{ parent_task_id: "task-1", title: "Reporting swarm", working_seconds: 7200, waiting_seconds: 1200, wall_clock_seconds: 3600, parallel_ratio: 2.3, worker_count: 3, subtask_count: 4 }],
      by_execution: [{ id: "ex-1", kind: "workflow_agent", status: "succeeded", task_id: "task-1", task_title: "Build reports", workflow_run_id: "run-1", working_seconds: 7200, waiting_seconds: 600, wall_clock_seconds: 4800, started_at: "2026-08-03T09:00:00+10:00", ended_at: "2026-08-03T10:20:00+10:00" }],
      ...overrides,
    },
    comparison: {
      period: { kind: "week", offset: -1, label: "Week of Jul 27 – Aug 2", start: "2026-07-27T00:00:00+10:00", end: "2026-08-03T00:00:00+10:00" },
      activity_available: true,
      summary: { working_seconds: 7200, waiting_seconds: 2400, tasks_created: 3, tasks_completed: 2, agent_sessions: 6, prs_merged: 1 },
    },
  }
}

function renderPage() {
  return render(<MemoryRouter><ReportsPage /></MemoryRouter>)
}

describe("ReportsPage", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockedApiFetch.mockReset()
    mockedApiFetch.mockResolvedValue(jsonResponse(reportFixture()))
  })
  afterEach(() => cleanup())

  it("loads the remote server's default week and renders summary, comparison, activity, and tasks", async () => {
    renderPage()
    expect(screen.getByRole("status", { name: "Loading report" })).toBeTruthy()
    await screen.findByRole("heading", { name: "This Week" })

    expect(mockedApiFetch).toHaveBeenCalledWith("https://remote.example", "/api/report?period=week&offset=0", expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(screen.getByText("3h")).toBeTruthy()
    expect(screen.getAllByText(/50% vs previous period/).length).toBeGreaterThan(0)
    expect(screen.getByRole("img", { name: /(Aug 3|3 Aug).*3h working.*30m waiting/i })).toBeTruthy()
    expect(screen.getByText("LEG-42")).toBeTruthy()
    expect(screen.getByText("Build reports")).toBeTruthy()
    expect(screen.getByText(/Active time is additive across parallel executions/)).toBeTruthy()
    expect(screen.getByText("Agent active time")).toBeTruthy()
  })

  it("navigates offsets, disables next at current, and resets to current when changing period", async () => {
    renderPage()
    await screen.findByRole("heading", { name: "This Week" })
    expect((screen.getByRole("button", { name: "Next period" }) as HTMLButtonElement).disabled).toBe(true)

    fireEvent.click(screen.getByRole("button", { name: "Previous period" }))
    await waitFor(() => expect(mockedApiFetch).toHaveBeenLastCalledWith("https://remote.example", "/api/report?period=week&offset=-1", expect.anything()))
    expect((screen.getByRole("button", { name: "Next period" }) as HTMLButtonElement).disabled).toBe(false)

    fireEvent.click(screen.getByRole("button", { name: "Month" }))
    await waitFor(() => expect(mockedApiFetch).toHaveBeenLastCalledWith("https://remote.example", "/api/report?period=month&offset=0", expect.anything()))
    expect((screen.getByRole("button", { name: "Next period" }) as HTMLButtonElement).disabled).toBe(true)
  })

  it("ignores a stale navigation response after a newer period loads", async () => {
    let resolvePrevious!: (response: Response) => void
    const previous = new Promise<Response>(resolve => { resolvePrevious = resolve })
    mockedApiFetch
      .mockResolvedValueOnce(jsonResponse(reportFixture()))
      .mockReturnValueOnce(previous)
      .mockResolvedValueOnce(jsonResponse(reportFixture({ period: { kind: "month", offset: 0, label: "This Month", start: "2026-08-01T00:00:00+10:00", end: "2026-08-03T14:30:00+10:00" } })))
    renderPage()
    await screen.findByRole("heading", { name: "This Week" })
    fireEvent.click(screen.getByRole("button", { name: "Previous period" }))
    fireEvent.click(screen.getByRole("button", { name: "Month" }))
    expect(await screen.findByRole("heading", { name: "This Month" })).toBeTruthy()
    resolvePrevious(jsonResponse(reportFixture({ period: { kind: "week", offset: -1, label: "Stale Week", start: "2026-07-27T00:00:00+10:00", end: "2026-08-03T00:00:00+10:00" } })))
    await Promise.resolve()
    expect(screen.queryByRole("heading", { name: "Stale Week" })).toBeNull()
  })

  it("suppresses activity deltas when the comparison predates the reporting epoch", async () => {
    const fixture = reportFixture()
    fixture.comparison.activity_available = false
    mockedApiFetch.mockResolvedValue(jsonResponse(fixture))
    renderPage()
    await screen.findByRole("heading", { name: "This Week" })
    expect(screen.getAllByText("Comparison unavailable before activity history began").length).toBe(3)
  })

  it("does not render Infinity or NaN when comparison values are zero", async () => {
    const fixture = reportFixture()
    fixture.comparison.summary = { working_seconds: 0, waiting_seconds: 0, tasks_created: 0, tasks_completed: 0, agent_sessions: 0, prs_merged: 0 }
    mockedApiFetch.mockResolvedValue(jsonResponse(fixture))
    renderPage()
    await screen.findByRole("heading", { name: "This Week" })
    expect(screen.queryByText(/Infinity|NaN/)).toBeNull()
    expect(screen.getAllByText("— vs previous period").length).toBeGreaterThan(0)
  })

  it("switches counted breakdown tabs, sorts numeric columns, and preserves empty states", async () => {
    renderPage()
    await screen.findByRole("heading", { name: "This Week" })
    expect(screen.getByRole("tab", { name: "Tasks 2" }).getAttribute("aria-selected")).toBe("true")
    const table = screen.getByRole("table", { name: "Tasks breakdown" })
    expect(within(table).getAllByRole("row")[1].textContent).toContain("LEG-42")

    fireEvent.click(within(table).getByRole("button", { name: /Waiting/ }))
    expect(within(table).getAllByRole("row")[1].textContent).toContain("task-2")

    fireEvent.click(screen.getByRole("tab", { name: "Projects 1" }))
    expect(screen.getByTitle("/home/dev/very/long/path/to/legato")).toBeTruthy()

    mockedApiFetch.mockResolvedValue(jsonResponse(reportFixture({ by_swarm: [] })))
    fireEvent.click(screen.getByRole("button", { name: "Month" }))
    await waitFor(() => expect(screen.getByRole("tab", { name: "Swarms 0" })).toBeTruthy())
    fireEvent.click(screen.getByRole("tab", { name: "Swarms 0" }))
    expect(screen.getByText("No swarm activity for this period")).toBeTruthy()

    fireEvent.click(screen.getByRole("tab", { name: "Executions 1" }))
    expect(screen.getByRole("table", { name: "Executions breakdown" })).toBeTruthy()
    expect(screen.getByText("workflow agent")).toBeTruthy()
  })

  it("copies server-rendered Markdown and announces success", async () => {
    mockedApiFetch
      .mockResolvedValueOnce(jsonResponse(reportFixture()))
      .mockResolvedValueOnce({ ok: true, text: async () => "# This Week Report" } as Response)
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } })

    renderPage()
    await screen.findByRole("heading", { name: "This Week" })
    fireEvent.click(screen.getByRole("button", { name: "Copy Markdown" }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith("# This Week Report"))
    expect(mockedApiFetch).toHaveBeenLastCalledWith("https://remote.example", "/api/report?period=week&offset=0&format=markdown")
    expect(screen.getByText("Markdown copied")).toBeTruthy()
  })

  it("surfaces request failures and retries successfully", async () => {
    mockedApiFetch.mockResolvedValueOnce(jsonResponse({ error: "report unavailable" }, false)).mockResolvedValueOnce(jsonResponse(reportFixture()))
    renderPage()
    expect((await screen.findByRole("alert")).textContent).toContain("report unavailable")
    fireEvent.click(screen.getByRole("button", { name: "Retry" }))
    expect(await screen.findByRole("heading", { name: "This Week" })).toBeTruthy()
  })

  it("shows a dedicated no-activity state instead of zero cards", async () => {
    mockedApiFetch.mockResolvedValue(jsonResponse(reportFixture({
      summary: { working_seconds: 0, waiting_seconds: 0, tasks_created: 0, tasks_completed: 0, agent_sessions: 0, prs_merged: 0 },
      by_day: [], by_task: [], by_workspace: [], by_directory: [], by_swarm: [],
    })))
    renderPage()
    await screen.findByRole("heading", { name: "This Week" })
    expect(screen.getByText("No activity recorded for this period")).toBeTruthy()
    expect(screen.queryByText("Active ratio")).toBeNull()
  })
})
