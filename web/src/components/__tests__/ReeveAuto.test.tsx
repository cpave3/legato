import { describe, it, expect, vi, afterEach, beforeEach } from "vitest"
import { render, screen, cleanup, fireEvent, waitFor, act } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"

const mockAuto = vi.fn()
const mockOff = vi.fn()
const mockLines = vi.fn()
vi.mock("../../lib/reeve", () => ({
  fetchReeveAuto: (...a: unknown[]) => mockAuto(...a),
  turnReeveAutoOff: (...a: unknown[]) => mockOff(...a),
  fetchChangeSetLines: (...a: unknown[]) => mockLines(...a),
}))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "http://legato" }) }))
const socket = vi.hoisted(() => ({ handler: undefined as ((m: Record<string, string>) => void) | undefined }))
vi.mock("../../hooks/useWebSocket", () => ({
  useWebSocket: () => ({ subscribe: (h: (m: Record<string, string>) => void) => { socket.handler = h; return () => { socket.handler = undefined } } }),
}))

import { ReeveAutoSettings } from "../settings/ReeveAutoSettings"
import { AutoRunNotice } from "../AutoRunNotice"
import { ChangeSetLines } from "../ChangeSetLines"

const off = { on: false, change: "", risky: "", host_auto: false, rules: [], pinned: [], recent: [] }
const run = { id: "cs-1", approval_id: "ap-1", task_id: "R1", reason: "Rename the task", state: "done", by: "auto: reeve.levels.change", at: "2026-11-12T10:00:00.000Z" }

function wrap(node: React.ReactNode) {
  return render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{node}</QueryClientProvider>)
}

describe("ReeveAutoSettings", () => {
  beforeEach(() => { mockAuto.mockReset(); mockOff.mockReset(); mockOff.mockResolvedValue({ changed: [], pinned: [] }) })
  afterEach(() => cleanup())

  it("says what runs without asking, warns about host operations, and turns it all off", async () => {
    mockAuto.mockResolvedValueOnce({ ...off, on: true, change: "auto", risky: "auto", host_auto: true, rules: ["POST /api/tasks*"], recent: [run] })
    mockAuto.mockResolvedValue(off)
    wrap(<ReeveAutoSettings baseUrl="http://legato" />)
    expect(await screen.findByText("Changes (tasks, agents, their messages, ordinary settings)")).toBeTruthy()
    expect(screen.getByText(/can run any command as your user/)).toBeTruthy()
    expect(screen.getByText(/restarts, stopping agents/)).toBeTruthy()
    expect(screen.getByText("POST /api/tasks*")).toBeTruthy()
    expect(screen.getByText("Rename the task")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Ask for everything" }))
    await waitFor(() => expect(mockOff).toHaveBeenCalledWith("http://legato"))
    expect(await screen.findByText("Every change waits for your approval.")).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Ask for everything" })).toBeNull()
  })

  it("names what config.yaml keeps at auto", async () => {
    mockAuto.mockResolvedValue({ ...off, on: true, change: "auto", pinned: ["reeve.levels.change"] })
    wrap(<ReeveAutoSettings baseUrl="http://legato" />)
    expect(await screen.findByText(/config.yaml sets reeve.levels.change/)).toBeTruthy()
  })
})

describe("AutoRunNotice", () => {
  beforeEach(() => { mockAuto.mockReset(); socket.handler = undefined })
  afterEach(() => cleanup())

  it("shows the set whose approval was decided without asking, and asks nothing until then", async () => {
    const fresh = { ...run, id: "cs-2", approval_id: "ap-2", reason: "Start an agent", by: "auto: rule POST /api/tasks/*/agent" }
    mockAuto.mockResolvedValue({ ...off, recent: [fresh, run] })
    render(<AutoRunNotice />)
    expect(mockAuto).not.toHaveBeenCalled()
    // A person's decision of some other approval shows nothing.
    await act(async () => { socket.handler?.({ type: "approvals_changed", kind: "approved", item_id: "ap-9" }) })
    expect(screen.queryByText("Rename the task")).toBeNull()
    await act(async () => { socket.handler?.({ type: "approvals_changed", kind: "approved", item_id: "ap-2" }) })
    expect(await screen.findByText("Start an agent")).toBeTruthy()
    expect(screen.getByText("Ran without asking · rule POST /api/tasks/*/agent")).toBeTruthy()
    expect(screen.queryByText("Rename the task")).toBeNull()
  })
})

describe("ChangeSetLines", () => {
  afterEach(() => cleanup())

  it("marks a set that ran without asking", async () => {
    mockLines.mockResolvedValue([
      { id: "cs-1", reason: "Rename", state: "done", calls: 1, ran: 1, auto: true },
      { id: "cs-2", reason: "Restart", state: "pending", calls: 1, ran: 0 },
    ])
    render(<ChangeSetLines baseUrl="http://legato" taskId="R1" />)
    expect(await screen.findByText("auto")).toBeTruthy()
    expect(screen.getAllByText("auto")).toHaveLength(1)
  })
})
