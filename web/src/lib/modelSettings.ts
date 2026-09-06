export type ModelSettings = Partial<Record<"thinking" | "reasoningEffort" | "thinkingBudgetTokens" | "reasoningSummary" | "verbosity" | "serviceTier" | "maxOutputTokens" | "temperature" | "topP" | "parallelToolCalls", string | number | boolean>>
export type ModelSettingsPatch = { [Key in keyof ModelSettings]?: ModelSettings[Key] | null }
export interface ModelCapabilities {
  schemaVersion: 1
  modelId: string
  profile: string
  source: string
  fingerprint: string
  settings: Record<string, {
    status: "supported" | "unsupported" | "unknown"
    type: "enum" | "number" | "boolean"
    values?: string[]
    min?: number
    max?: number
    integer?: boolean
    default?: string | number | boolean
    reason?: string
  }>
}
export interface ModelSettingsState {
  model: string
  overrides: ModelSettings
  defaults: ModelSettings
  effective: ModelSettings
  capabilities: ModelCapabilities
}

export const settingLabels: Record<keyof ModelSettings, string> = {
  thinking: "Thinking", reasoningEffort: "Reasoning effort", thinkingBudgetTokens: "Thinking budget",
  reasoningSummary: "Reasoning summary", verbosity: "Verbosity", serviceTier: "Service tier",
  maxOutputTokens: "Output limit", temperature: "Temperature", topP: "Top-p", parallelToolCalls: "Parallel tool calls",
}

export async function modelSettingsResponse(response: Response): Promise<unknown> {
  const body = await response.json().catch(() => null)
  if (!response.ok) {
    const error = body?.error
    throw new Error(typeof error === "string" ? error : error?.message ?? `Could not save model settings (${response.status})`)
  }
  return body
}
