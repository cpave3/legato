import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, describe, expect, it, vi } from "vitest"
import { WorkflowCanvas, type WorkflowCanvasProps } from "../WorkflowCanvas"
import type { WorkflowGraphModel } from "../../../lib/workflow"

// The canvas on its own, with the graph stood in for: its docks, its
// notices, the Panels button and keys, and the dock that steps aside.

vi.mock("../../workflow-graph/LazyWorkflowGraph", () => ({
  LazyWorkflowGraph: ({ canvas }: { canvas: { insets: unknown; focusId: unknown; tools: React.ReactNode } }) =>
    <div data-testid="graph" data-insets={JSON.stringify(canvas.insets)} data-focus={String(canvas.focusId)}>{canvas.tools}</div>,
}))

afterEach(() => { cleanup(); vi.restoreAllMocks() })

const graph = { graph: { version: 2, nodes: [], edges: [] } as WorkflowGraphModel }

function draw(props: Partial<WorkflowCanvasProps> = {}) {
  const onClosePanel = vi.fn()
  const view = render(<MemoryRouter><WorkflowCanvas label="Run graph" graph={graph} onClosePanel={onClosePanel} selected={false} hiddenPill="Steps" narrow={false} {...props} /></MemoryRouter>)
  return { onClosePanel, ...view }
}
const insets = () => JSON.parse(screen.getByTestId("graph").getAttribute("data-insets")!)

describe("on a wide screen", () => {
  it("docks the cards left and the panel right, and keeps the graph clear of them", () => {
    draw({ cards: <p>the cards</p>, panel: <p>the panel</p>, notices: <p>a notice</p>, focusId: "step:a" })
    expect(screen.getByRole("region", { name: "Run graph" })).toBeTruthy()
    expect(screen.getByText("the cards")).toBeTruthy()
    expect(screen.getByText("the panel")).toBeTruthy()
    expect(screen.getByText("a notice")).toBeTruthy()
    expect(insets()).toEqual({ top: 0, bottom: 0, left: 392, right: 412 })
    expect(screen.getByTestId("graph").getAttribute("data-focus")).toBe("step:a")
  })

  it("leaves the graph whole without docks, and takes a panel width", () => {
    draw({ panel: <p>p</p>, panelWidth: 480 })
    expect(insets()).toEqual({ top: 0, bottom: 0, left: 0, right: 512 })
  })

  it("says why there is no graph, in its place", () => {
    draw({ graph: null, empty: "Nothing to draw yet.", cards: <p>c</p> })
    const status = screen.getByRole("status")
    expect(status.textContent).toBe("Nothing to draw yet.")
    expect(status.style.paddingLeft).toBe("392px")
  })

  it("hides and shows the panels with the Panels button, and . when not typing", () => {
    draw({ cards: <p>the cards</p>, panel: <p>the panel</p> })
    const panels = screen.getByRole("button", { name: "Panels" })
    fireEvent.click(panels)
    expect(panels.getAttribute("aria-pressed")).toBe("false")
    expect(screen.queryByText("the cards")).toBeNull()
    expect(screen.queryByText("the panel")).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: /Show panels/ }))
    expect(screen.getByText("the cards")).toBeTruthy()
    fireEvent.keyDown(document, { key: "." })
    expect(screen.queryByText("the cards")).toBeNull()
    fireEvent.keyDown(document, { key: ".", ctrlKey: true })
    expect(screen.queryByText("the cards")).toBeNull()
    fireEvent.keyDown(document, { key: "." })
    expect(screen.getByText("the cards")).toBeTruthy()
  })

  it("closes the panel with Escape while there is a selection, but not from a field", () => {
    const { onClosePanel, rerender } = draw({ selected: true, panel: <input aria-label="A field" /> })
    fireEvent.keyDown(screen.getByLabelText("A field"), { key: "Escape" })
    expect(onClosePanel).not.toHaveBeenCalled()
    fireEvent.keyDown(document, { key: "Escape" })
    expect(onClosePanel).toHaveBeenCalledTimes(1)
    rerender(<MemoryRouter><WorkflowCanvas label="Run graph" graph={graph} onClosePanel={onClosePanel} selected={false} hiddenPill="Steps" narrow={false} /></MemoryRouter>)
    fireEvent.keyDown(document, { key: "Escape" })
    expect(onClosePanel).toHaveBeenCalledTimes(1)
  })

  it("steps the dock aside for the panel where there is no room for both, until asked for", () => {
    vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(1000)
    draw({ dockYields: true, cards: <p>the cards</p>, panel: <p>the panel</p> })
    expect(screen.queryByText("the cards")).toBeNull()
    expect(screen.getByText("the panel")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: /^Steps.*Show panels/ }))
    expect(screen.getByText("the cards")).toBeTruthy()
  })

  it("keeps both docks where there is room", () => {
    vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(2000)
    draw({ dockYields: true, cards: <p>the cards</p>, panel: <p>the panel</p> })
    expect(screen.getByText("the cards")).toBeTruthy()
  })
})

describe("on a phone", () => {
  it("puts the notices above the graph, the cards in a sheet along the bottom, and the panel in a sheet", () => {
    const { onClosePanel } = draw({ narrow: true, notices: <p>a notice</p>, cards: <p>the cards</p>, panel: <p>the panel</p>, panelTitle: "Step A" })
    expect(screen.getByText("a notice")).toBeTruthy()
    expect(insets()).toEqual({ top: 0, left: 0, right: 0, bottom: 220 })
    const sheet = screen.getByRole("dialog", { name: "Step A" })
    expect(sheet.textContent).toContain("the panel")
    fireEvent.click(screen.getByTestId("node-sheet-backdrop"))
    expect(onClosePanel).toHaveBeenCalled()
  })

  it("has no bottom inset without cards", () => {
    draw({ narrow: true })
    expect(insets()).toEqual({ top: 0, left: 0, right: 0, bottom: 0 })
  })
})
