import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { useState } from "react"
import { AgentSidebar } from "../AgentSidebar"
import type { AgentInfo } from "../../hooks/useWebSocket"
import type { WorkStage } from "../../lib/agentStage"

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast: vi.fn() }) }))

const base: AgentInfo = {
  id: 0, task_id: "", task_title: "", tmux_session: "", command: "claude", agent_kind: "claude-code",
  status: "running", activity: "", started_at: "", working_seconds: 60, waiting_seconds: 0, error_seconds: 0,
}
const a = (task_id: string, task_title: string, stage: string, stage_reason: string, glance: string, fields: Partial<AgentInfo> = {}): AgentInfo => ({
  ...base, id: task_id.length, task_id, task_title, stage, stage_reason, glance, ...fields,
})

// The ten agents of the design's canvas.
const canvas: AgentInfo[] = [
  a("LEG-409", "The card's strip says why it needs you", "implementing", "6 files changed", "needs_you", { activity: "waiting" }),
  a("LEG-412", "Card header and meta: source names", "planning", "plan awaiting you", "needs_you", { plan_pending: true }),
  a("LEG-386", "Settings registry: forms", "implementing", "fixing review feedback · pass 2", "your_turn"),
  a("LEG-438", "Plan: agent stages", "exploring", "read 41 files", "your_turn"),
  a("LEG-407", "A PR stack folds to a summary", "implementing", "9 files changed", "working", { activity: "working" }),
  a("LEG-407_reviewer", "Review pass 1 on #216–#218", "reviewing", "reviewer slot", "working", { activity: "working", slot: "reviewer" }),
  a("LEG-408", "A macro's agent starts in the clone", "reviewing", "PR #214 open · watching CI", "working", { activity: "background" }),
  a("LEG-406", "Each PR says its state in one word", "exploring", "read 18 files", "working", { activity: "working" }),
  a("LEG-405", "Macros by PR in the card menu", "finished", "PR #212 merged", "finished"),
  a("LEG-413", "Away review counts while the author works", "finished", "reported: both bugs fixed, PR #215", "finished", { open_items: 1, open_done_items: 1 }),
]

afterEach(cleanup)

function Sidebar({ agents = canvas, initial = null as WorkStage | null }: { agents?: AgentInfo[]; initial?: WorkStage | null }) {
  const [stage, setStage] = useState<WorkStage | null>(initial)
  return <AgentSidebar agents={agents} selectedId={null} onSelect={() => {}} onSpawn={() => {}} stageFilter={stage} onStageFilter={setStage} />
}

const headings = () => screen.getAllByRole("heading").map((heading) => heading.textContent)
const listed = () => screen.getAllByRole("button").map((button) => button.getAttribute("data-agent")).filter(Boolean)

