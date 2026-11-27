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
  kind?: string
  account?: string
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

// The providers the server knows and the meters they report, as
// GET /api/usage's catalog gives them: every surface labels providers and
// meters from it, and rule and pin choices are its qualified meters.
export interface UsageCatalogMeter {
  window: string
  qualified: string
  short: string
  long: string
}

export interface UsageCatalogProvider {
  provider: string
  kind?: string
  account?: string
  name: string
  meters: UsageCatalogMeter[]
}

export function catalogProviderName(catalog: UsageCatalogProvider[] | undefined, provider: string): string {
  return catalog?.find((p) => p.provider === provider)?.name ?? provider
}

export function catalogMeterLabel(catalog: UsageCatalogProvider[] | undefined, meter: string, form: "short" | "long" = "long"): string {
  for (const p of catalog ?? []) {
    const m = p.meters.find((candidate) => candidate.window === meter)
    if (m) return form === "short" ? m.short : m.long
  }
  return meter
}

export function catalogQualifiedMeters(catalog: UsageCatalogProvider[] | undefined): string[] {
  return (catalog ?? []).flatMap((p) => p.meters.map((m) => m.qualified))
}

export interface UsageProfile {
  name: string
  agent: string
  tier?: string
  account?: string
  reviewer?: { agent: string; tier?: string; account?: string }
}

export interface UsageReason { text: string; rule?: string; meter?: string; reset?: string }
export interface UsagePin { profile: string; until?: string; until_reset?: string }
export interface UsagePolicy {
  active: { profile: UsageProfile; reason: UsageReason; since: string; pin?: UsagePin; error?: string }
  profiles: UsageProfile[]
  switches: { id: number; from_profile: string; to_profile: string; since: string; reason: string }[]
}

export async function setUsagePin(baseUrl: string, pin?: UsagePin): Promise<void> {
  const response = await apiFetch(baseUrl, "/api/usage/pin", pin
    ? { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(pin) }
    : { method: "DELETE" })
  if (!response.ok) {
    const body = await response.json().catch(() => ({}))
    throw new Error(body?.error || `Pin change failed (${response.status})`)
  }
}

export interface SubscriptionUsage {
  catalog?: UsageCatalogProvider[]
  policy?: UsagePolicy
  providers: ProviderUsage[]
  poll_interval_seconds: number
}

// Older servers have no usage route; other refusals reach the card.
export async function fetchSubscriptionUsage(baseUrl: string): Promise<SubscriptionUsage> {
  const response = await apiFetch(baseUrl, "/api/usage")
  if (response.status === 404) return { providers: [], poll_interval_seconds: 0 }
  if (!response.ok) {
    const body = await response.json().catch(() => ({}))
    throw new Error(body?.error || `Usage read failed (${response.status})`)
  }
  const usage = await response.json() as SubscriptionUsage
  if (usage.policy) usage.policy.profiles ??= []
  return usage
}

// Asks the server to read the usage now; it arrives as usage_changed.
export async function refreshSubscriptionUsage(baseUrl: string): Promise<void> {
  const response = await apiFetch(baseUrl, "/api/usage/refresh", { method: "POST" })
  if (!response.ok && response.status !== 202) {
    const body = await response.json().catch(() => ({}))
    throw new Error(body?.error || `refresh failed (${response.status})`)
  }
}

// Why a provider's usage could not be read, as a short phrase (beside a
// stale reading) or a sentence (in place of one). The provider's own words
// name the tool the reason is about.
export function reasonPhrase(reason?: string): string {
  switch (reason) {
    case "not_read": return "not read yet"
    case "not_signed_in": return "not signed in"
    case "named_keychain_unavailable": return "named Keychain usage unavailable; requires file-backed login"
    case "login_expired": return "login expired"
    case "no_subscription": return "no subscription limits"
    case "missing_key": return "no API key named"
    case "invalid_key": return "API key refused"
    case "codex_missing": return "codex not installed"
    case "no_data": return "nothing reported yet"
    case "endpoint_changed": return "its usage report changed"
    case "rate_limited": return "asked too often; waiting"
    case "unreachable": return "could not reach it"
    default: return "could not read it"
  }
}

