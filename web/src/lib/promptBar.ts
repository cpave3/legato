import type { PromptState } from "./agentWire"

// The terminal prompt bar's decisions, apart from its markup. See
// components/PromptBar.tsx.

// While the message box is empty, these keys go to the terminal as tmux
// names, so the operator can arrow through a tool's choices and press Enter
// to pick one.
const passthrough: Record<string, string> = {
  ArrowUp: "Up",
  ArrowDown: "Down",
  Enter: "Enter",
  Escape: "Escape",
  Tab: "Tab",
}

export function terminalKey(key: string, input: string): string | undefined {
  if (input) return undefined
  return passthrough[key]
}

const askingTypes = new Set(["tool_approval", "plan_approval", "question", "menu"])

// askingPrompt is the prompt whose choices the bar offers in place of the
// message box: only while the agent waits for input, which avoids false
// positives from pattern-matching mid-output, and only a kind that asks.
export function askingPrompt(activity: string | undefined, promptState: PromptState | null): { type: string; actions: NonNullable<PromptState["actions"]> } | null {
  if (activity !== "waiting" || !promptState?.actions) return null
  if (!askingTypes.has(promptState.type)) return null
  return { type: promptState.type, actions: promptState.actions }
}

// Claude Code and Codex run a message that starts with "! " as a shell
// command.
export function isBashCommand(kind: string | undefined, input: string): boolean {
  return ["claude-code", "claude", "codex"].includes(kind || "") && input.startsWith("! ")
}

// The colour of a choice: green for yes, red for no, else the accent.
export function choiceTone(label: string): string {
  if (label === "Yes" || label === "Accept" || label === "Always") return "text-emerald-400"
  if (label === "No" || label === "Reject") return "text-red-400"
  return "text-indigo-400"
}

// choiceStep is where Down or j, and Up or k, move the highlighted choice;
// null for any other key.
export function choiceStep(key: string, selected: number, count: number): number | null {
  if (key === "ArrowDown" || key === "j") return Math.min(selected + 1, count - 1)
  if (key === "ArrowUp" || key === "k") return Math.max(selected - 1, 0)
  return null
}
