import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { AgentChatHeader } from "../AgentChatHeader"

afterEach(cleanup)

describe("AgentChatHeader", () => {
  it("says what the agent is doing, on which task, with which tool", () => {
    render(<AgentChatHeader state="running" time="5h 44m" title="Complete the Cover section" taskId="AG-10100" agentKind="claude-code" native menu={[]} />)
    const header = screen.getByRole("banner")
    expect(within(header).getByText(/Running/).textContent).toBe("● Running 5h 44m")
    expect(within(header).getByRole("heading", { name: "Complete the Cover section" })).toBeTruthy()
    expect(within(header).getByText("AG-10100")).toBeTruthy()
    expect(within(header).getByLabelText("Claude Code, native session")).toBeTruthy()
  })

  it("leaves the state to the agent picker on a phone, except a request to bring into view", () => {
    const { rerender } = render(<AgentChatHeader state="running" time="5h 44m" title="T" taskId="t-1" menu={[]} />)
    const chip = within(screen.getByRole("banner")).getByText(/Running/)
    expect(chip.className.split(" ")).toContain("max-sm:hidden")

    rerender(<AgentChatHeader state="needs-you" time="3m" title="T" taskId="t-1" menu={[]} onShowRequest={() => {}} />)
    const request = screen.getByRole("button", { name: "Needs you" })
    expect(request.className.split(" ")).not.toContain("max-sm:hidden")
    // The picker above it already says how long.
    expect(within(request).getByText("3m", { exact: false }).className.split(" ")).toContain("max-sm:hidden")
  })

  it("makes the needs-you state a button that brings the request into view", () => {
    const onShowRequest = vi.fn()
    render(<AgentChatHeader state="needs-you" title="T" taskId="t-1" menu={[]} onShowRequest={onShowRequest} />)
    fireEvent.click(screen.getByRole("button", { name: "Needs you" }))
    expect(onShowRequest).toHaveBeenCalledTimes(1)
  })

  it("names the task's pull request beside its ID, and offers no Open card", () => {
    render(<AgentChatHeader state="running" title="T" taskId="AG-10100" pullRequest={1334} menu={[]} />)
    const header = screen.getByRole("banner")
    expect(within(header).getByText("AG-10100 · PR #1334")).toBeTruthy()
    expect(within(header).queryByRole("button", { name: "Open card" })).toBeNull()
  })

  it("switches the side column between related work and artifacts", () => {
    const onSelect = vi.fn()
    const panels = { open: "related" as const, related: { count: 3, live: true }, artifacts: { count: 2, fresh: 1 }, onSelect }
    render(<AgentChatHeader state="idle" title="T" taskId="t-1" menu={[]} panels={panels} />)
    const sides = screen.getByRole("group", { name: "Side panel" })

    const related = within(sides).getByRole("button", { name: "Related work (3), in progress" })
    const artifacts = within(sides).getByRole("button", { name: "Artifacts (2), 1 new" })
    expect(related.getAttribute("aria-pressed")).toBe("true")
    expect(artifacts.getAttribute("aria-pressed")).toBe("false")
    fireEvent.click(artifacts)
    expect(onSelect).toHaveBeenCalledWith("artifacts")
    fireEvent.click(related)
    expect(onSelect).toHaveBeenCalledWith("related")
  })

  it("offers artifacts only once the agent has shown some", () => {
    const panels = { open: null, related: { count: 1, live: false }, artifacts: { count: 0, fresh: 0 }, onSelect: vi.fn() }
    render(<AgentChatHeader state="idle" title="T" taskId="t-1" menu={[]} panels={panels} />)
    expect((screen.getByRole("button", { name: "Artifacts (0)" }) as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getByRole("button", { name: "Related work (1)" }).getAttribute("aria-pressed")).toBe("false")
  })

  it("offers a studio as a tool of its own, apart from what the agent made", () => {
    const onSelect = vi.fn()
    const onOpen = vi.fn()
    render(<AgentChatHeader state="idle" title="T" taskId="t-1" menu={[]} panels={{ open: null, related: { count: 1, live: false }, artifacts: { count: 2, fresh: 0 }, onSelect, studio: { onOpen } }} />)

    // A studio is somewhere the person works, not one more thing to read:
    // it has its own button, and is not counted among the artifacts.
    fireEvent.click(screen.getByRole("button", { name: "Studio" }))
    expect(onOpen).toHaveBeenCalledTimes(1)
    expect(onSelect).not.toHaveBeenCalled()
    expect(screen.getByRole("button", { name: "Artifacts (2)" })).toBeTruthy()
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
