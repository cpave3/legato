import { describe, expect, it } from "vitest"
import { render, screen, within } from "@testing-library/react"
import { TileDeck } from "../TileDeck"
import { createNativeSyncState, reduceNativeEvent, type NativeConversationItem, type NativeEvent } from "../../../lib/nativeAgentSync"
import { subagentTiles } from "../subagentTiles"
import { fleetShell, fleetSubagent } from "../../../test/subagentFleet"
import type { BackgroundJob } from "../../../lib/agentWire"

// A Claude Code subagent as the bridge reports it: spawned in the background,
// its launch answered, then its own turn ended while its shells still run.
function finishedSubagent(reason = "stop"): NativeConversationItem[] {
  const events: NativeEvent[] = [
    { eventId: "start", type: "tool_call_start", callId: "call-a2", name: "spawn_agent", args: { purpose: "LEG-564 typed rate limits", background: true } },
    { eventId: "spawned", type: "subagent_spawned", subagentId: "a2", parentCallId: "call-a2", purpose: "LEG-564 typed rate limits" },
    { eventId: "result", type: "tool_call_result", callId: "call-a2", result: { status: "running" } },
    { eventId: "finished", type: "subagent_finished", subagentId: "a2", parentCallId: "call-a2", reason },
  ]
  return events.reduce(reduceNativeEvent, createNativeSyncState()).items
}

const waitingOnShells: BackgroundJob[] = [
  fleetSubagent("a2", "LEG-564 typed rate limits", "background"),
  fleetShell("s1", "a2", "go test ./internal/service"),
  fleetShell("s2", "a2", "go test ./internal/engine/store"),
  fleetShell("s3", "a2", "go vet ./..."),
]

function deck(items: NativeConversationItem[], jobs?: BackgroundJob[]) {
  render(<TileDeck title="Sub-agents" tiles={subagentTiles(items, jobs)} layout="grid" focused={null} onFocus={() => {}} />)
}

describe("subagent tiles", () => {
  it("keeps a subagent that waits on shells live, saying so, with its shells", () => {
    deck(finishedSubagent(), waitingOnShells)

    const tile = within(screen.getByRole("list", { name: "Running" })).getByRole("article", { name: "LEG-564 typed rate limits, waiting on 3 shells" })
    expect(within(tile).getByText(/waiting on 3 shells/i)).not.toBeNull()
    const shells = within(tile).getByRole("list", { name: "Shells" })
    expect(within(shells).getAllByRole("listitem").map((item) => item.textContent)).toEqual([
      expect.stringContaining("go test ./internal/service"),
      expect.stringContaining("go test ./internal/engine/store"),
      expect.stringContaining("go vet ./..."),
    ])
    expect(screen.queryByRole("list", { name: "Finished" })).toBeNull()
    expect(within(screen.getByRole("list", { name: "By state" })).getByText(/1 waiting on work/)).not.toBeNull()
  })

  it("puts a subagent Legato no longer lists on the shelf as done", () => {
    deck(finishedSubagent(), [fleetShell("own", undefined)])

    expect(screen.queryByRole("list", { name: "Running" })).toBeNull()
    expect(within(screen.getByRole("list", { name: "Finished" })).getByRole("button", { name: /LEG-564 typed rate limits/ })).not.toBeNull()
  })

  it("shows a subagent a shell woke up again as running, and one that asks as needing the operator", () => {
    expect(subagentTiles(finishedSubagent(), [fleetSubagent("a2", "LEG-564", "working")])[0].state).toBe("running")
    expect(subagentTiles(finishedSubagent(), [fleetSubagent("a2", "LEG-564", "waiting")])[0].state).toBe("waiting")
  })

  it("never hides an error or an interruption", () => {
    expect(subagentTiles(finishedSubagent("error"), waitingOnShells)[0].state).toBe("error")
    expect(subagentTiles(finishedSubagent("interrupted"), waitingOnShells)[0].state).toBe("interrupted")
  })

  it("cannot match a spawn rehydrated without its subagent record, so it stays done", () => {
    // After a bridge restart a spawn call comes back with its result and no
    // subagent id to find it by (a known gap, outside LEG-620).
    const items: NativeConversationItem[] = [{ key: "call-a2", role: "tool", text: "", tool: { key: "call-a2", toolName: "spawn_agent", args: {}, result: { reason: "stop" } } }]
    expect(subagentTiles(items, waitingOnShells)[0].state).toBe("done")
  })
})
