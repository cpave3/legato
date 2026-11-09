import { describe, it, expect, vi, afterEach, beforeEach } from "vitest"
import { render, screen, cleanup, fireEvent, waitFor, within } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"

const mockFetch = vi.fn()
const mockStart = vi.fn()
const mockEnd = vi.fn()
const mockResume = vi.fn()
const mockDelete = vi.fn()
vi.mock("../../lib/reeve", () => ({
  fetchReeve: (...a: unknown[]) => mockFetch(...a),
  startReeve: (...a: unknown[]) => mockStart(...a),
  endReeve: (...a: unknown[]) => mockEnd(...a),
  resumeReeve: (...a: unknown[]) => mockResume(...a),
  deleteReeve: (...a: unknown[]) => mockDelete(...a),
}))

import { ReeveSessions } from "../settings/ReeveSettings"

const ended = { task_id: "old1", state: "ended", agent_kind: "codex", title: "The Reeve", created_at: "2026-11-01T10:00:00Z", updated_at: "", active_at: "" }

function show() {
  render(<QueryClientProvider client={new QueryClient()}><ReeveSessions baseUrl="http://legato" /></QueryClientProvider>)
}

describe("ReeveSessions", () => {
  beforeEach(() => {
    for (const m of [mockStart, mockEnd, mockResume, mockDelete]) { m.mockReset(); m.mockResolvedValue({}) }
    mockFetch.mockReset()
  })
  afterEach(() => cleanup())

  it("starts a session with an override of its tool and model", async () => {
    mockFetch.mockResolvedValue({ history: [ended], folder: "/home/me/.local/state/legato/reeve" })
    show()
    fireEvent.change(await screen.findByRole("textbox", { name: "Model for this session" }), { target: { value: "opus" } })
    fireEvent.click(screen.getByRole("button", { name: "Start the Reeve" }))
    await waitFor(() => expect(mockStart).toHaveBeenCalledWith("http://legato", { agent: undefined, model: "opus" }))
  })

  it("ends the running session, and resumes or deletes an old one", async () => {
    mockFetch.mockResolvedValue({ current: { ...ended, task_id: "now1", state: "running" }, history: [{ ...ended, task_id: "now1", state: "running" }, ended], folder: "/r" })
    show()
    fireEvent.click(await screen.findByRole("button", { name: "End session" }))
    await waitFor(() => expect(mockEnd).toHaveBeenCalledWith("http://legato", "now1"))
    const old = screen.getByTestId("reeve-session-old1")
    expect((within(old).getByRole("button", { name: "Resume" }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.click(within(old).getByRole("button", { name: "Delete" }))
    await waitFor(() => expect(mockDelete).toHaveBeenCalledWith("http://legato", "old1"))
  })

  it("says why a start was refused", async () => {
    mockFetch.mockResolvedValue({ history: [], folder: "/r" })
    mockStart.mockRejectedValue(new Error("the Reeve runs only in Legato's sandbox, which is off here"))
    show()
    fireEvent.click(await screen.findByRole("button", { name: "Start the Reeve" }))
    expect((await screen.findByRole("alert")).textContent).toContain("sandbox")
  })
})
