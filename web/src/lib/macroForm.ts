import type { Adapters } from "./adapters"
import { agentKindLabel } from "./agentKind"
import { macroMessage, renameMacro, saveMacro, type AgentTier, type Macro, type MacroFields } from "./macros"

// The macro form's rules: what its name may be, which agents and tiers it
// offers, and what it saves.

// The fields the form starts with: the macro's, or empty for a new one.
export function macroFormStart(macro: Macro | null) {
  return {
    name: macro?.name ?? "",
    message: macro ? macroMessage(macro.keys) : "",
    applies: macro?.applies_to ?? "",
    agent: macro?.agent ?? "",
    tier: macro?.tier ?? "",
    account: macro?.account ?? "",
  }
}

// Macro names are matched without case or surrounding space.
export function sameMacroName(a: string, b: string): boolean {
  return a.trim().toLowerCase() === b.trim().toLowerCase()
}

// What the name typed means: taken by another macro, or a rename of the
// macro edited (which a built-in's name never is).
export function macroNameStatus(name: string, macro: Macro | null, macros: Macro[]): { renamed: boolean; taken: boolean } {
  const typed = name.trim() !== ""
  const isOwn = (other: string) => macro !== null && sameMacroName(other, macro.name)
  return {
    renamed: typed && macro !== null && !macro.builtin && !isOwn(name),
    taken: typed && macros.some((other) => sameMacroName(other.name, name) && !isOwn(other.name)),
  }
}

// Whether the form can be saved: a name, a message, and a name no other
// macro has.
export function canSaveMacro(name: string, message: string, taken: boolean): boolean {
  return Boolean(name.trim() && message.trim()) && !taken
}

// The agent a macro starts: the one it names, else Claude Code for an
// account (which is Claude Code's), else the default.
export function macroEffectiveAgent(agent: string, account: string, adapters: Adapters): string {
  return agent || (account ? "claude-code" : adapters.default)
}

// The agents offered: the default, each configured tool, and a tool the
// macro names that is no longer configured.
export function macroAgentChoices(adapters: Adapters, agent: string): [string, string][] {
  const choices: [string, string][] = [
    ["", adapters.default ? `Your default (${agentKindLabel(adapters.default)})` : "Your default"],
    ...adapters.adapters.map((kind): [string, string] => [kind, agentKindLabel(kind)]),
  ]
  if (agent && !adapters.adapters.includes(agent)) choices.push([agent, agentKindLabel(agent)])
  return choices
}

// The agent's tiers, and whether the tier set is not one of them.
export function macroTiers(tiers: Record<string, AgentTier[]>, agent: string, tier: string): { agentTiers: AgentTier[]; unknownTier: boolean } {
  const agentTiers = tiers[agent] ?? []
  return { agentTiers, unknownTier: tier !== "" && !agentTiers.some((t) => t.name === tier) }
}

// The tier kept when the agent changes: the same, if the new agent (or the
// default, for none) has it.
export function tierAfterAgent(tiers: Record<string, AgentTier[]>, next: string, defaultAgent: string, tier: string): string {
  return (tiers[next || defaultAgent] ?? []).some((t) => t.name === tier) ? tier : ""
}

// An account the settings do not list, kept so that saving keeps it.
export function unlistedAccount(adapters: Adapters, account: string): boolean {
  return account !== "" && !(adapters.accounts ?? []).some((a) => a.name === account)
}

// Whether the Account list shows: for Claude Code, with accounts to pick or
// one set already.
export function macroAccountsShown(agent: string, adapters: Adapters, account: string): boolean {
  return agent === "claude-code" && ((adapters.accounts?.length ?? 0) > 0 || account !== "")
}

// Saves a new macro under its name, or the macro edited under its own name
// and then renames it when the name changed (never a built-in's).
export async function persistMacro(baseUrl: string, macro: Macro | null, name: string, fields: MacroFields): Promise<void> {
  const typed = name.trim()
  if (macro === null) return saveMacro(baseUrl, typed, fields)
  await saveMacro(baseUrl, macro.name, fields)
  if (!macro.builtin && typed !== macro.name) await renameMacro(baseUrl, macro.name, typed)
}

// What the macro is saved with.
export function macroFields(message: string, applies: string, agent: string, tier: string, account: string): MacroFields {
  return { message, applies_to: applies as MacroFields["applies_to"], agent, tier, ...(account ? { account } : {}) }
}
