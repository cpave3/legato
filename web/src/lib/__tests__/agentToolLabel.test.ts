import { describe, expect, it } from "vitest"
import { agentToolLabel } from "../agentKind"
import { agentStripLook } from "../boardCard"
import type { BoardCard } from "../board-types"

describe("an agent's engram beside its tool", () => {
  it("names the engram after the tool", () => {
    expect(agentToolLabel("claude-code", "hestia")).toBe("Claude Code · hestia")
    expect(agentToolLabel("codex", undefined)).toBe("Codex")
    expect(agentToolLabel("codex", "")).toBe("Codex")
  })

  it("is the tool a running card's strip names in its tooltip, not its words", () => {
    const card = { agent_state: "working", agent_kind: "claude-code", agent_engram: "hestia", working_seconds: 0 } as unknown as BoardCard
    expect(agentStripLook(card).tool).toBe("Claude Code · hestia")
    expect(agentStripLook(card).label).toBe("Working")
  })
})
