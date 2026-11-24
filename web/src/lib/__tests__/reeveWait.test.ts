import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { agentStripLook } from "../boardCard"
import { needsYouReason, reeveWaitLabel } from "../agentStage"
import { agentLook } from "../../components/agent-graph/look"
import type { BoardCard } from "../board-types"
import type { AgentInfo } from "../../hooks/useWebSocket"

const now = new Date("2026-11-24T05:12:00Z")
beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(now) })
afterEach(() => { vi.useRealTimers() })

const card = (fields: Partial<BoardCard>) => ({ agent_state: "idle", working_seconds: 0, waiting_seconds: 0, error_seconds: 0, ...fields }) as unknown as BoardCard
const agent = (fields: Partial<AgentInfo>): AgentInfo => ({
  id: 1, task_id: "LEG-613", task_title: "", tmux_session: "", command: "claude", agent_kind: "claude-code",
  status: "running", activity: "", started_at: "", working_seconds: 0, waiting_seconds: 0, error_seconds: 0, ...fields,
})

describe("an agent waiting on the Reeve", () => {
  it("is Waiting on the Reeve on its card, for as long as it has waited, not your turn", () => {
    const look = agentStripLook(card({ glance: "working", stage: "implementing", waiting_on: "reeve", waiting_since: "2026-11-24T05:00:00Z" }))
    expect(look.label).toBe("Waiting on the Reeve")
    expect(look.glyph).toBe("◈")
    expect(look.time).toBe(12 * 60)
  })

  it("needs you on its card once the Reeve has not answered", () => {
    const look = agentStripLook(card({ glance: "needs_you", waiting_on: "reeve", waiting_since: "2026-11-24T05:00:00Z" }))
    expect(look.label).toBe("Needs you")
    expect(look.reason).toBe("The Reeve hasn't answered")
  })

  it("says why it needs you, and what it waits on in the rail", () => {
    expect(needsYouReason(agent({ glance: "needs_you", waiting_on: "reeve" }))).toBe("The Reeve hasn't answered")
    expect(needsYouReason(agent({ glance: "working", waiting_on: "reeve" }))).toBe("")
    expect(reeveWaitLabel(agent({ glance: "working", waiting_on: "reeve" }))).toBe("◈ Waiting on the Reeve")
    expect(reeveWaitLabel(agent({ glance: "working" }))).toBe("")
  })

  it("is waiting on Reeve on the graph", () => {
    expect(agentLook(agent({ glance: "working", waiting_on: "reeve" })).label).toBe("waiting on Reeve")
    expect(agentLook(agent({ glance: "needs_you", waiting_on: "reeve" })).label).toBe("needs you")
  })
})
