import { render, screen, fireEvent, cleanup } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, describe, expect, it } from "vitest"
import { RelatedWorkStrip } from "../RelatedWorkStrip"
import type { RelatedWorkItem } from "../../lib/task-activity"

afterEach(cleanup)

const items: RelatedWorkItem[] = [
  { kind: "plan", id: "pl-1", title: "Add search", status: "proposed", href: "/plans/pl-1" },
  { kind: "review", id: "rt-1", title: "Auth review", status: "ready", href: "/review/rt-1" },
  { kind: "workflow_run", id: "wr-1", title: "Triage", status: "running", href: "/workflows/runs/wr-1" },
]

describe("RelatedWorkStrip", () => {
  it("renders nothing when there are no items", () => {
    const { container } = render(<MemoryRouter><RelatedWorkStrip items={[]} /></MemoryRouter>)
    expect(container.firstChild).toBeNull()
  })

  it("renders links to plans, reviews, and workflows with status labels", () => {
    render(<MemoryRouter><RelatedWorkStrip items={items} /></MemoryRouter>)
    expect(screen.getByRole("link", { name: /Add search/ }).getAttribute("href")).toBe("/plans/pl-1")
    expect(screen.getByText("proposed")).toBeTruthy()
    expect(screen.getByRole("link", { name: /Auth review/ }).getAttribute("href")).toBe("/review/rt-1")
    expect(screen.getByText("ready")).toBeTruthy()
    expect(screen.getByRole("link", { name: /Triage/ }).getAttribute("href")).toBe("/workflows/runs/wr-1")
    expect(screen.getByText("running")).toBeTruthy()
  })

  it("shows a domain icon per kind", () => {
    render(<MemoryRouter><RelatedWorkStrip items={items} /></MemoryRouter>)
    expect(screen.getAllByLabelText("Plan").length).toBeGreaterThan(0)
    expect(screen.getAllByLabelText("Review").length).toBeGreaterThan(0)
    expect(screen.getAllByLabelText("Workflow").length).toBeGreaterThan(0)
  })

  it("collapses beyond the threshold and expands on toggle", () => {
    const many: RelatedWorkItem[] = Array.from({ length: 8 }, (_, i) => ({
      kind: "plan" as const, id: `pl-${i}`, title: `Plan ${i}`, status: "proposed", href: `/plans/pl-${i}`,
    }))
    render(<MemoryRouter><RelatedWorkStrip items={many} /></MemoryRouter>)
    // Only the threshold number of links visible initially.
    const visibleLinks = screen.getAllByRole("link")
    expect(visibleLinks.length).toBeLessThan(many.length)
    // Expand button present.
    const expand = screen.getByRole("button", { name: /show all/i })
    fireEvent.click(expand)
    expect(screen.getAllByRole("link").length).toBe(many.length)
  })
})
