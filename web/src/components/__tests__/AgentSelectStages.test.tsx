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
  a("LEG-413", "Away review counts", "finished", "both bugs fixed", "finished", { open_items: 1, open_done_items: 1 }),
]

afterEach(cleanup)

function Picker({ selected = "LEG-409", initial = null as WorkStage | null, list = agents, reeve = false }) {
  const [stage, setStage] = useState<WorkStage | null>(initial)
  return <AgentSelect agents={list} selectedId={selected} onSelect={() => {}} stageFilter={stage} onStageFilter={setStage} reeve={reeve ? <button type="button">Reeve</button> : undefined} />
}

describe("the phone picker's stages", () => {
  it("reads group · stage · time on its trigger", () => {
    render(<Picker />)
    const trigger = screen.getByRole("button", { name: /^Agent:/ })
    expect(trigger.getAttribute("aria-label")).toBe("Agent: The card's strip says why it needs you, needs you, implementing, 42m")
    expect(within(trigger).getByText("Needs you")).toBeTruthy()
    expect(within(trigger).getByText("Implementing")).toBeTruthy()
    expect(within(trigger).getByText("42m")).toBeTruthy()
    // The track comes before the stage's word, which names it.
    const track = trigger.querySelector("[data-stage-track]")
    expect(track?.getAttribute("data-stage-track")).toBe("implementing")
    expect(track?.nextElementSibling?.textContent).toBe("Implementing")
  })

  it("lists the agents in the same groups as the desktop, each with its stage", () => {
    render(<Picker />)
    fireEvent.click(screen.getByRole("button", { name: /^Agent:/ }))
    const list = screen.getByRole("group", { name: "Running agents" })
    expect(within(list).getAllByRole("heading").map((h) => h.textContent)).toEqual(["Needs you · 2", "Your turn · 1", "Working · 2", "Finished · 2"])
    const row = within(list).getByRole("button", { name: /Settings registry/ })
    expect(within(row).getByText("Implementing")).toBeTruthy()
    expect(within(row).getByText(/fixing review feedback · pass 2/)).toBeTruthy()
    expect(within(within(list).getByRole("button", { name: /Card header/ })).getByText("◆ Plan to approve")).toBeTruthy()
  })

  it("gives a Finished row only its reason, with the unread bar", () => {
    render(<Picker />)
    fireEvent.click(screen.getByRole("button", { name: /^Agent:/ }))
    const finished = screen.getByRole("region", { name: "Finished" })
    const merged = within(finished).getByRole("button", { name: /Macros by PR/ })
    expect(within(merged).queryByText("Finished")).toBeNull()
    expect(within(merged).getByText("PR #212 merged")).toBeTruthy()
    const noted = within(finished).getByRole("button", { name: /Away review counts/ })
    expect(within(noted).getByText("both bugs fixed · a note for you")).toBeTruthy()
    expect(within(finished).getAllByLabelText("Unread: finished with a note for you")).toHaveLength(1)
  })

  it("puts a Stage menu beside the Reeve, with no chips, and counts without the Reeve", () => {
    const reeveAgent = a("reeve-1", "Reeve", "", "", "", { reeve: true, activity: "working" })
    render(<Picker list={[reeveAgent, ...agents]} reeve />)
    fireEvent.click(screen.getByRole("button", { name: /^Agent:/ }))
    const list = screen.getByRole("group", { name: "Running agents" })
    expect(within(list).queryByRole("group", { name: "Filter by stage" })).toBeNull()
    const top = within(list).getByRole("button", { name: "Reeve" }).parentElement!.parentElement!
    const menu = within(top).getByRole("button", { name: "Filter by stage" })
    expect(menu.className).toContain("min-h-11")
    expect(within(list).getByText("7 agents")).toBeTruthy()
  })

  it("filters by the Stage menu, and says what needs you outside", () => {
    render(<Picker />)
    fireEvent.click(screen.getByRole("button", { name: /^Agent:/ }))
    fireEvent.click(screen.getByRole("button", { name: "Filter by stage" }))
    const menu = screen.getByRole("menu", { name: "Stage" })
    expect(within(menu).getByRole("menuitemradio", { name: /Reviewing/ }).textContent).toBe("Reviewing2")
    fireEvent.click(within(menu).getByRole("menuitemradio", { name: /Reviewing/ }))
    const list = screen.getByRole("group", { name: "Running agents" })
    expect(within(list).getAllByRole("heading").map((h) => h.textContent)).toEqual(["Working · 2"])
    expect(within(list).getByText("2 of 7 agents")).toBeTruthy()
    expect(within(list).getByRole("button", { name: "Filter by stage: Reviewing" })).toBeTruthy()
    const notice = within(list).getByRole("status")
    expect(notice.textContent).toContain("2 need you outside this filter")
    fireEvent.click(within(notice).getByRole("button", { name: "Show" }))
    expect(within(list).getAllByRole("heading")[0].textContent).toBe("Needs you · 2")
    expect(within(list).queryByRole("button", { name: "Clear stage filter" })).toBeNull()
  })
})
