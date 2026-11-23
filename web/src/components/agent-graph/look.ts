// How an agent's state looks on the graph: the same words, glyphs and
// colours as the ensemble's tiles, as a border the state can be read from
// at a glance.
import type { AgentInfo } from "../../hooks/useWebSocket"
import { glanceSection } from "../../lib/agentStage"

export interface Look {
  label: string
  glyph: string
  // The node's border, and the colour of its state word.
  border: string
  text: string
}

export function agentLook(agent: AgentInfo): Look {
  if (agent.activity === "error") return { label: "error", glyph: "✕", border: "border-lw-error", text: "text-lw-error" }
  if (agent.activity === "waiting" || glanceSection(agent) === "needs-you") return { label: "needs you", glyph: "◆", border: "border-lw-needs", text: "text-lw-needs" }
  switch (agent.activity) {
    case "working":
      return { label: "running", glyph: "●", border: "border-lw-live", text: "text-lw-live" }
    case "background":
      return { label: "background", glyph: "◌", border: "border-dashed border-lw-live", text: "text-lw-live" }
    default:
      return { label: "idle", glyph: "○", border: "border-lw-line-strong", text: "text-lw-text-2" }
  }
}

// subagentLook is a sub-agent's: working, waiting on the operator, or in
// the background (its turn ended and work it started still runs).
export function subagentLook(state: string | undefined): Look {
  switch (state) {
    case "waiting":
      return { label: "needs you", glyph: "◆", border: "border-lw-needs", text: "text-lw-needs" }
    case "background":
      return { label: "background", glyph: "◌", border: "border-dashed border-lw-live", text: "text-lw-live" }
    default:
      return { label: "working", glyph: "●", border: "border-lw-live", text: "text-lw-live" }
  }
}
