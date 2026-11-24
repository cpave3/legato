import { cleanup, render, screen, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { AgentSidebar } from "../AgentSidebar"
import type { AgentInfo } from "../../hooks/useWebSocket"

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast: vi.fn() }) }))

const agent = (fields: Partial<AgentInfo>): AgentInfo => ({
  id: 1, task_id: "", task_title: "", tmux_session: "", command: "claude", agent_kind: "claude-code",
  status: "running", activity: "", started_at: "", working_seconds: 60, waiting_seconds: 0, error_seconds: 0, stage: "implementing",
  ...fields,
})

afterEach(cleanup)

describe("AgentSidebar waiting on the Reeve", () => {
  it("lists an agent waiting on the Reeve under Working, saying so and for how long", () => {
    const since = new Date(Date.now() - 4 * 60_000).toISOString()
    render(<AgentSidebar agents={[
      agent({ id: 1, task_id: "LEG-613", task_title: "Waiting on the Reeve", glance: "working", waiting_on: "reeve", waiting_since: since }),
      agent({ id: 2, task_id: "LEG-614", task_title: "Unanswered", glance: "needs_you", waiting_on: "reeve", waiting_since: new Date(Date.now() - 20 * 60_000).toISOString() }),
    ]} selectedId={null} onSelect={() => {}} onSpawn={() => {}} />)
    const headings = screen.getAllByRole("heading").map((h) => h.textContent)
    expect(headings).toEqual(["Needs you · 1", "Working · 1"])
    const waiting = screen.getByRole("button", { name: /LEG-613/ })
    expect(within(waiting).getByText("◈ Waiting on the Reeve · 4m")).toBeTruthy()
    const unanswered = screen.getByRole("button", { name: /LEG-614/ })
    expect(within(unanswered).getByText("◆ Waiting on the Reeve for 20 min")).toBeTruthy()
  })
})
