import { apiFetch } from "./api"

// The Claude and Codex subscriptions' usage limits, as GET /api/usage gives
// them (internal/service/subscription_usage.go). See docs/ai/usage.md.

export type UsageState = "ok" | "stale" | "unavailable"

export interface UsageWindow {
  meter: string
  used_percent: number
  resets_at?: string
  window_minutes?: number
}

export interface ProviderUsage {
  provider: string
  state: UsageState
  stale: boolean
  reason?: string
  windows: UsageWindow[]
  credits?: { has_credits: boolean; unlimited: boolean; balance?: string }
  plan?: string
  as_of?: string
  checked_at?: string
  source?: string
}

export interface SubscriptionUsage {
  providers: ProviderUsage[]
  poll_interval_seconds: number
}

// The usage; none from an older server or one that cannot say.
export async function fetchSubscriptionUsage(baseUrl: string): Promise<SubscriptionUsage> {
  const empty = { providers: [], poll_interval_seconds: 0 }
  try {
    const response = await apiFetch(baseUrl, "/api/usage")
    if (!response.ok) return empty
    const body = await response.json()
    return Array.isArray(body?.providers) ? body : empty
  } catch {
    return empty
  }
}

// Asks the server to read the usage now; it arrives as usage_changed.
export async function refreshSubscriptionUsage(baseUrl: string): Promise<void> {
  const response = await apiFetch(baseUrl, "/api/usage/refresh", { method: "POST" })
  if (!response.ok && response.status !== 202) {
    const body = await response.json().catch(() => ({}))
    throw new Error(body?.error || `refresh failed (${response.status})`)
  }
}

export function providerName(provider: string): string {
  return provider === "claude" ? "Claude" : provider === "codex" ? "Codex" : provider
}

const meterLabels: Record<string, { short: string; long: string }> = {
  five_hour: { short: "5h", long: "5-hour" },
  weekly: { short: "wk", long: "weekly" },
  weekly_opus: { short: "Opus", long: "weekly Opus" },
  weekly_sonnet: { short: "Sonnet", long: "weekly Sonnet" },
}

export function meterLabel(meter: string, form: "short" | "long" = "long"): string {
  return meterLabels[meter]?.[form] ?? meter
}

// Why a provider's usage could not be read, as a short phrase (beside a
// stale reading) or a sentence (in place of one).
export function reasonPhrase(reason?: string): string {
  switch (reason) {
    case "not_read": return "not read yet"
    case "not_signed_in": return "not signed in"
    case "login_expired": return "login expired"
    case "no_subscription": return "no subscription limits"
    case "codex_missing": return "codex not installed"
    case "no_data": return "nothing reported yet"
    case "endpoint_changed": return "its usage report changed"
    case "rate_limited": return "asked too often; waiting"
    case "unreachable": return "could not reach it"
    default: return "could not read it"
  }
}

export function reasonSentence(provider: string, reason?: string): string {
  const tool = provider === "claude" ? "Claude Code" : "Codex"
  switch (reason) {
    case "not_read": return "Not read yet."
    case "not_signed_in": return `Sign in to ${tool} on this machine.`
    case "login_expired": return `${tool}'s login has expired; using ${tool} signs in again.`
    case "no_subscription": return "This login has no subscription limits to show."
    case "codex_missing": return "The codex command is not installed on this machine."
    case "no_data": return `${tool} has not reported any usage yet.`
    case "endpoint_changed": return `${tool}'s usage report has changed and Legato cannot read it.`
    case "rate_limited": return "Asked too often; Legato is waiting before it asks again."
    case "unreachable": return "Could not reach it."
    default: return "Could not read it."
  }
}

// A span in its two largest units: "2h", "5d 1h", "12m".
export function spanText(ms: number): string {
  const minutes = Math.max(1, Math.round(ms / 60000))
  const days = Math.floor(minutes / 1440)
  const hours = Math.floor(minutes / 60) % 24
  const mins = minutes % 60
  if (days > 0) return hours > 0 ? `${days}d ${hours}h` : `${days}d`
  if (hours > 0) return mins > 0 ? `${hours}h ${mins}m` : `${hours}h`
  return `${mins}m`
}

export function agoText(iso: string, now: Date): string {
  const ms = now.getTime() - new Date(iso).getTime()
  return ms < 60000 ? "just now" : `${spanText(ms)} ago`
}

export function resetText(iso: string | undefined, now: Date): string {
  if (!iso) return ""
  const ms = new Date(iso).getTime() - now.getTime()
  return ms > 0 ? `resets in ${spanText(ms)}` : `reset ${spanText(-ms)} ago`
}

// The tone of a bar: calm, then warning from 75%, then over from 90%.
export function usageTone(percent: number): "calm" | "warn" | "over" {
  return percent >= 90 ? "over" : percent >= 75 ? "warn" : "calm"
}
