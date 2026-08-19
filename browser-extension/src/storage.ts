import { DEFAULT_CONFIG, normalizeConfig, type ExtensionConfig } from "./config"

const CONFIG_KEY = "legatoConfig"

export async function loadConfig(): Promise<ExtensionConfig> {
  const stored = await chrome.storage.local.get(CONFIG_KEY)
  return normalizeConfig({ ...DEFAULT_CONFIG, ...(stored[CONFIG_KEY] as Partial<ExtensionConfig> | undefined) })
}

export async function saveConfig(config: ExtensionConfig): Promise<ExtensionConfig> {
  const normalized = normalizeConfig(config)
  await chrome.storage.local.set({ [CONFIG_KEY]: normalized })
  return normalized
}

export async function clearToken(): Promise<void> {
  const config = await loadConfig()
  await chrome.storage.local.set({ [CONFIG_KEY]: { ...config, token: "" } })
}
