import { useSettings } from "./useSettings"

export function useVoiceEnabled(baseUrl: string): boolean {
  return Boolean(useSettings(baseUrl).data?.voice_enabled)
}
