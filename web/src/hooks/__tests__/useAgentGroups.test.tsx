import { act, cleanup, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import type { AgentInfo } from "../useWebSocket"
import { useAgentGroups } from "../useAgentGroups"

vi.mock("../useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../useToast", () => ({ useToast: () => ({ addToast: vi.fn() }) }))

function agent(task_id: string, group?: string): AgentInfo {
  return {
    id: 1, task_id, task_title: `Task ${task_id}`, tmux_session: "", command: "claude", agent_kind: "claude-code",
    status: "running", activity: "working", started_at: "2026-11-12T00:00:00Z", working_seconds: 0, waiting_seconds: 0, error_seconds: 0,
    ...(group ? { group } : {}),
  }
}

let calls: { url: string; init?: RequestInit }[] = []

beforeEach(() => {
  localStorage.clear()
  calls = []
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    calls.push({ url, init })
    if (url === "/api/agent-groups") return new Response(JSON.stringify({ defaults: ["Tickets"], groups: ["Tickets"] }))
    return new Response(JSON.stringify({}))
  }))
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("useAgentGroups", () => {
  it("lists by group once an agent has one, and the default groups are known", async () => {
    const { result, rerender } = renderHook(({ agents }) => useAgentGroups({ agents, onChanged: () => {} }), { initialProps: { agents: [agent("a")] } })
    expect(result.current.grouping.mode).toBe("status")
    rerender({ agents: [agent("a", "Tooling")] })
    expect(result.current.grouping.mode).toBe("group")
    await waitFor(() => expect(result.current.grouping.known).toEqual(["Tickets"]))
  })

  it("remembers a collapsed group across a reload", () => {
    const first = renderHook(() => useAgentGroups({ agents: [agent("a", "Tooling")], onChanged: () => {} }))
    act(() => first.result.current.grouping.toggle("Tooling"))
    act(() => first.result.current.grouping.toggle(null))
    expect(first.result.current.grouping.isCollapsed("Tooling")).toBe(true)
    first.unmount()

    const second = renderHook(() => useAgentGroups({ agents: [agent("a", "Tooling")], onChanged: () => {} }))
    expect(second.result.current.grouping.isCollapsed("Tooling")).toBe(true)
    expect(second.result.current.grouping.isCollapsed(null)).toBe(true)
    expect(second.result.current.grouping.isCollapsed("Tickets")).toBe(false)
  })

  it("collapses every group but one", () => {
    const { result } = renderHook(() => useAgentGroups({ agents: [agent("a", "One"), agent("b", "Two"), agent("c")], onChanged: () => {} }))
    act(() => result.current.grouping.collapseOthers("Two"))
    expect(["One", "Two", null].map((name) => result.current.grouping.isCollapsed(name))).toEqual([true, false, true])
  })

  it("moves an agent through the server and refreshes the list", async () => {
    const onChanged = vi.fn()
    const { result } = renderHook(() => useAgentGroups({ agents: [agent("a_reviewer")], onChanged }))
    act(() => result.current.grouping.move("a_reviewer", "Autopilot"))
    await waitFor(() => expect(onChanged).toHaveBeenCalled())
    const put = calls.find((call) => call.init?.method === "PUT")!
    expect(put.url).toBe("/api/agents/a_reviewer/group")
    expect(JSON.parse(put.init!.body as string)).toEqual({ group: "Autopilot" })
  })

  it("moves an agent from its context menu into a new group", async () => {
    const onChanged = vi.fn()
    function Harness() {
      const groups = useAgentGroups({ agents: [agent("a", "Tooling")], onChanged })
      return <>
        <button onClick={() => groups.openMove(agent("a", "Tooling"))}>open</button>
        {groups.dialogs}
      </>
    }
    render(<Harness />)
    fireEvent.click(screen.getByText("open"))
    expect(screen.getByRole("button", { name: "Tooling" }).getAttribute("aria-current")).toBe("true")

    fireEvent.click(screen.getByRole("button", { name: /New group/ }))
    fireEvent.change(screen.getByLabelText("New group name"), { target: { value: " Side projects " } })
    fireEvent.click(screen.getByRole("button", { name: "Move" }))

    await waitFor(() => expect(onChanged).toHaveBeenCalled())
    const put = calls.find((call) => call.init?.method === "PUT")!
    expect(JSON.parse(put.init!.body as string)).toEqual({ group: "Side projects" })
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
  })

  it("moves an agent from its context menu to Ungrouped", async () => {
    const onChanged = vi.fn()
    function Harness() {
      const groups = useAgentGroups({ agents: [agent("a", "Tooling")], onChanged })
      return <>
        <button onClick={() => groups.openMove(agent("a", "Tooling"))}>open</button>
        {groups.dialogs}
      </>
    }
    render(<Harness />)
    fireEvent.click(screen.getByText("open"))
    fireEvent.click(screen.getByRole("button", { name: "Ungrouped" }))
    await waitFor(() => expect(onChanged).toHaveBeenCalled())
    const put = calls.find((call) => call.init?.method === "PUT")!
    expect(JSON.parse(put.init!.body as string)).toEqual({ group: null })
  })
})
