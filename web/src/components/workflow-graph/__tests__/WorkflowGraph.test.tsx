import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, describe, expect, it, vi } from "vitest"
import { mockReactFlow } from "../../../test/reactFlow"
import type { GraphNode, WorkflowGraphModel } from "../../../lib/workflow"
import WorkflowGraph, { type WorkflowGraphProps } from "../WorkflowGraph"

// The graph on its own: choosing nodes by click, key and pane, the plain
// controls and minimap, and a canvas's toolbar.

mockReactFlow()
afterEach(cleanup)

const step = (id: string, title: string, more: Partial<GraphNode> = {}, group?: "review_loop"): GraphNode =>
  ({ id: `step:${id}`, kind: "step", step: { id, path: id, title, type: "task.note", start: "automatic", key: {}, definition: { id, title } as never, ...(group ? { group } : {}) }, ...more })
const graph: WorkflowGraphModel = {
  version: 2,
  nodes: [
    { id: "trigger:manual", kind: "trigger", trigger: { kind: "manual", summary: "Run by hand", enabled: true } as never },
    step("prep", "Prepare"),
    step("review", "Review it", {}, "review_loop"),
    { id: "inner:pass", kind: "inner", parent: "step:review", inner: { role: "pass", title: "Review pass" } },
    { id: "inner:fixes", kind: "inner", parent: "step:review", inner: { role: "fixes", title: "Author fixes" } },
  ],
  edges: [
    { id: "start", kind: "start", source: "trigger:manual", target: "step:prep" },
    { id: "order", kind: "order", source: "step:prep", target: "step:review" },
    { id: "loop1", kind: "loop", source: "inner:pass", target: "inner:fixes" },
    { id: "loop2", kind: "loop", source: "inner:fixes", target: "inner:pass" },
  ],
}

function draw(props: Partial<WorkflowGraphProps> = {}) {
  const onSelect = vi.fn()
  render(<MemoryRouter><div style={{ width: 800, height: 600 }}><WorkflowGraph graph={graph} onSelect={onSelect} {...props} /></div></MemoryRouter>)
  return { onSelect }
}
const nodeOf = async (name: RegExp) => (await screen.findByRole("button", { name })).closest<HTMLElement>(".react-flow__node")!

describe("choosing nodes", () => {
  it("chooses a node by click, its group for an inner node, and none on the pane", async () => {
    const { onSelect } = draw()
    fireEvent.click(await nodeOf(/^Step: Prepare/))
    expect(onSelect).toHaveBeenLastCalledWith("step:prep")
    fireEvent.click(document.querySelector<HTMLElement>('.react-flow__node[data-id="inner:pass"]')!)
    expect(onSelect).toHaveBeenLastCalledWith("step:review")
    fireEvent.click(document.querySelector<HTMLElement>(".react-flow__pane")!)
    expect(onSelect).toHaveBeenLastCalledWith(null)
  })

  it("chooses a focused node with Enter or Space, and marks the one chosen", async () => {
    const { onSelect } = draw({ selectedId: "step:prep" })
    const prep = await nodeOf(/^Step: Prepare/)
    expect(prep.classList.contains("selected")).toBe(true)
    fireEvent.keyDown(prep, { key: "Enter" })
    expect(onSelect).toHaveBeenLastCalledWith("step:prep")
    fireEvent.keyDown(await nodeOf(/^Step: Review it/), { key: " " })
    expect(onSelect).toHaveBeenLastCalledWith("step:review")
    fireEvent.keyDown(document.querySelector<HTMLElement>('.react-flow__node[data-id="inner:fixes"]')!, { key: "Enter" })
    expect(onSelect).toHaveBeenLastCalledWith("step:review")
    onSelect.mockClear()
    fireEvent.keyDown(prep, { key: "a" })
    expect(onSelect).not.toHaveBeenCalled()
  })

  it("dims a trigger that did not start the run", async () => {
    draw({ extras: { "trigger:manual": { trigger: "other" } } })
    expect((await screen.findByRole("button", { name: /Run by hand/ })).closest(".react-flow__node")!.classList.contains("opacity-50")).toBe(true)
  })
})

describe("the plain graph", () => {
  it("has zoom controls and, left to right, a minimap", async () => {
    draw()
    await nodeOf(/^Step: Prepare/)
    expect(document.querySelector(".react-flow__controls")).toBeTruthy()
    expect(screen.getByLabelText("Workflow graph overview")).toBeTruthy()
    expect(screen.queryByRole("toolbar", { name: "View" })).toBeNull()
  })

  it("has no minimap top to bottom", async () => {
    draw({ direction: "TB" })
    await nodeOf(/^Step: Prepare/)
    expect(screen.queryByLabelText("Workflow graph overview")).toBeNull()
  })
})

describe("a canvas", () => {
  const insets = { top: 0, left: 0, right: 0, bottom: 0 }

  it("has a toolbar of zoom, Fit, Where it is and the page's tools", async () => {
    draw({ canvas: { insets, focusId: "step:prep", tools: <button type="button">Panels</button> } })
    const toolbar = await screen.findByRole("toolbar", { name: "View" })
    expect(document.querySelector(".react-flow__controls")).toBeNull()
    expect(toolbar.textContent).toMatch(/\d+%/)
    for (const name of ["Zoom out", "Zoom in", "Fit the whole flow", "Where it is", "Panels"]) expect(screen.getByRole("button", { name })).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Fit the whole flow" }))
    fireEvent.click(screen.getByRole("button", { name: "Where it is" }))
    fireEvent.click(screen.getByRole("button", { name: "Zoom in" }))
    fireEvent.click(screen.getByRole("button", { name: "Zoom out" }))
  })

  it("offers Where it is only with a step to centre on", async () => {
    draw({ canvas: { insets } })
    await screen.findByRole("toolbar", { name: "View" })
    expect(screen.queryByRole("button", { name: "Where it is" })).toBeNull()
  })

  it("moves the view to a node each time it is asked", async () => {
    const { rerender } = render(<MemoryRouter><WorkflowGraph graph={graph} canvas={{ insets }} centerOn={null} /></MemoryRouter>)
    await nodeOf(/^Step: Prepare/)
    const viewport = document.querySelector<HTMLElement>(".react-flow__viewport")!
    const before = viewport.style.transform
    rerender(<MemoryRouter><WorkflowGraph graph={graph} canvas={{ insets }} centerOn={{ id: "step:review", n: 1 }} /></MemoryRouter>)
    await waitFor(() => expect(viewport.style.transform).not.toBe(before))
  })
})
