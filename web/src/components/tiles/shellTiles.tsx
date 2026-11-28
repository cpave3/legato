import type { DeckTile, TileState } from "./TileDeck"
import { ShellOutput } from "./ShellParts"
import { lastLine, type NativeBackgroundShell } from "../../lib/backgroundShells"

// How the native panel shows background shells in its TileDeck, beside its
// subagents: one tile for each shell, in the order they started.

// The tile of a shell, by its ID: shell IDs and tool call keys never meet.
export const shellTileId = (shellId: string) => `shell:${shellId}`

export function shellTiles(shells: NativeBackgroundShell[]): DeckTile[] {
  return shells.map(shellTile)
}

// shellTileForCall is the tile of the shell a tool call started, if any.
export function shellTileForCall(tiles: DeckTile[], shells: NativeBackgroundShell[], callId: string): DeckTile | undefined {
  const shell = shells.find((each) => each.callId === callId)
  return shell ? tiles.find((tile) => tile.id === shellTileId(shell.shellId)) : undefined
}

function tileState(shell: NativeBackgroundShell): TileState {
  if (shell.status === "running") return "running"
  if (shell.status === "killed") return "killed"
  return shell.exitCode !== undefined && shell.exitCode !== 0 ? "error" : "done"
}

function shellTile(shell: NativeBackgroundShell): DeckTile {
  const command = shell.command.trim()
  const firstLine = command.split("\n")[0] || "Background shell"
  const state = tileState(shell)
  const printed = shell.output?.available ? lastLine(shell.output.text) : ""
  const ending = shell.status === "exited" ? shell.exitCode !== undefined ? `exit ${shell.exitCode}` : "ended" : shell.status === "killed" ? "killed" : undefined
  return {
    id: shellTileId(shell.shellId),
    title: shell.description || firstLine,
    meta: shell.description ? `$ ${firstLine}` : "background shell",
    state,
    startedAt: shell.startedAt,
    endedAt: shell.endedAt,
    count: ending,
    summary: printed || (shell.output?.available ? (state === "running" ? "No output yet" : ending ?? "") : ending ?? "running"),
    detail: command.includes("\n") || shell.description
      ? <pre className="m-0 whitespace-pre-wrap break-words rounded-md border border-lw-line bg-[var(--theme-canvas,#121216)] px-2.5 py-1.5 font-lw-mono text-xs text-lw-text-2">{command}</pre>
      : undefined,
    body: <ShellOutput shell={shell} />,
  }
}
