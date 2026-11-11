import { describe, it, expect, vi, afterEach, beforeEach } from "vitest"
import { render, screen, cleanup, fireEvent, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { ReactNode } from "react"

const mockFetch = vi.fn()
const mockStart = vi.fn()
const mockResume = vi.fn()
vi.mock("../../lib/reeve", () => ({
  fetchReeve: (...a: unknown[]) => mockFetch(...a),
  startReeve: (...a: unknown[]) => mockStart(...a),
  resumeReeve: (...a: unknown[]) => mockResume(...a),
}))

import { ReeveEntry, ReeveStatus } from "../ReeveEntry"
import { AgentSidebar } from "../AgentSidebar"
import { AgentSelect } from "../AgentSelect"
import type { AgentInfo } from "../../hooks/useWebSocket"

const wrap = (node: ReactNode) => <QueryClientProvider client={new QueryClient()}>{node}</QueryClientProvider>
const overview = { history: [], folder: "/r", instance: "legato-1", role: "main", to_approve: 2 }
const agent = (over: Partial<AgentInfo>): AgentInfo => ({
  id: 1, task_id: "a1", task_title: "Fix the sync", tmux_session: "legato-a1", command: "claude", agent_kind: "claude-code",
  status: "running", activity: "working", started_at: "", working_seconds: 0, waiting_seconds: 0, error_seconds: 0, ...over,
})

describe("ReeveEntry", () => {
  beforeEach(() => {
    mockFetch.mockReset(); mockStart.mockReset(); mockResume.mockReset()
  })
  afterEach(() => cleanup())

  it("shows the instance and what waits for approval, and selects the running Reeve", async () => {
    mockFetch.mockResolvedValue({ ...overview, current: { task_id: "rv1", state: "running" } })
    const onSelect = vi.fn()
    render(wrap(<ReeveEntry baseUrl="" agent={agent({ task_id: "rv1", reeve: true })} selected={false} onSelect={onSelect} />))
    const entry = await screen.findByTestId("reeve-entry")
    expect(entry.textContent).toContain("legato-1")
    expect(entry.textContent).toContain("2 to approve")
    fireEvent.click(screen.getByRole("button", { name: "The Reeve" }))
    expect(onSelect).toHaveBeenCalledWith("rv1")
  })

  it("starts a session when none runs, and resumes a suspended one", async () => {
    mockFetch.mockResolvedValue(overview)
    mockStart.mockResolvedValue({ task_id: "new1" })
    const onSelect = vi.fn()
    render(wrap(<ReeveEntry baseUrl="" selected={false} onSelect={onSelect} />))
    fireEvent.click(await screen.findByRole("button", { name: "Start the Reeve" }))
    await waitFor(() => expect(onSelect).toHaveBeenCalledWith("new1"))
    cleanup()

    mockFetch.mockResolvedValue({ ...overview, current: { task_id: "s1", state: "suspended" } })
    mockResume.mockResolvedValue({ task_id: "s1" })
    render(wrap(<ReeveEntry baseUrl="" selected={false} onSelect={onSelect} />))
    fireEvent.click(await screen.findByRole("button", { name: "Start the Reeve" }))
    await waitFor(() => expect(mockResume).toHaveBeenCalledWith("", "s1"))
  })

  it("is pinned above the groups in the sidebar, and leads the phone's list", async () => {
    mockFetch.mockResolvedValue(overview)
    const agents = [agent({ task_id: "a1" }), agent({ task_id: "rv1", reeve: true })]
    render(wrap(<AgentSidebar agents={agents} selectedId={null} onSelect={() => {}} onSpawn={() => {}}
      reeve={<ReeveEntry baseUrl="" agent={agents[1]} selected={false} onSelect={() => {}} />} />))
    const list = screen.getByLabelText("Agent sessions")
    const reeve = await screen.findByTestId("reeve-entry")
    expect(list.textContent!.indexOf("Reeve")).toBeLessThan(list.textContent!.indexOf("Running"))
    expect(list.querySelector('[data-agent="rv1"]')).toBeNull()
    expect(reeve).toBeTruthy()
    cleanup()

    render(wrap(<AgentSelect agents={agents} selectedId="a1" onSelect={() => {}} />))
    fireEvent.click(screen.getByRole("button", { name: /Agent:/ }))
    const rows = screen.getByRole("group", { name: "Running agents" }).querySelectorAll("button")
    expect(rows[0].textContent).toContain("rv1")
  })

  it("puts the sandbox and the instance with its role in the status bar", async () => {
    mockFetch.mockResolvedValue(overview)
    render(wrap(<ReeveStatus baseUrl="" />))
    const status = await screen.findByTestId("reeve-status")
    expect(status.textContent).toContain("sandboxed")
    expect(status.textContent).toContain("legato-1 (main)")
    expect(status.textContent).toContain("no engram")
  })

  it("names the engram the Reeve runs as", async () => {
    mockFetch.mockResolvedValue({ ...overview, engram: "reeve-1a2b3c4d" })
    render(wrap(<ReeveStatus baseUrl="" />))
    const status = await screen.findByTestId("reeve-status")
    expect(status.textContent).toContain("reeve-1a2b3c4d")
    expect(status.textContent).not.toContain("no engram")
  })
})
