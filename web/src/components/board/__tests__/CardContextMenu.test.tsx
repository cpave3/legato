import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { CardContextMenu } from "../CardContextMenu"
import { cardWorkflowOptions } from "../../../lib/card-workflows"
import type { BoardCard } from "../../../lib/board-types"

const card = { id: "T-1", title: "Context card" } as BoardCard

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
