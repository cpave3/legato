import { fireEvent, render, screen, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { BackgroundJob } from "../../lib/agentWire"
import { SubagentsLine } from "../SubagentTree"
import { fleetShell, subagentFleet } from "../../test/subagentFleet"

const job = (id: string, label: string, values: Partial<BackgroundJob> = {}): BackgroundJob =>
  ({ kind: "subagent", id, label, started_at: "2026-11-23T04:30:00Z", ...values })

// The live example of LEG-534: an orchestrator waits on the two it started.
const jobs: BackgroundJob[] = [
  job("bash1", "npm run dev", { kind: "bash" }),
  job("a1", "general-purpose: Orchestrate AG-10884", { state: "background" }),
  job("a2", "general-purpose: Implement D1 PROP-541", { parent: "a1", state: "working" }),
  job("a3", "general-purpose: Implement D2 PROP-542", { parent: "a1", state: "waiting" }),
]

describe("SubagentsLine", () => {
  it("says how many sub-agents work, and opens their tree", () => {
    render(<SubagentsLine jobs={jobs} />)

    const line = screen.getByRole("button", { name: "3 sub-agents · 1 working · 1 needs you · 1 waiting" })
    expect(screen.queryByRole("tree")).toBeNull()

    fireEvent.click(line)

    const tree = screen.getByRole("tree", { name: "Sub-agents" })
    const items = within(tree).getAllByRole("treeitem")
    expect(items.map((item) => [item.getAttribute("aria-level"), item.textContent])).toEqual([
      ["1", "Orchestrate AG-10884waiting on 2 sub-agents"],
      ["2", "Implement D1 PROP-541working"],
      ["2", "Implement D2 PROP-542needs you"],
    ])
  })

  it("is not drawn without sub-agents", () => {
    const { container } = render(<SubagentsLine jobs={[job("bash1", "npm run dev", { kind: "bash" })]} />)

    expect(container.innerHTML).toBe("")
  })

  it("draws a sub-agent whose parent is not listed at the top", () => {
    render(<SubagentsLine jobs={[job("a2", "general-purpose: Implement D1", { parent: "gone", state: "working" })]} />)

    fireEvent.click(screen.getByRole("button", { name: "1 sub-agent · working" }))

    expect(screen.getByRole("treeitem").getAttribute("aria-level")).toBe("1")
  })

  it("shows sub-agents that wait on shells, with their shells under them", () => {
    render(<SubagentsLine jobs={[...subagentFleet(), fleetShell("own", undefined, "npm run dev")]} />)

    fireEvent.click(screen.getByRole("button", { name: "7 sub-agents · 1 working · 6 waiting on shells" }))

    const items = within(screen.getByRole("tree", { name: "Sub-agents" })).getAllByRole("treeitem")
    const at = items.findIndex((item) => item.textContent?.startsWith("LEG-564 typed rate limits"))
    expect(items.slice(at, at + 4).map((item) => [item.getAttribute("aria-level"), item.textContent])).toEqual([
      ["1", "LEG-564 typed rate limitswaiting on 3 shells"],
      ["2", "$go test ./internal/service -run LEG-564-1"],
      ["2", "$go test ./internal/service -run LEG-564-2"],
      ["2", "$go test ./internal/service -run LEG-564-3"],
    ])
    // Every subagent and each of their shells; the agent's own shell is not in the tree.
    expect(items).toHaveLength(7 + 10)
    expect(items.some((item) => item.textContent?.includes("npm run dev"))).toBe(false)
  })
})