describe("AgentSidebar stages", () => {
  it("lists the agents in glance groups, Your turn under Needs you", () => {
    render(<Sidebar />)
    expect(headings()).toEqual(["Needs you · 2", "Your turn · 2", "Working · 4", "Finished · 2"])
    expect(listed()).toEqual(["LEG-409", "LEG-412", "LEG-386", "LEG-438", "LEG-407", "LEG-407_reviewer", "LEG-408", "LEG-406", "LEG-405", "LEG-413"])
  })

  it("gives each agent a stage line, and those that need the operator their reason", () => {
    render(<Sidebar />)
    const row = (id: string) => screen.getByRole("button", { name: new RegExp(id) })
    expect(within(row("LEG-386")).getByText("Implementing")).toBeTruthy()
    expect(within(row("LEG-386")).getByText("· fixing review feedback · pass 2")).toBeTruthy()
    expect(within(row("LEG-386")).getByRole("img", { name: "Implementing" })).toBeTruthy()
    expect(within(row("LEG-409")).getByText("◆ Waiting for your answer")).toBeTruthy()
    expect(within(row("LEG-412")).getByText("◆ Plan to approve")).toBeTruthy()
    expect(within(row("LEG-405")).queryByText(/◆/)).toBeNull()
  })

  it("shows a done note as Finished, marked unread", () => {
    render(<Sidebar />)
    const finished = screen.getByRole("region", { name: "Finished" })
    expect(within(finished).getByLabelText("Unread: finished with a note for you")).toBeTruthy()
    expect(within(finished).getAllByLabelText(/Unread/)).toHaveLength(1)
    expect(screen.getByRole("region", { name: "Needs you" }).textContent).not.toContain("LEG-413")
  })

  it("has no chips: one Stage menu counts each stage, empty ones dimmed but choosable", () => {
    render(<Sidebar agents={canvas.filter((agent) => agent.stage !== "planning")} />)
    expect(screen.queryByRole("group", { name: "Filter by stage" })).toBeNull()
    const trigger = screen.getByRole("button", { name: "Filter by stage" })
    expect(trigger.getAttribute("aria-expanded")).toBe("false")
    fireEvent.click(trigger)
    const menu = screen.getByRole("menu", { name: "Stage" })
    const items = within(menu).getAllByRole("menuitemradio")
    expect(items.map((item) => item.textContent)).toEqual(["All9", "Exploring2", "Planning0", "Implementing3", "Reviewing2", "Finished2"])
    expect(items[0].getAttribute("aria-checked")).toBe("true")
    expect(within(items[3]).getByRole("img", { name: "Implementing" })).toBeTruthy()
    const planning = within(menu).getByRole("menuitemradio", { name: /Planning/ })
    fireEvent.click(planning)
    expect(screen.getByText(/No agents are planning/)).toBeTruthy()
  })

  it("filters by the stage chosen, groups kept, and the button and header say so", () => {
    render(<Sidebar />)
    expect(screen.getByTestId("agent-count").textContent).toBe("Agents 10")
    fireEvent.click(screen.getByRole("button", { name: "Filter by stage" }))
    fireEvent.click(screen.getByRole("menuitemradio", { name: /Reviewing/ }))
    expect(screen.queryByRole("menu")).toBeNull()
    expect(headings()).toEqual(["Working · 2"])
    expect(listed()).toEqual(["LEG-407_reviewer", "LEG-408"])
    expect(screen.getByTestId("agent-count").textContent).toBe("Agents 2 of 10")
    const trigger = screen.getByRole("button", { name: "Filter by stage: Reviewing" })
    expect(trigger.textContent).toContain("Reviewing")
    expect(within(trigger).getByRole("img", { name: "Reviewing" })).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Clear stage filter" }))
    expect(listed()).toHaveLength(10)
    expect(screen.getByTestId("agent-count").textContent).toBe("Agents 10")
    expect(screen.queryByRole("button", { name: "Clear stage filter" })).toBeNull()
  })

  it("closes the menu on Escape without changing the filter", () => {
    render(<Sidebar initial="finished" />)
    fireEvent.click(screen.getByRole("button", { name: "Filter by stage: Finished" }))
    expect(screen.getByRole("menuitemradio", { name: /Finished/ }).getAttribute("aria-checked")).toBe("true")
    fireEvent.keyDown(document, { key: "Escape" })
    expect(screen.queryByRole("menu")).toBeNull()
    expect(listed()).toEqual(["LEG-405", "LEG-413"])
  })

  it("gives a Finished row only its reason, not the stage again", () => {
    render(<Sidebar />)
    const row = (id: string) => screen.getByRole("button", { name: new RegExp(id) })
    expect(within(row("LEG-405")).queryByText("Finished")).toBeNull()
    expect(within(row("LEG-405")).queryByRole("img")).toBeNull()
    expect(within(row("LEG-405")).getByText("PR #212 merged")).toBeTruthy()
    expect(within(row("LEG-413")).getByText("reported: both bugs fixed, PR #215 · a note for you")).toBeTruthy()
  })

  it("leaves the Reeve out of every count", () => {
    const reeve = a("reeve-1", "Reeve", "", "", "", { reeve: true, activity: "working" })
    render(<AgentSidebar agents={[reeve, ...canvas.slice(0, 3)]} reeve={<div>Reeve</div>} selectedId={null} onSelect={() => {}} onSpawn={() => {}} stageFilter={null} onStageFilter={() => {}} />)
    expect(screen.getByTestId("agent-count").textContent).toBe("Agents 3")
    fireEvent.click(screen.getByRole("button", { name: "Filter by stage" }))
    expect(screen.getByRole("menuitemradio", { name: /All/ }).textContent).toBe("All3")
    expect(headings()).toEqual(["Needs you · 2", "Your turn · 1"])
  })

  it("keeps saying how many need the operator outside the filter", () => {
    render(<Sidebar initial="reviewing" />)
    const notice = screen.getByRole("status")
    expect(notice.textContent).toContain("2 need you outside this filter")
    fireEvent.click(within(notice).getByRole("button", { name: "Show" }))
    expect(headings()[0]).toBe("Needs you · 2")
    expect(screen.queryByRole("status")).toBeNull()
  })

  it("says when no agent is at the stage, with a way back", () => {
    render(<Sidebar agents={canvas.filter((agent) => agent.stage !== "planning")} initial="planning" />)
    expect(screen.getByText(/No agents are planning/)).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Show all" }))
    expect(listed()).toHaveLength(9)
  })
})
