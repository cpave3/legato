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
  it("renders only a trigger button when closed and there are items", () => {
    render(<MemoryRouter><RelatedWorkStrip items={items} /></MemoryRouter>)
    expect(screen.getByRole("button", { name: /related work/i })).toBeTruthy()
    // Links not visible until opened.
    expect(screen.queryByRole("link", { name: /Add search/ })).toBeNull()
  })

  it("renders nothing when there are no items", () => {
    const { container } = render(<MemoryRouter><RelatedWorkStrip items={[]} /></MemoryRouter>)
    expect(container.firstChild).toBeNull()
  })

  it("opens a drawer with links to plans, reviews, and workflows on trigger click", () => {
    render(<MemoryRouter><RelatedWorkStrip items={items} /></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: /related work/i }))
    expect(screen.getByRole("link", { name: /Add search/ }).getAttribute("href")).toBe("/plans/pl-1")
    expect(screen.getByText("proposed")).toBeTruthy()
    expect(screen.getByRole("link", { name: /Auth review/ }).getAttribute("href")).toBe("/review/rt-1")
    expect(screen.getByText("ready")).toBeTruthy()
    expect(screen.getByRole("link", { name: /Triage/ }).getAttribute("href")).toBe("/workflows/runs/wr-1")
    expect(screen.getByText("running")).toBeTruthy()
  })

  it("shows a domain icon per kind in the drawer", () => {
    render(<MemoryRouter><RelatedWorkStrip items={items} /></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: /related work/i }))
    expect(screen.getAllByLabelText("Plan").length).toBeGreaterThan(0)
    expect(screen.getAllByLabelText("Review").length).toBeGreaterThan(0)
    expect(screen.getAllByLabelText("Workflow").length).toBeGreaterThan(0)
  })

  it("closes the drawer on close button click", () => {
    render(<MemoryRouter><RelatedWorkStrip items={items} /></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: /related work/i }))
    expect(screen.getByRole("link", { name: /Add search/ })).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: /close/i }))
    expect(screen.queryByRole("link", { name: /Add search/ })).toBeNull()
  })

  it("closes on backdrop click", () => {
    const { container } = render(<MemoryRouter><RelatedWorkStrip items={items} /></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: /related work/i }))
    expect(screen.getByRole("link", { name: /Add search/ })).toBeTruthy()
    const backdrop = container.querySelector("[data-related-work-backdrop]")
    expect(backdrop).toBeTruthy()
    fireEvent.click(backdrop!)
    expect(screen.queryByRole("link", { name: /Add search/ })).toBeNull()
  })

  it("shows an item count badge on the trigger", () => {
    render(<MemoryRouter><RelatedWorkStrip items={items} /></MemoryRouter>)
    expect(screen.getByRole("button", { name: /related work/i }).textContent).toContain("3")
  })
})
