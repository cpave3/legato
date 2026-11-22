import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { useState } from "react"
import { afterEach, describe, expect, it } from "vitest"
import { AgentSelect } from "../AgentSelect"
import type { AgentInfo } from "../../hooks/useWebSocket"
import type { WorkStage } from "../../lib/agentStage"

const base: AgentInfo = {
  id: 0, task_id: "", task_title: "", tmux_session: "", command: "claude", agent_kind: "claude-code",
  status: "running", activity: "", started_at: "", working_seconds: 0, waiting_seconds: 0, error_seconds: 0,
}
const a = (task_id: string, task_title: string, stage: string, stage_reason: string, glance: string, fields: Partial<AgentInfo> = {}): AgentInfo => ({
  ...base, task_id, task_title, stage, stage_reason, glance, ...fields,
})

const agents: AgentInfo[] = [
  a("LEG-409", "The card's strip says why it needs you", "implementing", "6 files changed", "needs_you", { activity: "waiting", waiting_seconds: 42 * 60 }),
  a("LEG-412", "Card header and meta", "planning", "plan awaiting you", "needs_you", { plan_pending: true }),
  a("LEG-386", "Settings registry: forms", "implementing", "fixing review feedback · pass 2", "your_turn"),
  a("LEG-407_reviewer", "Review pass 1 on #216–#218", "reviewing", "reviewer slot", "working", { activity: "working", working_seconds: 240 }),
  a("LEG-408", "A macro's agent starts in the clone", "reviewing", "PR #214 open · watching CI", "working", { activity: "background" }),
  a("LEG-405", "Macros by PR in the card menu", "finished", "PR #212 merged", "finished"),
]

afterEach(cleanup)

function Picker({ selected = "LEG-409", initial = null as WorkStage | null }) {
  const [stage, setStage] = useState<WorkStage | null>(initial)
  return <AgentSelect agents={agents} selectedId={selected} onSelect={() => {}} stageFilter={stage} onStageFilter={setStage} />
}

describe("the phone picker's stages", () => {
  it("reads group · stage · time on its trigger", () => {
    render(<Picker />)
    const trigger = screen.getByRole("button", { name: /^Agent:/ })
    expect(trigger.getAttribute("aria-label")).toBe("Agent: The card's strip says why it needs you, needs you, implementing, 42m")
    expect(within(trigger).getByText("Needs you")).toBeTruthy()
    expect(within(trigger).getByText("Implementing")).toBeTruthy()
    expect(within(trigger).getByText("42m")).toBeTruthy()
  })

  it("lists the agents in the same groups as the desktop, each with its stage", () => {
    render(<Picker />)
    fireEvent.click(screen.getByRole("button", { name: /^Agent:/ }))
    const list = screen.getByRole("group", { name: "Running agents" })
    expect(within(list).getAllByRole("heading").map((h) => h.textContent)).toEqual(["Needs you · 2", "Your turn · 1", "Working · 2", "Finished · 1"])
    const row = within(list).getByRole("button", { name: /Settings registry/ })
    expect(within(row).getByText("Implementing")).toBeTruthy()
    expect(within(row).getByText(/fixing review feedback · pass 2/)).toBeTruthy()
    expect(within(within(list).getByRole("button", { name: /Card header/ })).getByText("◆ Plan to approve")).toBeTruthy()
  })

  it("filters by stage with chips a thumb can hit, and says what needs you outside", () => {
    render(<Picker />)
    fireEvent.click(screen.getByRole("button", { name: /^Agent:/ }))
    const chips = screen.getByRole("group", { name: "Filter by stage" })
    expect(chips.className).toContain("overflow-x-auto")
    const reviewing = within(chips).getByRole("button", { name: /Reviewing/ })
    expect(reviewing.className).toContain("min-h-11")
    fireEvent.click(reviewing)
    const list = screen.getByRole("group", { name: "Running agents" })
    expect(within(list).getAllByRole("heading").map((h) => h.textContent)).toEqual(["Working · 2"])
    const notice = within(list).getByRole("status")
    expect(notice.textContent).toContain("2 need you outside this filter")
    fireEvent.click(within(notice).getByRole("button", { name: "Show" }))
    expect(within(list).getAllByRole("heading")[0].textContent).toBe("Needs you · 2")
  })
})
