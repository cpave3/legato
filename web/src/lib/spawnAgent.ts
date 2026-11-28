import type { Adapters } from "./adapters"
import { agentKindLabel } from "./agentKind"
import { macroMessage, type Macro } from "./macros"

// The spawn dialog's agent tool, account and macro, the parts that are
// worked out.

export interface AgentOption {
  value: string
  label: string
  note: string
}

// The default follows the usage profile of the work's repository, which
// may differ from the global default, so it names no adapter; every
// adapter, the global default's too, stays an explicit override. A tool
// picked that the server no longer lists stays listed; the shell is last.
export function agentOptionsFor(adapters: Adapters | undefined, agentKind: string): AgentOption[] {
  const picked = agentKind && agentKind !== "shell" ? [agentKind] : []
  return [
    { value: "", label: "Default", note: "usage profile" },
    ...[...new Set([...(adapters?.adapters ?? []), ...picked])]
      .map((adapter) => ({ value: adapter, label: agentKindLabel(adapter), note: "" })),
    { value: "shell", label: agentKindLabel("shell"), note: "" },
  ]
}

// The Account list's first line: the saved session's account while it is
// resumed, else the default's for an explicit tool, else the usage
// profile's.
export function accountPlaceholder(resuming: boolean, agentKind: string): string {
  if (resuming) return "Saved session account"
  return agentKind ? "Default account" : "Follow usage profile"
}

// Accounts are Claude Code's, offered when it runs and has some.
export function accountsOffered(effectiveAdapter: string, adapters: Adapters | undefined): boolean {
  return effectiveAdapter === "claude-code" && (adapters?.accounts?.length ?? 0) > 0
}

// What starting with a macro sets: its tier and account (both cleared by
// None), and for a macro its message and, when it names a tool or an
// account, the tool (an account is Claude Code's).
export interface MacroStart {
  tier: string
  account: string
  prompt?: string
  agentKind?: string
}

export function macroStart(macro: Macro | undefined): MacroStart {
  if (!macro) return { tier: "", account: "" }
  const start: MacroStart = { tier: macro.tier ?? "", account: macro.account ?? "", prompt: macroMessage(macro.keys) }
  if (macro.agent || macro.account) start.agentKind = macro.agent || "claude-code"
  return start
}
