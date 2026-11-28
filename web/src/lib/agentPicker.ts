import type { ConfigSetting } from "./configSettings"
import type { AgentPicker, PickerData } from "./settingPickers"

// An agent setting's row in Settings: its agent menu, and beside it a menu
// of the agent's models or tiers held in one partner key (modelOrTier) or
// two (tier and model). These derive the menus and plan each choice.

const agentNames: Record<string, string> = { "claude-code": "Claude Code", codex: "Codex", chimera: "Chimera" }
export const agentName = (id: string) => agentNames[id] ?? id

// unsetAgent is what an agent setting's empty choice says: the default
// agent by name, unless unset means something else (the workers' agent).
export function unsetAgent(defaultLabel: string | undefined, defaultAgent: string): string {
  const meansDefault = !defaultLabel || defaultLabel === "Same as the default agent" || defaultLabel in agentNames
  if (!meansDefault) return defaultLabel
  return defaultAgent ? `Default agent · ${agentName(defaultAgent)}` : "Default agent"
}

// agentUnsetLabel is the agent menu's empty choice: the fallback setting's
// agent where there is one, else what unsetAgent says.
export function agentUnsetLabel(picker: AgentPicker, defaultLabel: string | undefined, fallback: string, defaultAgent: string): string {
  if (picker.fallback) return `${defaultLabel ?? "Fallback"} · ${agentName(fallback)}`
  return unsetAgent(defaultLabel, defaultAgent)
}

export interface AgentPartners {
  modelOrTier?: ConfigSetting
  tier?: ConfigSetting
  model?: ConfigSetting
  // The partner the second menu stands for; the first of the three set.
  second?: ConfigSetting
}

export function agentPartners(picker: AgentPicker, settings: Record<string, ConfigSetting>): AgentPartners {
  const partner = (key?: string) => key ? settings[key] : undefined
  const modelOrTier = partner(picker.modelOrTier), tier = partner(picker.tier), model = partner(picker.model)
  return { modelOrTier, tier, model, second: modelOrTier ?? tier ?? model }
}

// fallbackAgent is the agent an unset setting takes: its fallback
// setting's (the conductor takes the workers'), else the default agent.
export function fallbackAgent(picker: AgentPicker, data: PickerData): string {
  const fallback = picker.fallback ? String(data.settings[picker.fallback]?.value ?? "") : ""
  return fallback || data.defaultAgent
}

// secondOptions is the model menu: the agent's tiers where a key holds a
// tier, its models where one holds a model.
export function secondOptions({ modelOrTier, tier, model }: AgentPartners, tiers: string[], models: string[]) {
  return [
    ...(modelOrTier || tier ? tiers.map((t) => ({ value: `tier:${t}`, label: `Tier: ${t}` })) : []),
    ...(modelOrTier || model ? models.map((m) => ({ value: `model:${m}`, label: m })) : []),
  ]
}

// secondChoice is what the model menu shows: current is the saved value,
// empty when unset, and value its option, a one-key value read as a tier
// when the agent has a tier of that name, else as a model.
export function secondChoice({ modelOrTier, tier, model }: AgentPartners, tiers: string[]): { current: string; value: string } {
  if (modelOrTier) {
    const current = String(modelOrTier.value ?? "")
    if (current === "") return { current, value: current }
    return { current, value: tiers.includes(current) ? `tier:${current}` : `model:${current}` }
  }
  const current = tier?.value ? `tier:${tier.value}` : model?.value ? `model:${model.value}` : ""
  return { current, value: current }
}

// A plan saves one key after clearing others; it stops at the first clear
// refused. A final step without a value resets its key.
export interface PickerPlan {
  resets: string[]
  then?: { key: string; value?: string }
}

export function parseChoice(choice: string): [kind: string, name: string] {
  if (choice === "") return ["", ""]
  const colon = choice.indexOf(":")
  return [choice.slice(0, colon), choice.slice(colon + 1)]
}

// planSecond saves a model or tier. Two keys hold one or the other: the
// other is cleared first, since the server refuses a tier beside a model.
export function planSecond(partners: AgentPartners, choice: string): PickerPlan {
  const [kind, name] = parseChoice(choice)
  const { modelOrTier, tier, model } = partners
  if (modelOrTier) return { resets: [], then: { key: modelOrTier.key, value: name === "" ? undefined : name } }
  const resets = ([[tier, "tier"], [model, "model"]] as const)
    .filter(([s, k]) => s?.value && kind !== k)
    .map(([s]) => s!.key)
  const target = kind === "tier" ? tier : kind === "model" ? model : undefined
  return { resets, then: target && { key: target.key, value: name } }
}

// planAgent saves an agent, after clearing a model or tier the new agent
// lacks: it means nothing to the new one.
export function planAgent(setting: ConfigSetting, partners: AgentPartners, choice: string, next: string, data: PickerData): PickerPlan {
  const known = [...(data.tiers[next] ?? []), ...(data.models[next] ?? [])]
  const resets = [partners.modelOrTier, partners.tier, partners.model]
    .filter((s): s is ConfigSetting => Boolean(s?.value) && !known.includes(String(s!.value)))
    .map((s) => s.key)
  return { resets, then: { key: setting.key, value: choice === "" ? undefined : choice } }
}

// runPlan carries out a plan, each step only once the one before it is done:
// the server checks the whole configuration at every save.
export async function runPlan(plan: PickerPlan, setKey: (key: string, value: unknown) => Promise<boolean>, resetKey: (key: string) => Promise<boolean>) {
  for (const key of plan.resets) {
    if (!await resetKey(key)) return
  }
  if (!plan.then) return
  const { key, value } = plan.then
  await (value === undefined ? resetKey(key) : setKey(key, value))
}
