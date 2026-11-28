import { cleanup, render, screen, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { AgentSidebar } from "../AgentSidebar"
import type { AgentInfo } from "../../lib/agentWire"

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast: vi.fn() }) }))

const agent = (fields: Partial<AgentInfo>): AgentInfo => ({
  id: 1, task_id: "", task_title: "", tmux_session: "", command: "claude", agent_kind: "claude-code",
  status: "running", activity: "working", started_at: "", working_seconds: 60, waiting_seconds: 0, error_seconds: 0, glance: "working",
  ...fields,
})

afterEach(cleanup)

describe("AgentSidebar run card", () => {
  it("names the card a workflow run's agent works for", () => {
    render(<AgentSidebar agents={[
      agent({ task_id: "we9uhb1r_reviewer", task_title: "Ticket to PR · LEG-603 · reviewer", slot: "reviewer",
        run_card: { task_id: "c9x2k1m4", key: "LEG-603", title: "Keep a paused built-in paused" } }),
      agent({ id: 2, task_id: "LEG-7", task_title: "Fix login" }),
    ]} selectedId={null} onSelect={() => {}} onSpawn={() => {}} />)
    const reviewer = screen.getByRole("button", { name: /we9uhb1r_reviewer/ })
    expect(within(reviewer).getByTitle("Keep a paused built-in paused").textContent).toBe("for LEG-603")
    const own = screen.getByRole("button", { name: /LEG-7/ })
    expect(within(own).queryByText(/^for /)).toBeNull()
  })
})
