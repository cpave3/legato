// The fleet of LEG-620: a milestone-to-stack agent with seven Claude Code
// subagents, one per ticket. One works; six have ended their turns and wait
// on the go test runs they started, which Claude Code reports as background
// shells whose parent is the subagent.
import type { BackgroundJob } from "../hooks/useWebSocket"

const started = "2026-11-24T10:00:00Z"

export const fleetSubagent = (id: string, ticket: string, state: string, values: Partial<BackgroundJob> = {}): BackgroundJob =>
  ({ kind: "subagent", id, label: `general-purpose: ${ticket}`, state, started_at: started, ...values })

export const fleetShell = (id: string, parent: string | undefined, label = "go test ./internal/service"): BackgroundJob =>
  ({ kind: "bash", id, label, started_at: started, ...(parent ? { parent } : {}) })

// The subagents waiting on shells, and how many shells each waits on.
export const waitingSubagents: Array<[id: string, ticket: string, shells: number]> = [
  ["a2", "LEG-562 ticket", 2],
  ["a3", "LEG-563 settings", 1],
  ["a4", "LEG-564 typed rate limits", 3],
  ["a5", "LEG-565 store", 1],
  ["a6", "LEG-566 docs", 2],
  ["a7", "LEG-567 web", 1],
]

export function subagentFleet(): BackgroundJob[] {
  return [
    fleetSubagent("a1", "LEG-561 bridge", "working"),
    ...waitingSubagents.flatMap(([id, ticket, shells]) => [
      fleetSubagent(id, ticket, "background"),
      ...Array.from({ length: shells }, (_, n) => fleetShell(`${id}-sh${n + 1}`, id, `go test ./internal/service -run ${ticket.split(" ")[0]}-${n + 1}`)),
    ]),
  ]
}
