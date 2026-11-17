// Background shells: commands an agent started that run on after its tool
// call returned (a dev server, a test watcher, a long build).
//
// They reach the panel two ways, in one shape:
// - from the agent's native endpoint, in its snapshot (backgroundShells) and
//   live (background_shell, background_shell_output), with their output.
//   Claude Code's bridge serves them so.
// - from Legato's record of what the agent's mod reported (the agent's
//   background_shells in /api/agents), without output. Chimera's come so
//   until Chimera lets a mod read a shell's output (CHIM-95); its endpoint
//   can then serve them as the bridge does, and those win.

export type ShellStatus = "running" | "exited" | "killed"

export interface NativeShellOutput {
  // False when nothing can read this shell's output.
  available: boolean
  // The newest output, at most shellOutputLimit characters.
  text: string
  // Older output was dropped.
  truncated?: boolean
}

export interface NativeBackgroundShell {
  shellId: string
  // The tool call that started it, when known.
  callId?: string
  command: string
  description?: string
  status: ShellStatus
  exitCode?: number
  // Milliseconds since the epoch.
  startedAt?: number
  endedAt?: number
  output?: NativeShellOutput
}

// A shell as /api/agents lists it (the store's BackgroundShell).
export interface StoredBackgroundShell {
  id: string
  command: string
  status: ShellStatus | string
  exit_code?: number
  started_at?: string
  ended_at?: string
}

// How much output the browser keeps for each shell: the bridge keeps as much.
export const shellOutputLimit = 32 * 1024

export interface ShellOutputEvent {
  shellId?: string
  chunk?: string
  // Output was skipped before this chunk: it starts over from here.
  gap?: boolean
  // The file was started over.
  reset?: boolean
}

// withShell puts a shell's start or end in the list, keeping the output
// already gathered for it.
export function withShell(shells: NativeBackgroundShell[], shell: NativeBackgroundShell): NativeBackgroundShell[] {
  const index = shells.findIndex((each) => each.shellId === shell.shellId)
  if (index < 0) return [...shells, { ...shell, output: shell.output ?? { available: true, text: "" } }]
  const known = shells[index]
  const output = known.output ? { ...known.output, available: shell.output?.available ?? known.output.available } : shell.output
  const next = [...shells]
  next[index] = { ...known, ...shell, output }
  return next
}

// withShellOutput adds what a shell printed.
export function withShellOutput(shells: NativeBackgroundShell[], event: ShellOutputEvent): NativeBackgroundShell[] {
  const index = shells.findIndex((each) => each.shellId === event.shellId)
  if (index < 0) return shells
  const known = shells[index]
  const fresh = event.gap || event.reset
  let text = (fresh ? "" : known.output?.text ?? "") + (event.chunk ?? "")
  let truncated = event.gap ? true : event.reset ? false : known.output?.truncated ?? false
  if (text.length > shellOutputLimit) {
    text = text.slice(text.length - shellOutputLimit)
    truncated = true
  }
  const next = [...shells]
  next[index] = { ...known, output: { available: true, text, truncated } }
  return next
}

function millis(time: string | undefined): number | undefined {
  if (!time) return undefined
  const ms = Date.parse(time)
  return Number.isNaN(ms) ? undefined : ms
}

// fromStored reads the shells Legato recorded from an agent's mod.
export function fromStored(stored: StoredBackgroundShell[] | undefined): NativeBackgroundShell[] {
  return (stored ?? []).map((shell) => ({
    shellId: shell.id,
    command: shell.command,
    status: shell.status === "exited" || shell.status === "killed" ? shell.status : "running",
    exitCode: shell.exit_code,
    startedAt: millis(shell.started_at),
    endedAt: millis(shell.ended_at),
    output: { available: false, text: "" },
  }))
}

// mergeShells is every shell the panel knows: the endpoint's, then those
// only Legato recorded.
export function mergeShells(native: NativeBackgroundShell[], stored: NativeBackgroundShell[]): NativeBackgroundShell[] {
  const known = new Set(native.map((shell) => shell.shellId))
  return [...native, ...stored.filter((shell) => !known.has(shell.shellId))]
}

// The escape sequences a terminal program writes for colour and the cursor.
// eslint-disable-next-line no-control-regex
const terminalEscapes = /\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\)|[@-Z\\-_])/g

// plainOutput is output as text, without the terminal's escape sequences or
// the carriage returns a progress bar redraws itself with.
export function plainOutput(text: string): string {
  return text
    .replace(terminalEscapes, "")
    .split("\n")
    .map((line) => {
      const redrawn = line.replace(/\r+$/, "").split("\r")
      return redrawn[redrawn.length - 1]
    })
    .join("\n")
}

// lastLine is the newest line a shell printed that says something.
export function lastLine(text: string): string {
  const lines = plainOutput(text).split("\n")
  for (let index = lines.length - 1; index >= 0; index--) {
    if (lines[index].trim()) return lines[index].trim()
  }
  return ""
}
