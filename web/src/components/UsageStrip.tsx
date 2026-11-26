import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useEffect, useState } from "react"
import { Link, useSearchParams } from "react-router-dom"
import { ChevronDown, ChevronRight, RefreshCw } from "lucide-react"
import { useServer } from "../hooks/useServer"
import { queryKeys } from "../lib/query"
import {
  agoText,
  fetchSubscriptionUsage,
  meterLabel,
  providerName,
  reasonPhrase,
  reasonSentence,
  refreshSubscriptionUsage,
  resetText,
  usageTone,
  type ProviderUsage,
  type UsagePolicy,
  qualifiedUsageMeters,
  setUsagePin,
  type UsagePin,
  type UsageWindow,
} from "../lib/usage"
import { cn } from "../lib/utils"

const toneText = { calm: "text-lw-text-2", warn: "text-lw-needs", over: "text-lw-error" }
const toneBar = { calm: "bg-lw-accent", warn: "bg-lw-needs", over: "bg-lw-error" }

// UsageStrip is the Claude and Codex subscriptions' usage at the head of
// the agents list: a line per provider in brief, which opens to the Usage
// card with a bar per window, when it resets and how fresh the reading is.
// It also shows profile policy while the usage readers are off.
export function UsageStrip({ className }: { className?: string }) {
  const { baseUrl } = useServer()
  const queryClient = useQueryClient()
  const query = useQuery({
    queryKey: queryKeys.subscriptionUsage(baseUrl),
    queryFn: () => fetchSubscriptionUsage(baseUrl),
    // Times like "as of 2m ago" move on their own.
    refetchInterval: 60_000,
    retry: false,
  })
  const [params] = useSearchParams()
  const requestedPin = params.get("pin")
  const requestedOpen = params.get("usage") === "open" || requestedPin !== null
  const [open, setOpen] = useState(requestedOpen)
  useEffect(() => { if (requestedOpen) setOpen(true) }, [requestedOpen, requestedPin])
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState("")
  const data = query.data
  const providers = data?.providers ?? []
  if (query.isError) return <div className={cn("px-2 py-1.5 text-xs", className)}>
    <p role="alert" className="break-words text-lw-error">{query.error.message}</p>
    <button type="button" className="min-h-9 text-lw-link hover:underline" disabled={query.isFetching} onClick={() => void query.refetch()}>Retry usage</button>
  </div>
  if (providers.length === 0 && !data?.policy) return null
  const now = new Date()

  const refresh = async () => {
    setError("")
    setRefreshing(true)
    try {
      await refreshSubscriptionUsage(baseUrl)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setRefreshing(false)
      void queryClient.invalidateQueries({ queryKey: queryKeys.subscriptionUsage(baseUrl) })
    }
  }

  return (
    <div className={cn("flex flex-col", className)}>
      <button
        type="button"
        aria-expanded={open}
        aria-label={`Subscription usage: ${[data?.policy?.active.profile.name, ...providers.map(briefLabel)].filter(Boolean).join("; ")}`}
        onClick={() => setOpen((o) => !o)}
        className="flex w-full items-start gap-1.5 rounded-md px-2 py-1.5 text-left text-xs hover:bg-lw-raised"
      >
        {open ? <ChevronDown size={13} aria-hidden="true" className="mt-0.5 shrink-0 text-lw-faint" /> : <ChevronRight size={13} aria-hidden="true" className="mt-0.5 shrink-0 text-lw-faint" />}
        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
          {data?.policy && <span className="font-medium text-lw-text">{data.policy.active.profile.name || "Usual defaults"}</span>}
          {providers.map((p) => (
            <span key={p.provider} className="flex min-w-0 items-baseline gap-2">
              <span className="w-12 shrink-0 text-lw-text-2">{providerName(p.provider)}</span>
              {p.state === "unavailable" ? (
                <span className="text-lw-faint">unavailable</span>
              ) : (
                <>
                  {p.windows.map((w) => (
                    <span key={w.meter} className={cn("tabular-nums", p.stale ? "text-lw-faint" : toneText[usageTone(w.used_percent)])}>
                      {meterLabel(w.meter, "short")} {Math.round(w.used_percent)}%
                    </span>
                  ))}
                  {p.stale && <span className="text-lw-faint">stale</span>}
                </>
              )}
            </span>
          ))}
        </span>
      </button>

      {open && (
        <section aria-label="Subscription usage" className="mx-1 mb-1 flex flex-col gap-3 rounded-md border border-lw-line bg-lw-raised px-2.5 py-2.5 text-xs">
          {data?.policy && <PolicyCard policy={data.policy} requestedPin={requestedPin} baseUrl={baseUrl} />}
          {providers.length === 0 && <p className="text-lw-text-2">Usage readers are off.</p>}
          {providers.map((p) => <ProviderCard key={p.provider} usage={p} now={now} />)}
          {error && <p role="alert" className="m-0 break-words text-lw-error">{error}</p>}
          <button
            type="button"
            onClick={() => void refresh()}
            disabled={refreshing}
            className="flex items-center gap-1.5 self-start rounded px-1.5 py-0.5 text-lw-link hover:bg-lw-surface disabled:opacity-50"
          >
            <RefreshCw size={12} aria-hidden="true" className={refreshing ? "animate-spin" : undefined} />
            Read now
          </button>
        </section>
      )}
    </div>
  )
}

