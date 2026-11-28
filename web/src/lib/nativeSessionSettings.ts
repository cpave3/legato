import type { ModelSettingsState } from "./modelSettings"

// How a native session runs (its model and the model's effort) as the panel
// changes it.

// The efforts the model takes, as the session reports them.
export function effortChoices(settingsState: ModelSettingsState | undefined): string[] {
  const setting = settingsState?.capabilities.settings.reasoningEffort
  return setting?.status === "supported" && setting.type === "enum" ? setting.values ?? [] : []
}

// What a model change answered: the model now in force (the session's word
// for it first, then the endpoint's, then the one asked for) and its
// settings.
export function modelChangeResult(body: unknown, asked: string): { model: string; settingsState?: ModelSettingsState } {
  const result = (body ?? {}) as { to?: string; settingsState?: ModelSettingsState }
  return { model: result.settingsState?.model ?? result.to ?? asked, settingsState: result.settingsState }
}

// A change's answer stands while the session shows the model it changed
// from or the one it changed to; anything else came later.
export function modelChangeStands(current: string | undefined, from: string | undefined, to: string): boolean {
  return current === from || current === to
}

// The settings an effort change answered, if any.
export function effortChangeResult(body: unknown): ModelSettingsState | undefined {
  return (body as { settingsState?: ModelSettingsState } | null)?.settingsState
}
