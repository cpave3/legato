import type { ConfigSetting } from "./configSettings"

// How a setting's value reads, and how a number counted in bytes is shown.

export function isZero(value: unknown): boolean {
  return value === undefined || value === null || value === "" || value === 0 || value === false || (Array.isArray(value) && value.length === 0)
}

// The unit a bytes value is shown in: MB or KB when it divides evenly.
export function byteUnit(value: number): { label: string; factor: number } {
  if (value > 0 && value % 1048576 === 0) return { label: "MB", factor: 1048576 }
  if (value > 0 && value % 1024 === 0) return { label: "KB", factor: 1024 }
  return { label: "bytes", factor: 1 }
}

// formatValue is a value as a person reads it, with its unit.
export function formatValue(setting: Pick<ConfigSetting, "kind" | "unit" | "default_label">, value: unknown): string {
  if (isZero(value) && setting.default_label && setting.kind !== "bool") return setting.default_label
  if (setting.kind === "bool") return value ? "On" : "Off"
  if (Array.isArray(value)) return value.join(", ")
  if (typeof value === "number" && setting.unit === "bytes") {
    const unit = byteUnit(value)
    return `${value / unit.factor} ${unit.label}`
  }
  if (typeof value === "number" && setting.unit) return `${value} ${setting.unit}`
  return value === "" || value === undefined || value === null ? "none" : String(value)
}

const modeWords: Record<string, string> = {
  live: "Applies now",
  new_agents: "Applies to new agents",
  restart: "Applies at restart",
}

export function modeLabel(mode: string): string {
  return modeWords[mode] ?? ""
}