function briefLabel(p: ProviderUsage): string {
  const name = providerName(p.provider)
  if (p.state === "unavailable") return `${name} unavailable`
  const windows = p.windows.map((w) => `${meterLabel(w.meter)} ${Math.round(w.used_percent)}%`).join(", ")
  return p.stale ? `${name} ${windows} (stale)` : `${name} ${windows}`
}

function ProviderCard({ usage, now }: { usage: ProviderUsage; now: Date }) {
  const name = providerName(usage.provider)
  if (usage.state === "unavailable") {
    return (
      <div role="group" aria-label={name} className="flex flex-col gap-1">
        <span className="font-semibold text-lw-text">{name}</span>
        <span className="text-lw-text-2">{reasonSentence(usage.provider, usage.reason)}</span>
      </div>
    )
  }
  const fresh = usage.as_of ? `as of ${agoText(usage.as_of, now)}` : ""
  return (
    <div role="group" aria-label={name} className="flex flex-col gap-1.5">
      <div className="flex items-baseline justify-between gap-2">
        <span className="font-semibold text-lw-text">
          {name}
          {usage.plan && <span className="ml-1.5 font-normal text-lw-faint">{usage.plan}</span>}
        </span>
        <span className={cn("text-right text-[11px]", usage.stale ? "text-lw-needs" : "text-lw-faint")} title={usage.as_of ? new Date(usage.as_of).toLocaleString() : undefined}>
          {usage.stale ? ["stale", fresh, usage.reason ? reasonPhrase(usage.reason) : ""].filter(Boolean).join(" · ") : fresh}
        </span>
      </div>
      {usage.windows.map((w) => <WindowBar key={w.meter} provider={name} window={w} stale={usage.stale} now={now} />)}
      {usage.credits && (usage.credits.unlimited || usage.credits.balance) && (
        <span className="text-[11px] text-lw-faint">Credits: {usage.credits.unlimited ? "unlimited" : usage.credits.balance}</span>
      )}
      {usage.source === "logs" && <span className="text-[11px] text-lw-faint">from Codex's session logs, as of its last turn</span>}
    </div>
  )
}

function WindowBar({ provider, window: w, stale, now }: { provider: string; window: UsageWindow; stale: boolean; now: Date }) {
  const percent = Math.max(0, Math.min(100, Math.round(w.used_percent)))
  const tone = usageTone(w.used_percent)
  return (
    <div className="flex flex-col gap-0.5">
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-lw-text-2">{meterLabel(w.meter)}</span>
        <span className={cn("tabular-nums", stale ? "text-lw-faint" : toneText[tone])}>{percent}%</span>
      </div>
      <div
        role="progressbar"
        aria-label={`${provider} ${meterLabel(w.meter)}`}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={percent}
        className="h-1.5 overflow-hidden rounded-full bg-lw-line-strong"
      >
        <div className={cn("h-full rounded-full", stale ? "bg-lw-faint" : toneBar[tone])} style={{ width: `${percent}%` }} />
      </div>
      {w.resets_at && (
        <span className="text-[11px] text-lw-faint" title={new Date(w.resets_at).toLocaleString()}>{resetText(w.resets_at, now)}</span>
      )}
    </div>
  )
}

function PolicyCard({ policy, requestedPin, baseUrl }: { policy: UsagePolicy; requestedPin: string | null; baseUrl: string }) {
  const { active } = policy
  return <div className="flex min-w-0 flex-col gap-2 text-lw-text-2">
    <div><strong className="text-lw-text">{active.profile.name || "Usual defaults"}</strong><p>{[active.profile.agent, active.profile.tier].filter(Boolean).join(" · ")}</p></div>
    {active.profile.reviewer && <p>Reviewer: {[active.profile.reviewer.agent, active.profile.reviewer.tier].filter(Boolean).join(" · ")}</p>}
    <p>{active.reason.text}</p>
    {active.profile.name && <p>Since {new Date(active.since).toLocaleString()}</p>}
    {active.error && <p role="alert" className="text-lw-error">{active.error}</p>}
    {policy.profiles.length > 0
      ? <PinControls key={requestedPin} policy={policy} requestedPin={requestedPin} baseUrl={baseUrl} />
      : <Link to="/settings#usage" className="min-h-9 text-lw-link hover:underline">Configure profiles</Link>}
    {policy.switches.length > 0 && <details><summary className="cursor-pointer">Recent switches</summary><ol className="mt-2 flex max-h-48 flex-col gap-2 overflow-y-auto" aria-label="Recent switches">
      {policy.switches.slice(0, 5).map((change) => <li key={change.id}><p>{change.from_profile} → {change.to_profile}</p><p>{change.reason}</p><time dateTime={change.since}>{new Date(change.since).toLocaleString()}</time></li>)}
    </ol></details>}
  </div>
}

const pinField = "min-h-9 w-full min-w-0 rounded border border-lw-line-strong bg-lw-ground px-2 text-xs text-lw-text focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-lw-accent-ring"

function PinControls({ policy, requestedPin, baseUrl }: { policy: UsagePolicy; requestedPin: string | null; baseUrl: string }) {
  const queryClient = useQueryClient()
  const pin = policy.active.pin
  const [open, setOpen] = useState(requestedPin !== null)
  const [profile, setProfile] = useState(requestedPin && policy.profiles.some((p) => p.name === requestedPin) ? requestedPin : pin?.profile ?? policy.active.profile.name)
  const [expiry, setExpiry] = useState(pin?.until ? "time" : pin?.until_reset ? "reset" : "indefinite")
  const [until, setUntil] = useState(() => {
    if (!pin?.until) return ""
    const date = new Date(pin.until)
    return new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 16)
  })
  const [meter, setMeter] = useState(pin?.until_reset ?? qualifiedUsageMeters[0])
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const save = async (unpin = false) => {
    setSaving(true)
    setError("")
    setNotice("")
    try {
      const next: UsagePin = { profile }
      if (!unpin && expiry === "time") next.until = new Date(until).toISOString()
      if (!unpin && expiry === "reset") next.until_reset = meter
      await setUsagePin(baseUrl, unpin ? undefined : next)
      setNotice(unpin ? "Pin removed." : "Profile pinned.")
      await queryClient.invalidateQueries({ queryKey: queryKeys.subscriptionUsage(baseUrl) })
      await queryClient.invalidateQueries({ queryKey: queryKeys.adapters(baseUrl) })
    } catch (reason) { setError(reason instanceof Error ? reason.message : String(reason)) }
    finally { setSaving(false) }
  }
  return <div className="flex flex-col gap-2 border-t border-lw-line pt-2">
    {pin && <p>Pinned: {pin.profile}{pin.until ? ` until ${new Date(pin.until).toLocaleString()}` : pin.until_reset ? ` until ${pin.until_reset} resets` : " indefinitely"}</p>}
    <div className="flex flex-wrap gap-3"><button type="button" className="min-h-9 text-lw-link hover:underline" aria-expanded={open} onClick={() => setOpen(!open)}>{pin ? "Change pin" : "Pin a profile"}</button>
      {pin && <button type="button" className="min-h-9 text-lw-link hover:underline disabled:opacity-50" disabled={saving} onClick={() => void save(true)}>Unpin</button>}
    </div>
    {open && <form onSubmit={(e) => { e.preventDefault(); void save() }}><fieldset disabled={saving} className="flex min-w-0 flex-col gap-2">
      <label className="flex flex-col gap-1">Pin profile<select aria-label="Pin profile" className={pinField} value={profile} onChange={(e) => setProfile(e.target.value)}>{policy.profiles.map((p) => <option key={p.name} value={p.name}>{p.name}</option>)}</select></label>
      <label className="flex flex-col gap-1">Pin expires<select aria-label="Pin expires" className={pinField} value={expiry} onChange={(e) => setExpiry(e.target.value)}><option value="indefinite">When unpinned</option><option value="time">At a time</option><option value="reset">When a meter resets</option></select></label>
      {expiry === "time" && <label className="flex flex-col gap-1">Until (local time)<input type="datetime-local" className={pinField} required value={until} onChange={(e) => setUntil(e.target.value)} /></label>}
      {expiry === "reset" && <label className="flex flex-col gap-1">Until meter resets<select aria-label="Until meter resets" className={pinField} value={meter} onChange={(e) => setMeter(e.target.value)}>{qualifiedUsageMeters.map((m) => <option key={m} value={m}>{m}</option>)}</select></label>}
      <button type="submit" className="min-h-9 self-start rounded bg-lw-accent px-3 text-[var(--theme-canvas,#fff)] disabled:opacity-50" disabled={saving || !profile}>{saving ? "Saving…" : "Pin profile"}</button>
    </fieldset></form>}
    {error && <p role="alert" className="break-words text-lw-error">{error}</p>}
    {notice && <p role="status">{notice}</p>}
  </div>
}
