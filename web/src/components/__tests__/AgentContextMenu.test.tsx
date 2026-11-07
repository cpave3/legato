import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { AgentContextMenu } from "../AgentContextMenu"
import type { AgentInfo } from "../../hooks/useWebSocket"

const agent = {
  id: 1,
  task_id: "TASK-42",
  task_title: "Context agent",
  tmux_session: "legato-TASK-42",
  command: "chimera",
  agent_kind: "chimera",
  status: "running",
  activity: "working",
  started_at: "2026-08-13T00:00:00Z",
  working_seconds: 1,
  waiting_seconds: 0,
  error_seconds: 0,
} as AgentInfo

afterEach(cleanup)

describe("AgentContextMenu", () => {
  it("clamps to the viewport and focuses its kill action", async () => {
    Object.defineProperty(window, "innerWidth", { value: 320, configurable: true })
    Object.defineProperty(window, "innerHeight", { value: 200, configurable: true })

    render(
      <AgentContextMenu
        agent={agent}
        position={{ x: 999, y: 999 }}
        entries={[{ label: "Kill agent", onSelect: () => {}, danger: true }]}
        onClose={() => {}}
      />,
    )

    const menu = screen.getByRole("menu", { name: "Actions for TASK-42" })
    const kill = screen.getByRole("menuitem", { name: "Kill agent" })
    expect(menu.style.left).toBe("72px")
    expect(menu.style.top).toBe("108px")
    await waitFor(() => expect(document.activeElement).toBe(kill))
  })

  it("keeps keyboard focus on its action and emits kill when activated", () => {
    const onKill = vi.fn()
    render(
      <AgentContextMenu
        agent={agent}
        position={{ x: 10, y: 10 }}
        entries={[{ label: "Kill agent", onSelect: onKill, danger: true }]}
        onClose={() => {}}
      />,
    )

    const kill = screen.getByRole("menuitem", { name: "Kill agent" })
    fireEvent.keyDown(window, { key: "ArrowDown" })
    expect(document.activeElement).toBe(kill)
    fireEvent.click(kill)
    expect(onKill).toHaveBeenCalledTimes(1)
  })

  it("lists every entry, moves focus with the arrow keys and closes after a selection", () => {
    const rename = vi.fn()
    const onClose = vi.fn()
    render(
      <AgentContextMenu
        agent={agent}
        position={{ x: 10, y: 10 }}
        entries={[
          { label: "Rename", onSelect: rename },
          { label: "Reload agent", onSelect: () => {} },
          { label: "Kill agent", onSelect: () => {}, danger: true },
        ]}
        onClose={onClose}
      />,
    )
    const items = screen.getAllByRole("menuitem")
    expect(items.map((item) => item.textContent)).toEqual(["Rename", "Reload agent", "Kill agent"])
    expect(document.activeElement).toBe(items[0])
    fireEvent.keyDown(window, { key: "ArrowDown" })
    expect(document.activeElement).toBe(items[1])
    fireEvent.keyDown(window, { key: "ArrowUp" })
    fireEvent.keyDown(window, { key: "ArrowUp" })
    expect(document.activeElement).toBe(items[2])
    fireEvent.click(items[0])
    expect(rename).toHaveBeenCalledTimes(1)
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it("dismisses with Escape, outside pointer-down, external scroll, or resize", () => {
    const onClose = vi.fn()
    const { rerender } = render(
      <AgentContextMenu
        agent={agent}
        position={{ x: 10, y: 10 }}
        entries={[]}
        onClose={onClose}
      />,
    )

    fireEvent.keyDown(window, { key: "Escape" })
    fireEvent.pointerDown(screen.getByTestId("agent-context-backdrop"))
    fireEvent.scroll(document.body)
    fireEvent.resize(window)
    expect(onClose).toHaveBeenCalledTimes(4)

    onClose.mockClear()
    rerender(
      <AgentContextMenu
        agent={agent}
        position={{ x: 10, y: 10 }}
        entries={[]}
        onClose={onClose}
      />,
    )
    fireEvent.pointerDown(screen.getByRole("menu"))
    fireEvent.scroll(screen.getByRole("menu"))
    expect(onClose).not.toHaveBeenCalled()
  })
})
