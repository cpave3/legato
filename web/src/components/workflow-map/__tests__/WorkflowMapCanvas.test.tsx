import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { mockReactFlow } from "../../../test/reactFlow"
import type { WorkflowMap } from "../../../lib/workflowMap"
import WorkflowMapCanvas, { type WorkflowMapCanvasProps } from "../WorkflowMapCanvas"

mockReactFlow()
afterEach(() => { cleanup(); vi.unstubAllGlobals() })

const on = (trigger: string, enabled: boolean) => ({ trigger, enabled, file_enabled: enabled, overridden: false })
const map: WorkflowMap = {
  nodes: [
    { id: "ticket-to-merged-pr", name: "ticket-to-merged-pr", title: "Ticket to merged pull request", tags: ["delivery"], triggers: [on("task.entered_column", false)] },
    { id: "review-loop", name: "review-loop", title: "Review loop", built_in: true, source: "builtin:review-loop" },
    { id: "design-build", name: "design-build", title: "Design build", triggers: [on("schedule", true)] },
    { id: "one-off-task", name: "one-off-task", title: "One-off task" },
    { id: "broken", name: "broken", broken: true, error: "steps: oops" },
  ],
  edges: [
    { id: "ticket-to-merged-pr->review-loop", source: "ticket-to-merged-pr", target: "review-loop", calls: 2, in_loop: true, steps: ["review", "again.review"] },
  ],
}

function draw(props: Partial<WorkflowMapCanvasProps> = {}) {
  const onOpen = vi.fn()
  render(<div style={{ width: 900, height: 600 }}><WorkflowMapCanvas map={map} context={new Set()} onOpen={onOpen} {...props} /></div>)
  return { onOpen }
}
const nodeOf = async (name: RegExp) => (await screen.findByRole("button", { name })).closest<HTMLElement>(".react-flow__node")!
const faint = (element: HTMLElement) => element.querySelector("[data-map-node]")!.className.includes("opacity-30")

describe("the workflow map", () => {
  it("draws every workflow with its title, name, markers and state", async () => {
    draw()
    const ticket = await nodeOf(/^Ticket to merged pull request/)
    expect(ticket.textContent).toContain("ticket-to-merged-pr")
    expect(ticket.textContent).toContain("delivery")
    expect(screen.getByRole("button", { name: /^Ticket to merged pull request.*when a card moves.*paused/i })).toBeTruthy()
    expect(screen.getByRole("button", { name: /^Review loop.*built in/i })).toBeTruthy()
    expect(screen.getByRole("button", { name: /^Design build.*on a schedule.*on/i })).toBeTruthy()
    expect(screen.getByRole("button", { name: /^One-off task.*run by hand/i })).toBeTruthy()
    expect(screen.getByRole("button", { name: /^broken.*does not parse/i })).toBeTruthy()
    expect(screen.getByRole("group", { name: /stand alone, 3 workflows/i })).toBeTruthy()
    expect(screen.getByText("2 calls · in a loop")).toBeTruthy()
  })

  it("lights a workflow's callers and callees while it is hovered, and dims the rest", async () => {
    draw()
    const ticket = await nodeOf(/^Ticket to merged pull request/)
    fireEvent.mouseEnter(ticket)
    expect(faint(await nodeOf(/^Review loop/))).toBe(false)
    expect(faint(await nodeOf(/^One-off task/))).toBe(true)
    fireEvent.mouseLeave(ticket)
    expect(faint(await nodeOf(/^One-off task/))).toBe(false)
  })

  it("opens a workflow on click, and on a touch screen on the second tap", async () => {
    const { onOpen } = draw()
    fireEvent.click(await nodeOf(/^Review loop/))
    expect(onOpen).toHaveBeenLastCalledWith(expect.objectContaining({ id: "review-loop" }))
    // A broken node is handed over whole, its file with it, not by name.
    fireEvent.click(await nodeOf(/^broken/))
    expect(onOpen).toHaveBeenLastCalledWith(expect.objectContaining({ id: "broken", broken: true }))
    cleanup()
    vi.stubGlobal("matchMedia", (query: string) => ({ matches: query === "(hover: none)", addEventListener() {}, removeEventListener() {} }))
    const touch = draw()
    // A tap focuses the node before it clicks it.
    fireEvent.focus(await nodeOf(/^Ticket to merged pull request/))
    fireEvent.click(await nodeOf(/^Ticket to merged pull request/))
    expect(touch.onOpen).not.toHaveBeenCalled()
    expect(faint(await nodeOf(/^One-off task/))).toBe(true)
    fireEvent.click(await nodeOf(/^Ticket to merged pull request/))
    expect(touch.onOpen).toHaveBeenLastCalledWith(expect.objectContaining({ id: "ticket-to-merged-pr" }))
  })

  it("draws workflows kept only as context faint", async () => {
    draw({ context: new Set(["review-loop"]) })
    expect(faint(await nodeOf(/^Review loop/))).toBe(true)
    expect(faint(await nodeOf(/^Ticket to merged pull request/))).toBe(false)
  })
})

describe("framing the map", () => {
  it("fits the map again when its workflows change, as a tag filter changes them", async () => {
    // A phone-width frame: the map's width is fitted, so a wide map is
    // zoomed out and a single workflow is not.
    Object.defineProperty(HTMLElement.prototype, "clientWidth", { configurable: true, get() { return 390 } })
    try {
      const wide: WorkflowMap = { nodes: Array.from({ length: 6 }, (_, i) => ({ id: `w${i}`, name: `w${i}` })), edges: [] }
      const onOpen = vi.fn()
      const { rerender } = render(<div style={{ width: 390, height: 600 }}><WorkflowMapCanvas map={wide} context={new Set()} onOpen={onOpen} /></div>)
      await nodeOf(/^w0/)
      const scale = () => document.querySelector<HTMLElement>(".react-flow__viewport")!.style.transform
      await waitFor(() => expect(scale()).toMatch(/scale\(0\./))
      rerender(<div style={{ width: 390, height: 600 }}><WorkflowMapCanvas map={{ nodes: [wide.nodes[5]], edges: [] }} context={new Set()} onOpen={onOpen} /></div>)
      await waitFor(() => expect(scale()).toContain("scale(1)"))
    } finally {
      delete (HTMLElement.prototype as { clientWidth?: number }).clientWidth
    }
  })
})