export function reasonSentence(provider: string, reason?: string, accountLabel?: string): string {
  const tool = accountLabel || (provider === "claude" ? "Claude Code" : provider === "codex" ? "Codex" : provider === "opencode" ? "OpenCode Go" : provider)
  switch (reason) {
    case "not_read": return "Not read yet."
    case "not_signed_in": return `Sign in to ${tool} on this machine.`
    case "named_keychain_unavailable": return "Usage polling for named Keychain logins is unavailable on macOS. Only file-backed logins can be read."
    case "login_expired": return `${tool}'s login has expired; using ${tool} signs in again.`
    case "no_subscription": return "This account has no subscription limits to show."
    case "missing_key": return "Name its API key in Settings › Usage."
    case "invalid_key": return `${tool}'s API key was refused; replace the key's secret and restart.`
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

// The largest unit only, for a corner of the card: "2m", "3h", "now".
export function agoShort(iso: string, now: Date): string {
  const ms = now.getTime() - new Date(iso).getTime()
  return ms < 60000 ? "now" : spanText(ms).split(" ")[0]
}

function localDay(date: Date): number {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime()
}

// A moment in the browser's zone, as short as it can be read: "09:05"
// today, "Mon 09:00" within a week either way, "Dec 9" further off.
export function whenText(iso: string, now: Date): string {
  const date = new Date(iso)
  const days = Math.round((localDay(date) - localDay(now)) / 86_400_000)
  const clock = `${String(date.getHours()).padStart(2, "0")}:${String(date.getMinutes()).padStart(2, "0")}`
  if (days === 0) return clock
  if (Math.abs(days) <= 6) return `${date.toLocaleDateString(undefined, { weekday: "short" })} ${clock}`
  return date.toLocaleDateString(undefined, { month: "short", day: "numeric" })
}

// When a window resets, beside its bar: a countdown within a day, else the
// day and time.
export function resetShort(iso: string | undefined, now: Date): string {
  if (!iso) return ""
  const ms = new Date(iso).getTime() - now.getTime()
  if (ms <= 0) return `reset ${spanText(-ms)} ago`
  return ms < 86_400_000 ? `in ${spanText(ms)}` : whenText(iso, now)
}

export function sinceText(iso: string, now: Date): string {
  return `${whenText(iso, now)} · ${agoText(iso, now)}`
}

// Codex reports its credit balance with every digit it has.
export function formatCredits(balance: string, locale?: string): string {
  const value = Number(balance)
  if (balance.trim() === "" || !Number.isFinite(value)) return balance
  return new Intl.NumberFormat(locale, { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(value)
}

// The window nearest its limit, which the collapsed card shows as a number.
export function tightestWindow(windows: UsageWindow[]): UsageWindow | undefined {
  return windows.reduce<UsageWindow | undefined>((top, w) => (!top || w.used_percent > top.used_percent ? w : top), undefined)
}

// The providers the collapsed card has room for: all of them up to max,
// else the most constrained, kept in the server's order.
export function summaryProviders(providers: ProviderUsage[], max: number): { shown: ProviderUsage[]; hidden: ProviderUsage[] } {
  if (providers.length <= max) return { shown: providers, hidden: [] }
  const pressure = (p: ProviderUsage) => (p.state === "unavailable" ? -1 : tightestWindow(p.windows)?.used_percent ?? -1)
  const kept = new Set([...providers].sort((a, b) => pressure(b) - pressure(a)).slice(0, max))
  return { shown: providers.filter((p) => kept.has(p)), hidden: providers.filter((p) => !kept.has(p)) }
}

// The tone of a bar: calm, then warning from 75%, then over from 90%.
export function usageTone(percent: number): "calm" | "warn" | "over" {
  return percent >= 90 ? "over" : percent >= 75 ? "warn" : "calm"
}

// The worst tone of any window that was read freshly: what a button too
// small for the bars warns of. Stale and unavailable readings are left out,
// as the card leaves their bars grey.
export function worstTone(providers: ProviderUsage[]): "calm" | "warn" | "over" {
  const percents = providers
    .filter((p) => !p.stale && p.state !== "unavailable")
    .flatMap((p) => p.windows.map((w) => w.used_percent))
  return usageTone(Math.max(0, ...percents))
}
