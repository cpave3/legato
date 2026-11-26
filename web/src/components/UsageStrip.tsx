import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useState } from "react"
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
  type UsageWindow,
} from "../lib/usage"
import { cn } from "../lib/utils"

const toneText = { calm: "text-lw-text-2", warn: "text-lw-needs", over: "text-lw-error" }
const toneBar = { calm: "bg-lw-accent", warn: "bg-lw-needs", over: "bg-lw-error" }

// UsageStrip is the Claude and Codex subscriptions' usage at the head of
// the agents list: a line per provider in brief, which opens to the Usage
// card with a bar per window, when it resets and how fresh the reading is.
// It shows nothing when the server reads no provider's usage.
export function UsageStrip({ className }: { className?: string }) {
  const { baseUrl } = useServer()
  const queryClient = useQueryClient()
  const { data } = useQuery({
    queryKey: queryKeys.subscriptionUsage(baseUrl),
    queryFn: () => fetchSubscriptionUsage(baseUrl),
    // Times like "as of 2m ago" move on their own.
    refetchInterval: 60_000,
  })
  const [open, setOpen] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState("")
  const providers = data?.providers ?? []
  if (providers.length === 0) return null
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
        aria-label={`Subscription usage: ${providers.map(briefLabel).join("; ")}`}
        onClick={() => setOpen((o) => !o)}
        className="flex w-full items-start gap-1.5 rounded-md px-2 py-1.5 text-left text-xs hover:bg-lw-raised"
      >
        {open ? <ChevronDown size={13} aria-hidden="true" className="mt-0.5 shrink-0 text-lw-faint" /> : <ChevronRight size={13} aria-hidden="true" className="mt-0.5 shrink-0 text-lw-faint" />}
        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
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
