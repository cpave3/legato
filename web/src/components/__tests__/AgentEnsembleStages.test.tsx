import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { AgentEnsemble, ENSEMBLE_LAYOUT_KEY } from "../AgentEnsemble"
import type { AgentInfo } from "../../hooks/useWebSocket"

const { apiFetch } = vi.hoisted(() => ({ apiFetch: vi.fn() }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../lib/api", () => ({ apiFetch }))

const agent = (task_id: string, stage: string | undefined, glance: string, fields: Partial<AgentInfo> = {}): AgentInfo => ({
  id: 1, task_id, task_title: `${task_id} title`, tmux_session: "", command: "claude", agent_kind: "claude-code",
  status: "running", activity: "", started_at: "now", working_seconds: 0, waiting_seconds: 0, error_seconds: 0,
  stage, glance, ...fields,
})

const agents = [
  agent("LEG-438", "exploring", "your_turn"),
  agent("LEG-406", "exploring", "working", { activity: "working" }),
  agent("LEG-412", "planning", "needs_you", { plan_pending: true }),
  agent("LEG-409", "implementing", "needs_you", { activity: "waiting" }),
  agent("LEG-407", "implementing", "working", { activity: "working" }),
  agent("LEG-408", "reviewing", "working", { activity: "background" }),
  agent("LEG-405", "finished", "finished"),
  agent("REEVE", undefined, "your_turn", { reeve: true }),
]

function show() {
  render(<AgentEnsemble agents={agents} onOpen={vi.fn()} onSpawn={vi.fn()} onShowList={vi.fn()} />)
}

beforeEach(() => {
  localStorage.clear()
  apiFetch.mockReset()
  apiFetch.mockResolvedValue(new Response("not available", { status: 404 }))
})
afterEach(cleanup)

describe("the ensemble's stages", () => {
  it("counts the agents by glance, leaving the Reeve out", () => {
    show()
    const counts = screen.getByRole("list", { name: "Agents by state" })
    expect(within(counts).getAllByRole("listitem").map((item) => item.textContent)).toEqual([
      "◆ 2 need you", "◇ 1 your turn", "● 3 working", "✓ 1 finished",
    ])
  })

  it("names each tile's stage beside its state", () => {
    show()
    const tile = screen.getByRole("article", { name: /LEG-409/ })
    expect(within(tile).getByRole("img", { name: "Implementing" })).toBeTruthy()
    expect(within(tile).getByText("Implementing")).toBeTruthy()
  })

  it("starts in tiles, and lays the agents out by stage when asked, remembering it", () => {
    show()
    expect(screen.queryByRole("region", { name: "Exploring" })).toBeNull()
    fireEvent.click(within(screen.getByRole("group", { name: "Layout" })).getByRole("button", { name: "By stage" }))
    const lanes = screen.getAllByRole("region").map((lane) => lane.getAttribute("aria-label")).filter((name) => name !== "Ensemble")
    expect(lanes).toEqual(["Exploring", "Planning", "Implementing", "Reviewing", "Finished"])
    const implementing = screen.getByRole("region", { name: "Implementing" })
    expect(within(implementing).getAllByRole("article").map((tile) => tile.getAttribute("aria-label")?.split(",")[0])).toEqual(["LEG-409", "LEG-407"])
    expect(screen.queryByRole("article", { name: /REEVE/ })).toBeNull()
    expect(localStorage.getItem(ENSEMBLE_LAYOUT_KEY)).toBe("stage")

    cleanup()
    show()
    expect(screen.getByRole("region", { name: "Planning" })).toBeTruthy()
  })

  it("opens by itself, on a phone, a stage holding an agent that needs you", () => {
    localStorage.setItem(ENSEMBLE_LAYOUT_KEY, "stage")
    show()
    const toggle = (name: string) => within(screen.getByRole("region", { name })).getByRole("button", { name: new RegExp(`^${name}`) })
    expect(toggle("Planning").getAttribute("aria-expanded")).toBe("true")
    expect(toggle("Implementing").getAttribute("aria-expanded")).toBe("true")
    expect(toggle("Exploring").getAttribute("aria-expanded")).toBe("false")
    expect(toggle("Finished").getAttribute("aria-expanded")).toBe("false")
    fireEvent.click(toggle("Exploring"))
    expect(toggle("Exploring").getAttribute("aria-expanded")).toBe("true")
  })

  it("opens a stage by itself when one of its agents comes to need you", () => {
    localStorage.setItem(ENSEMBLE_LAYOUT_KEY, "stage")
    const working = [agent("LEG-406", "exploring", "working", { activity: "working" })]
    const { rerender } = render(<AgentEnsemble agents={working} onOpen={vi.fn()} onSpawn={vi.fn()} onShowList={vi.fn()} />)
    const toggle = () => within(screen.getByRole("region", { name: "Exploring" })).getByRole("button", { name: /^Exploring/ })
    expect(toggle().getAttribute("aria-expanded")).toBe("false")
    rerender(<AgentEnsemble agents={[{ ...working[0], activity: "waiting", glance: "needs_you" }]} onOpen={vi.fn()} onSpawn={vi.fn()} onShowList={vi.fn()} />)
    expect(toggle().getAttribute("aria-expanded")).toBe("true")
  })
})
