import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { AgentKindBadge } from "../AgentKindBadge"

afterEach(cleanup)

describe("AgentKindBadge", () => {
  it.each([
    ["claude-code", "Claude Code"],
    ["chimera", "Chimera"],
    ["codex", "Codex"],
    ["shell", "Shell"],
  ])("names a %s session as %s", (kind, name) => {
    render(<AgentKindBadge kind={kind} isNative={false} />)

    expect(screen.getByText(name)).not.toBeNull()
  })

  it("says whether the session is driven natively or through its terminal", () => {
    const { rerender } = render(<AgentKindBadge kind="claude-code" isNative />)
    expect(screen.getByLabelText("Claude Code, native session")).not.toBeNull()
    expect(screen.getByText("native")).not.toBeNull()

    rerender(<AgentKindBadge kind="claude-code" isNative={false} />)
    expect(screen.getByLabelText("Claude Code, terminal session")).not.toBeNull()
    expect(screen.getByText("terminal")).not.toBeNull()
  })

  it("shows an adapter it does not know by its own name rather than hiding it", () => {
    render(<AgentKindBadge kind="aider" isNative={false} />)

    expect(screen.getByText("aider")).not.toBeNull()
  })

  it("treats a session with no adapter as a shell", () => {
    render(<AgentKindBadge kind="" isNative={false} />)

    expect(screen.getByText("Shell")).not.toBeNull()
  })
})
