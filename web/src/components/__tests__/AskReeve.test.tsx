import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { MemoryRouter, useLocation } from "react-router-dom"

const mockFetch = vi.fn()
const mockStart = vi.fn()
const mockEnd = vi.fn()
const mockMessage = vi.fn()
vi.mock("../../lib/reeve", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../lib/reeve")>()),
  fetchReeve: (...a: unknown[]) => mockFetch(...a),
  startReeve: (...a: unknown[]) => mockStart(...a),
  endReeve: (...a: unknown[]) => mockEnd(...a),
  messageReeve: (...a: unknown[]) => mockMessage(...a),
}))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "http://legato" }) }))

import { AskReeve } from "../AskReeve"

function Where() {
  const location = useLocation()
  return <p data-testid="where">{location.pathname + location.search}</p>
}

function show() {
  render(<MemoryRouter initialEntries={["/attention"]}><AskReeve brief="what is wrong?" /><Where /></MemoryRouter>)
}

const running = { task_id: "rv-1", state: "running", agent_kind: "claude-code", title: "The Reeve", created_at: "", updated_at: "", active_at: "" }

describe("AskReeve", () => {
  beforeEach(() => {
    for (const m of [mockFetch, mockStart, mockEnd, mockMessage]) m.mockReset()
    mockStart.mockResolvedValue({ ...running, task_id: "rv-2" })
    mockEnd.mockResolvedValue({})
    mockMessage.mockResolvedValue({})
  })
  afterEach(() => cleanup())

  it("starts a session with the brief when none runs, and opens it", async () => {
    mockFetch.mockResolvedValue({ history: [], folder: "/r" })
    show()
    fireEvent.click(screen.getByRole("button", { name: "Ask the Reeve" }))
    await waitFor(() => expect(mockStart).toHaveBeenCalledWith("http://legato", { brief: "what is wrong?" }))
    await waitFor(() => expect(screen.getByTestId("where").textContent).toBe("/agents?task=rv-2"))
  })

  it("adds to the running conversation when the person says so", async () => {
    mockFetch.mockResolvedValue({ current: running, history: [running], folder: "/r" })
    show()
    fireEvent.click(screen.getByRole("button", { name: "Ask the Reeve" }))
    fireEvent.click(await screen.findByRole("button", { name: "Add to the current conversation" }))
    expect(screen.queryByRole("alert")).toBeNull()
    await waitFor(() => expect(mockMessage).toHaveBeenCalledWith("http://legato", "what is wrong?"))
    expect(mockStart).not.toHaveBeenCalled()
    await waitFor(() => expect(screen.getByTestId("where").textContent).toBe("/agents?task=rv-1"))
  })

  it("ends the running session and starts fresh when the person says so", async () => {
    mockFetch.mockResolvedValue({ current: running, history: [running], folder: "/r" })
    show()
    fireEvent.click(screen.getByRole("button", { name: "Ask the Reeve" }))
    fireEvent.click(await screen.findByRole("button", { name: "End it and start fresh" }))
    await waitFor(() => expect(mockStart).toHaveBeenCalledWith("http://legato", { brief: "what is wrong?" }))
    expect(mockEnd).toHaveBeenCalledWith("http://legato", "rv-1")
    await waitFor(() => expect(screen.getByTestId("where").textContent).toBe("/agents?task=rv-2"))
  })

  it("says why it could not ask", async () => {
    mockFetch.mockResolvedValue({ history: [], folder: "/r" })
    mockStart.mockRejectedValue(new Error("the Reeve runs only in Legato's sandbox, which is off here"))
    show()
    fireEvent.click(screen.getByRole("button", { name: "Ask the Reeve" }))
    expect((await screen.findByRole("alert")).textContent).toContain("sandbox")
  })
})
