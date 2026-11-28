import { describe, expect, it } from "vitest"
import { render, screen, within } from "@testing-library/react"
import { TileDeck } from "../TileDeck"
import { shellTileForCall, shellTiles, shellTileId } from "../shellTiles"
import type { NativeBackgroundShell } from "../../../lib/backgroundShells"

const running: NativeBackgroundShell = {
  shellId: "b1", callId: "toolu_1", command: "sleep 2; echo hi; sleep 30", description: "Say hi later", status: "running",
  startedAt: Date.now() - 5000, output: { available: true, text: "\x1b[32mhi\x1b[0m\n" },
}
const failed: NativeBackgroundShell = {
  shellId: "b2", command: "make test", status: "exited", exitCode: 3, startedAt: 0, endedAt: 42_000,
  output: { available: true, text: "FAIL pkg\n" },
}
const killed: NativeBackgroundShell = { shellId: "b3", command: "npm run dev", status: "killed", startedAt: 0, endedAt: 1000, output: { available: true, text: "" } }
const chimera: NativeBackgroundShell = { shellId: "c1", command: "cargo build", status: "exited", exitCode: 0, startedAt: 0, endedAt: 9000, output: { available: false, text: "" } }

describe("background shell tiles", () => {
  it("gives each shell a tile that says what it runs, its state and how it ended", () => {
    const tiles = shellTiles([running, failed, killed, chimera])

    expect(tiles.map((tile) => [tile.id, tile.title, tile.state, tile.count])).toEqual([
      ["shell:b1", "Say hi later", "running", undefined],
      ["shell:b2", "make test", "error", "exit 3"],
      ["shell:b3", "npm run dev", "killed", "killed"],
      ["shell:c1", "cargo build", "done", "exit 0"],
    ])
    expect(tiles[0].meta).toBe("$ sleep 2; echo hi; sleep 30")
    // The shelf's line is the newest thing it printed.
    expect(tiles[0].summary).toBe("hi")
    expect(tiles[1].summary).toBe("FAIL pkg")
  })

  it("shows a running shell's output live in the deck, and ended ones on the shelf", () => {
    render(<TileDeck title="Sub-agents and shells" tiles={shellTiles([running, failed])} layout="grid" focused={null} onFocus={() => {}} />)

    const live = screen.getByRole("list", { name: "Running" })
    const tile = within(live).getByRole("article", { name: "Say hi later, running" })
    expect(within(tile).getByLabelText("Output").textContent).toBe("hi\n")

    const shelf = screen.getByRole("list", { name: "Finished" })
    expect(within(shelf).getByRole("button", { name: /make test/ }).textContent).toContain("FAIL pkg")
  })

  it("opens a shell's output in full, and says when earlier output was dropped", () => {
    const long = { ...running, output: { available: true, text: "line 2\nline 3\n", truncated: true } }
    render(<TileDeck title="Shells" tiles={shellTiles([long])} layout="grid" focused={shellTileId("b1")} onFocus={() => {}} />)

    const opened = screen.getByRole("article", { name: "Say hi later, running" })
    expect(within(opened).getByLabelText("Output").textContent).toBe("line 2\nline 3\n")
    expect(within(opened).getByText("Earlier output is not kept.")).not.toBeNull()
    // The whole command is there too.
    expect(within(opened).getByText("sleep 2; echo hi; sleep 30", { selector: "pre" })).not.toBeNull()
  })

  it("says a Chimera shell's output is not available yet", () => {
    render(<TileDeck title="Shells" tiles={shellTiles([chimera])} layout="grid" focused={shellTileId("c1")} onFocus={() => {}} />)
    expect(screen.getByText(/Output isn’t available for this agent yet/)).not.toBeNull()
    expect(screen.getByRole("article", { name: "cargo build, done" })).not.toBeNull()
  })

  it("finds the tile of the shell a tool call started", () => {
    const tiles = shellTiles([running, failed])
    expect(shellTileForCall(tiles, [running, failed], "toolu_1")?.id).toBe("shell:b1")
    expect(shellTileForCall(tiles, [running, failed], "toolu_other")).toBeUndefined()
  })
})
