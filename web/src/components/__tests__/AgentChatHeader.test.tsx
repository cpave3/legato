import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { AgentChatHeader } from "../AgentChatHeader"

afterEach(cleanup)

describe("AgentChatHeader", () => {
  it("says what the agent is doing, on which task, with which tool", () => {
    render(<AgentChatHeader state="running" time="5h 44m" title="Complete the Cover section" taskId="AG-10100" agentKind="claude-code" native menu={[]} />)
    const header = screen.getByRole("banner")
    expect(within(header).getByText("Running 5h 44m")).toBeTruthy()
    expect(within(header).getByRole("heading", { name: "Complete the Cover section" })).toBeTruthy()
    expect(within(header).getByText("AG-10100")).toBeTruthy()
    expect(within(header).getByLabelText("Claude Code, native session")).toBeTruthy()
  })

  it("makes the needs-you state a button that brings the request into view", () => {
    const onShowRequest = vi.fn()
    render(<AgentChatHeader state="needs-you" title="T" taskId="t-1" menu={[]} onShowRequest={onShowRequest} />)
    fireEvent.click(screen.getByRole("button", { name: "Needs you" }))
    expect(onShowRequest).toHaveBeenCalledTimes(1)
  })

  it("opens the agent's card on the board", () => {
    const onOpenCard = vi.fn()
    const { rerender } = render(<AgentChatHeader state="idle" title="T" taskId="t-1" menu={[]} />)
    expect(screen.queryByRole("button", { name: "Open card" })).toBeNull()
    rerender(<AgentChatHeader state="idle" title="T" taskId="t-1" menu={[]} onOpenCard={onOpenCard} />)
    fireEvent.click(screen.getByRole("button", { name: "Open card" }))
    expect(onOpenCard).toHaveBeenCalledTimes(1)
  })

  it("lists the agent's other actions under More actions, the dangerous ones last", () => {
    const refresh = vi.fn()
    const kill = vi.fn()
    render(<AgentChatHeader state="idle" title="T" taskId="t-1" menu={[
      { label: "Kill agent", onSelect: kill, danger: true },
      { label: "Refresh from snapshot", onSelect: refresh },
    ]} />)
    fireEvent.click(screen.getByRole("button", { name: "More actions" }))
    const menu = screen.getByRole("menu")
    expect(within(menu).getAllByRole("menuitem").map((item) => item.textContent)).toEqual(["Refresh from snapshot", "Kill agent"])
    fireEvent.click(within(menu).getByRole("menuitem", { name: "Refresh from snapshot" }))
    expect(refresh).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole("menu")).toBeNull()
  })

  it("shows notes beside the title, such as a mode or an error", () => {
    render(<AgentChatHeader state="idle" title="T" taskId="t-1" menu={[]} notes={<span>Syncing…</span>} />)
    expect(screen.getByText("Syncing…")).toBeTruthy()
  })
})
