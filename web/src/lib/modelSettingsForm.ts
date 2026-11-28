import type { ModelCapabilities, ModelSettings, ModelSettingsPatch, ModelSettingsState } from "./modelSettings"

// The form for a session's model settings keeps a draft of changes over the
// settings the model was read with. These helpers derive what each field
// shows from the read state, the draft and whether Reset was chosen.

export type ModelSettingCapability = ModelCapabilities["settings"][string]
export type ModelSettingValue = string | number | boolean | null

export interface ModelSettingsDraft {
  state: ModelSettingsState | null
  draft: ModelSettingsPatch
  reset: boolean
}

// supportedCapability is a setting's capability when the model takes it.
export function supportedCapability(state: ModelSettingsState | null, key: keyof ModelSettings): ModelSettingCapability | null {
  const capability = state?.capabilities.settings[key]
  return capability?.status === "supported" ? capability : null
}

// selectedSetting is the value a field shows: the draft's, else nothing
// after Reset, else the session's override.
export function selectedSetting({ state, draft, reset }: ModelSettingsDraft, key: keyof ModelSettings) {
  if (Object.hasOwn(draft, key)) return draft[key]
  return reset ? null : state?.overrides[key]
}

// currentThinking is the thinking mode the form would apply.
export function currentThinking({ state, draft, reset }: ModelSettingsDraft) {
  if (Object.hasOwn(draft, "thinking")) return draft.thinking ?? state?.defaults.thinking
  return reset ? state?.defaults.thinking : state?.effective.thinking
}

// needsThinking says the setting means nothing while thinking is off.
export function needsThinking(key: keyof ModelSettings) {
  return key === "reasoningEffort" || key === "thinkingBudgetTokens"
}

export function defaultSettingLabel(inherited: unknown) {
  return inherited === undefined ? "Default" : `Default (${String(inherited)})`
}

export function settingOptions(capability: ModelSettingCapability) {
  return capability.type === "boolean" ? ["true", "false"] : capability.values ?? []
}

export function settingOptionLabel(value: string) {
  if (value === "true") return "On"
  if (value === "false") return "Off"
  return value
}

// parseSelectedSetting reads a select's value: empty is the default, and a
// boolean setting's options are the strings true and false.
export function parseSelectedSetting(capability: ModelSettingCapability, raw: string): ModelSettingValue {
  if (raw === "") return null
  return capability.type === "boolean" ? raw === "true" : raw
}

export function parseNumberSetting(raw: string): ModelSettingValue {
  return raw === "" ? null : Number(raw)
}

// changeDraft sets one setting; turning thinking off also clears the
// settings that need it.
export function changeDraft(current: ModelSettingsPatch, key: keyof ModelSettings, value: ModelSettingValue): ModelSettingsPatch {
  const next = { ...current, [key]: value }
  if (key === "thinking" && value === "off") { next.reasoningEffort = null; next.thinkingBudgetTokens = null }
  return next
}

export function hasSupportedSetting(state: ModelSettingsState | null) {
  return !!state && Object.values(state.capabilities.settings).some((capability) => capability.status === "supported")
}

// unsupportedConfigured lists the settings with a default or an override
// that the model does not take.
export function unsupportedConfigured(state: ModelSettingsState | null) {
  return (Object.keys({ ...state?.defaults, ...state?.overrides }) as (keyof ModelSettings)[])
    .filter((key) => state?.capabilities.settings[key]?.status !== "supported")
}
