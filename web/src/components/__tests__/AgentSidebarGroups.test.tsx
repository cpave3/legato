import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { AgentSidebar } from "../AgentSidebar"
import type { AgentInfo } from "../../hooks/useWebSocket"
import type { AgentGrouping } from "../../hooks/useAgentGroups"

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast: vi.fn() }) }))

function agent(task_id: string, activity: string, group?: string): AgentInfo {
  return {
    id: 1, task_id, task_title: `Task ${task_id}`, tmux_session: `legato-${task_id}`, command: "claude", agent_kind: "claude-code",
    status: "running", activity, started_at: "2026-11-12T00:00:00Z", working_seconds: 0, waiting_seconds: 0, error_seconds: 0,
    ...(group ? { group } : {}),
  }
}

function grouping(overrides: Partial<AgentGrouping> = {}): AgentGrouping {
  return {
    mode: "group",
    setMode: vi.fn(),
    known: ["Tickets", "Tooling"],
    defaults: ["Tickets", "Tooling"],
    isCollapsed: () => false,
    toggle: vi.fn(),
    collapseOthers: vi.fn(),
    move: vi.fn(),
    newGroup: vi.fn(),
    renameGroup: vi.fn(),
    deleteGroup: vi.fn(),
    ...overrides,
  }
}

// jsdom has no DataTransfer.
function dataTransfer() {
  const data: Record<string, string> = {}
  return {
    setData: (type: string, value: string) => { data[type] = value },
    getData: (type: string) => data[type] ?? "",
    get types() { return Object.keys(data) },
    effectAllowed: "", dropEffect: "",
  }
}

const groupHeader = (name: string) => screen.getByRole("button", { name: new RegExp(`^${name},`) })

afterEach(cleanup)

