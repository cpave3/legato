import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { CardContextMenu } from "../CardContextMenu"
import { cardWorkflowOptions } from "../../../lib/card-workflows"
import type { BoardCard } from "../../../lib/board-types"

const card = { id: "T-1", title: "Context card" } as BoardCard
const workflow = { definition: { schema_version: 1, name: "triage", title: "Triage", source: "triage.yaml", steps: [] }, taskInput: "subject" } as never

afterEach(cleanup)

describe("CardContextMenu", () => {
  it("clamps to the viewport and dismisses with Escape or outside click", () => {
    Object.defineProperty(window, "innerWidth", { value: 320, configurable: true })
    Object.defineProperty(window, "innerHeight", { value: 300, configurable: true })
    const onClose = vi.fn()
    const { rerender } = render(<CardContextMenu card={card} position={{ x: 999, y: 999 }} onAction={() => {}} onClose={onClose} />)
    const menu = screen.getByRole("menu")
    expect(menu.style.left).toBe("72px")
    expect(menu.style.top).toBe("96px")

    fireEvent.keyDown(window, { key: "Escape" })
    expect(onClose).toHaveBeenCalledTimes(1)

    onClose.mockClear()
    rerender(<CardContextMenu card={card} position={{ x: 10, y: 10 }} onAction={() => {}} onClose={onClose} />)
    fireEvent.pointerDown(screen.getByTestId("card-context-backdrop"))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it("emits existing card actions", () => {
    const onAction = vi.fn()
    render(<CardContextMenu card={card} position={{ x: 10, y: 10 }} onAction={onAction} onClose={() => {}} />)
    const open = screen.getByRole("menuitem", { name: "Open details" })
    const move = screen.getByRole("menuitem", { name: "Move" })
    open.focus()
    fireEvent.keyDown(window, { key: "ArrowDown" })
    expect(document.activeElement).toBe(move)
    fireEvent.click(move)
    expect(onAction).toHaveBeenCalledWith("move")
  })

  it("opens a scrollable workflow flyout without closing on internal scroll", () => {
    const onClose = vi.fn()
    render(<CardContextMenu card={card} position={{ x: 10, y: 10 }} workflows={[workflow]} onAction={() => {}} onClose={onClose} />)
    fireEvent.mouseEnter(screen.getByRole("menuitem", { name: "Workflows" }))
    const flyout = screen.getByRole("menu", { name: "Workflows for Context card" })
    fireEvent.scroll(flyout)
    expect(onClose).not.toHaveBeenCalled()
    expect(screen.getByRole("menuitem", { name: "Triage" })).toBeTruthy()

    fireEvent.scroll(document.body)
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it("flips the workflow flyout left near the viewport edge", () => {
    Object.defineProperty(window, "innerWidth", { value: 500, configurable: true })
    Object.defineProperty(window, "innerHeight", { value: 300, configurable: true })
    render(<CardContextMenu card={card} position={{ x: 490, y: 40 }} workflows={[workflow]} onAction={() => {}} onClose={() => {}} />)
    fireEvent.mouseEnter(screen.getByRole("menuitem", { name: "Workflows" }))
    const primary = screen.getByRole("menu", { name: "Actions for Context card" })
    const flyout = screen.getByRole("menu", { name: "Workflows for Context card" })
    expect(Number.parseInt(flyout.style.left)).toBeLessThan(Number.parseInt(primary.style.left))
    expect(flyout.style.maxHeight).not.toBe("")
  })

  it("enters and leaves the flyout with keyboard controls", () => {
    const onClose = vi.fn()
    const onWorkflow = vi.fn()
    render(<CardContextMenu card={card} position={{ x: 10, y: 10 }} workflows={[workflow]} onAction={() => {}} onWorkflow={onWorkflow} onClose={onClose} />)
    const trigger = screen.getByRole("menuitem", { name: "Workflows" })
    trigger.focus()
    fireEvent.keyDown(window, { key: "ArrowRight" })
    const item = screen.getByRole("menuitem", { name: "Triage" })
    expect(document.activeElement).toBe(item)
    fireEvent.keyDown(window, { key: "ArrowLeft" })
    expect(document.activeElement).toBe(trigger)
    fireEvent.keyDown(window, { key: "ArrowRight" })
    fireEvent.keyDown(window, { key: "Enter" })
    expect(onWorkflow).toHaveBeenCalledWith(workflow)

    trigger.focus()
    fireEvent.keyDown(window, { key: "Enter" })
    fireEvent.keyDown(window, { key: "Escape" })
    expect(onClose).not.toHaveBeenCalled()
    expect(document.activeElement).toBe(trigger)
    fireEvent.keyDown(window, { key: "Escape" })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it("shows only definitions with exactly one task input, including triggered definitions", () => {
    const base = { schema_version: 1, title: "Test", source: "test.yaml", steps: [] }
    const options = cardWorkflowOptions([
      { ...base, name: "one", title: "One task", inputs: { subject: { type: "task" } }, on: { task: { entered_column: { columns: ["Done"] } } } },
      { ...base, name: "zero", inputs: { label: { type: "string" } } },
      { ...base, name: "two", inputs: { source: { type: "task" }, target: { type: "task" } } },
    ] as never)
    expect(options.map(option => [option.definition.name, option.taskInput])).toEqual([["one", "subject"]])
  })
})
