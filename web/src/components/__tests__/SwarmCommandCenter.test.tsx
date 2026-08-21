import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { SwarmCommandCenter } from "../SwarmCommandCenter"
import type { AgentInfo } from "../../hooks/useWebSocket"
import type { SwarmStatusData } from "../../lib/swarm"

const { apiFetch } = vi.hoisted(() => ({ apiFetch: vi.fn() }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../lib/api", () => ({ apiFetch }))

const agent = (values: Partial<AgentInfo> & Pick<AgentInfo, "task_id">): AgentInfo => ({
  id: 1,
  task_title: values.task_id,
  tmux_session: `legato-${values.task_id}`,
  command: "claude",
  agent_kind: "claude",
  status: "running",
  activity: "working",
  started_at: "now",
  working_seconds: 0,
  waiting_seconds: 0,
  error_seconds: 0,
  parent_task_id: "P-1",
  ...values,
})

const agents = [
  agent({ task_id: "P-1", role: "conductor" }),
  agent({ task_id: "worker-a", role: "builder", subtask_id: "sub-a" }),
  agent({ task_id: "worker-b", role: "qa", subtask_id: "sub-b", has_chimera_endpoint: true, activity: "waiting" }),
]

const status: SwarmStatusData = {
  parent: { id: "P-1", title: "Parent", status: "Doing", active_step: 0 },
  subtasks: [
    { id: "sub-a", title: "Build API", role: "builder", scope_globs: [], status: "in_progress", step_index: 0 },
    { id: "sub-b", title: "Test API", role: "qa", scope_globs: [], status: "reporting", step_index: 0 },
    { id: "sub-c", title: "Write docs", role: "writer", scope_globs: [], status: "queued", step_index: 1 },
  ],
}

describe("SwarmCommandCenter", () => {
  beforeEach(() => {
    apiFetch.mockReset()
    apiFetch.mockResolvedValue(new Response(JSON.stringify({ task_id: "worker-a", content: "recent worker output", captured_at: "now" }), { status: 200 }))
  })
  afterEach(cleanup)

  it("keeps the conductor visible and represents running and queued workers", async () => {
    render(<SwarmCommandCenter parentId="P-1" agents={agents} status={status} focusedId="P-1" mobile={false} onFocus={() => {}} onOpenSingle={() => {}} renderParticipant={(value) => <div>{`panel:${value.task_id}`}</div>} />)

    expect(screen.getByText("panel:P-1")).toBeTruthy()
    expect(screen.getByText("Build API")).toBeTruthy()
    expect(screen.getByText("Test API")).toBeTruthy()
    expect(screen.getByText("Write docs")).toBeTruthy()
    expect(screen.getByText("No active session")).toBeTruthy()
    expect(screen.getByText("Native session · expand to inspect")).toBeTruthy()
    await waitFor(() => expect(screen.getByText("recent worker output")).toBeTruthy())
    expect(apiFetch).toHaveBeenCalledTimes(1)
    expect(apiFetch).toHaveBeenCalledWith("", "/api/agents/worker-a/preview")
  })

  it("expands a worker while retaining the conductor", () => {
    const focus = vi.fn()
    const view = render(<SwarmCommandCenter parentId="P-1" agents={agents} status={status} focusedId="P-1" mobile={false} onFocus={focus} onOpenSingle={() => {}} renderParticipant={(value) => <div>{`panel:${value.task_id}`}</div>} />)
    fireEvent.click(screen.getByRole("button", { name: "Expand Build API" }))
    expect(focus).toHaveBeenCalledWith("worker-a")

    view.rerender(<SwarmCommandCenter parentId="P-1" agents={agents} status={status} focusedId="worker-a" mobile={false} onFocus={focus} onOpenSingle={() => {}} renderParticipant={(value) => <div>{`panel:${value.task_id}`}</div>} />)
    expect(screen.getByText("panel:P-1")).toBeTruthy()
    expect(screen.getByText("panel:worker-a")).toBeTruthy()
    expect(screen.queryByText("recent worker output")).toBeNull()
  })

  it("uses one participant picker on mobile", () => {
    const focus = vi.fn()
    render(<SwarmCommandCenter parentId="P-1" agents={agents} status={status} focusedId="P-1" mobile onFocus={focus} onOpenSingle={() => {}} renderParticipant={(value) => <div>{`panel:${value.task_id}`}</div>} />)
    expect(screen.getAllByText(/^panel:/)).toHaveLength(1)
    fireEvent.change(screen.getByLabelText("Participant"), { target: { value: "worker-b" } })
    expect(focus).toHaveBeenCalledWith("worker-b")
  })
})