describe("AgentSidebar grouped by group", () => {
  it("lists the groups below the pinned Reeve, with each group's agents in status order", () => {
    render(
      <AgentSidebar
        agents={[agent("idle", "", "Tooling"), agent("asks", "waiting", "Tooling"), agent("loose", "working"), agent("side", "working", "Autopilot")]}
        selectedId={null}
        onSelect={() => {}}
        onSpawn={() => {}}
        reeve={<div data-testid="reeve">Reeve</div>}
        grouping={grouping()}
      />,
    )
    const groups = screen.getAllByRole("region").map((region) => region.getAttribute("aria-label"))
    expect(groups).toEqual(["Tickets", "Tooling", "Autopilot", "Ungrouped"])
    const reeve = screen.getByTestId("reeve")
    expect(reeve.compareDocumentPosition(screen.getByRole("region", { name: "Tickets" })) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    const tooling = within(screen.getByRole("region", { name: "Tooling" }))
    expect(tooling.getAllByRole("button").map((b) => b.getAttribute("data-agent")).filter(Boolean)).toEqual(["asks", "idle"])
  })

  it("collapses a group to its count, and still says when one of its agents needs the person", () => {
    const toggle = vi.fn()
    render(
      <AgentSidebar
        agents={[agent("asks", "waiting", "Tooling"), agent("failed", "error", "Tooling"), agent("busy", "working", "Tooling")]}
        selectedId={null}
        onSelect={() => {}}
        onSpawn={() => {}}
        grouping={grouping({ isCollapsed: (name) => name === "Tooling", toggle })}
      />,
    )
    const header = groupHeader("Tooling")
    expect(header.getAttribute("aria-expanded")).toBe("false")
    expect(header.textContent).toContain("3")
    expect(within(header).getByLabelText("2 need you")).toBeTruthy()
    expect(screen.queryByRole("button", { name: /Task asks/ })).toBeNull()

    fireEvent.click(header)
    expect(toggle).toHaveBeenCalledWith("Tooling")
  })

  it("shows no waiting mark on an expanded group, where the agents show it", () => {
    render(
      <AgentSidebar agents={[agent("asks", "waiting", "Tooling")]} selectedId={null} onSelect={() => {}} onSpawn={() => {}} grouping={grouping()} />,
    )
    expect(within(groupHeader("Tooling")).queryByLabelText(/need you/)).toBeNull()
  })

  it("moves an agent dropped on a group, and out of any group when dropped on Ungrouped", () => {
    const move = vi.fn()
    render(
      <AgentSidebar
        agents={[agent("one", "working", "Tooling")]}
        selectedId={null}
        onSelect={() => {}}
        onSpawn={() => {}}
        grouping={grouping({ move })}
      />,
    )
    const entry = screen.getByRole("button", { name: /Task one/ }).closest("[draggable]")!
    const transfer = dataTransfer()
    fireEvent.dragStart(entry, { dataTransfer: transfer })
    const tickets = screen.getByRole("region", { name: "Tickets" })
    fireEvent.dragOver(tickets, { dataTransfer: transfer })
    expect(tickets.getAttribute("data-drop-target")).toBe("true")
    fireEvent.drop(tickets, { dataTransfer: transfer })
    expect(move).toHaveBeenCalledWith("one", "Tickets")

    fireEvent.dragStart(entry, { dataTransfer: transfer })
    const ungrouped = screen.getByRole("region", { name: "Ungrouped" })
    fireEvent.drop(within(ungrouped).getByRole("button", { name: /^Ungrouped,/ }), { dataTransfer: transfer })
    expect(move).toHaveBeenLastCalledWith("one", null)
  })

  it("does not move an agent dropped on the group it is in", () => {
    const move = vi.fn()
    render(
      <AgentSidebar agents={[agent("one", "working", "Tooling")]} selectedId={null} onSelect={() => {}} onSpawn={() => {}} grouping={grouping({ move })} />,
    )
    const entry = screen.getByRole("button", { name: /Task one/ }).closest("[draggable]")!
    const transfer = dataTransfer()
    fireEvent.dragStart(entry, { dataTransfer: transfer })
    fireEvent.drop(screen.getByRole("region", { name: "Tooling" }), { dataTransfer: transfer })
    expect(move).not.toHaveBeenCalled()
  })

  it("offers rename, delete and collapse all others on a group's menu", () => {
    const renameGroup = vi.fn(), deleteGroup = vi.fn(), collapseOthers = vi.fn()
    render(
      <AgentSidebar agents={[agent("one", "working", "Autopilot")]} selectedId={null} onSelect={() => {}} onSpawn={() => {}} grouping={grouping({ renameGroup, deleteGroup, collapseOthers })} />,
    )
    const menu = () => screen.getByRole("button", { name: "Autopilot group actions" })
    fireEvent.click(menu())
    fireEvent.click(screen.getByRole("menuitem", { name: "Rename group" }))
    expect(renameGroup).toHaveBeenCalledWith("Autopilot")
    fireEvent.click(menu())
    fireEvent.click(screen.getByRole("menuitem", { name: "Collapse all others" }))
    expect(collapseOthers).toHaveBeenCalledWith("Autopilot")
    fireEvent.click(menu())
    fireEvent.click(screen.getByRole("menuitem", { name: "Delete group" }))
    expect(deleteGroup).toHaveBeenCalledWith("Autopilot")
  })

  it("lists by status when switched to it", () => {
    const setMode = vi.fn()
    const { rerender } = render(
      <AgentSidebar agents={[agent("one", "working", "Tooling")]} selectedId={null} onSelect={() => {}} onSpawn={() => {}} grouping={grouping({ setMode })} />,
    )
    fireEvent.click(screen.getByRole("button", { name: "List by status" }))
    expect(setMode).toHaveBeenCalledWith("status")

    rerender(
      <AgentSidebar agents={[agent("one", "working", "Tooling")]} selectedId={null} onSelect={() => {}} onSpawn={() => {}} grouping={grouping({ mode: "status", setMode })} />,
    )
    expect(screen.queryByRole("region", { name: "Tooling" })).toBeNull()
    expect(screen.getAllByRole("heading").map((h) => h.textContent)).toEqual(["Running · 1"])
    expect(screen.getByRole("button", { name: "List by group" })).toBeTruthy()
  })
})
