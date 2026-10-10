// The agent tools Legato launches, by adapter name: the name each is shown
// under, the binary it runs as, and the color that sets it apart in a list.
interface AgentKind {
  label: string
  binary?: string
  tone: string
}

const neutralTone = "border-zinc-700 bg-zinc-900 text-zinc-400"

const agentKinds: Record<string, AgentKind> = {
  "claude-code": { label: "Claude Code", binary: "claude", tone: "border-orange-800/60 bg-orange-950/40 text-orange-300" },
  chimera: { label: "Chimera", binary: "chimera", tone: "border-violet-800/60 bg-violet-950/40 text-violet-300" },
  codex: { label: "Codex", binary: "codex", tone: "border-sky-800/60 bg-sky-950/40 text-sky-300" },
  shell: { label: "Shell", tone: neutralTone },
}

// A session with no adapter is a plain shell; an adapter this build does not
// know keeps its own name so it is never shown as something it is not.
function resolve(kind: string | undefined): AgentKind {
  if (!kind) return agentKinds.shell
  return agentKinds[kind] ?? { label: kind, tone: neutralTone }
}

export function agentKindLabel(kind: string | undefined): string {
  return resolve(kind).label
}

export function agentKindTone(kind: string | undefined): string {
  return resolve(kind).tone
}

// The agent tools Legato can drive through a native endpoint beside their
// terminal: Chimera serves one itself, the others through Legato's bridges.
const nativeKinds = new Set(["chimera", "claude-code", "codex"])

export function canBeNative(kind: string | undefined): boolean {
  return kind !== undefined && nativeKinds.has(kind)
}

// The name the native panel's wording uses for the tool it shows. The panel
// was Chimera's first, so a session that names no tool is one.
export function nativeToolName(kind: string | undefined): string {
  return kind ? agentKindLabel(kind) : "Chimera"
}

// Whether the pane's foreground process is the agent tool itself. When it is
// not (a shell after the tool exited, an editor), the process is worth naming.
export function isAgentToolInForeground(kind: string | undefined, command: string | undefined): boolean {
  const binary = resolve(kind).binary
  if (!binary || !command) return false
  const executable = command.trim().split(/\s+/)[0] ?? ""
  return executable.split("/").pop() === binary
}
