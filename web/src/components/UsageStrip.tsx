import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useEffect, useId, useLayoutEffect, useRef, useState, type ReactNode, type RefObject } from "react"
import { Link, useSearchParams } from "react-router-dom"
import { Ban, ChevronDown, ChevronRight, Clock, Gauge, OctagonAlert, Pin, RefreshCw, TriangleAlert, X } from "lucide-react"
import { useServer } from "../hooks/useServer"
import { useMenuLayer } from "../hooks/useMenuLayer"
import { queryKeys } from "../lib/query"
import {
  agoShort,
  agoText,
  catalogMeterLabel,
  catalogProviderName,
  catalogQualifiedMeters,
  fetchSubscriptionUsage,
  formatCredits,
  reasonPhrase,
  reasonSentence,
  refreshSubscriptionUsage,
  resetShort,
  setUsagePin,
  sinceText,
  summaryProviders,
  tightestWindow,
  usageTone,
  whenText,
  worstTone,
  type ProviderUsage,
  type SubscriptionUsage,
  type UsageCatalogProvider,
  type UsagePin,
  type UsagePolicy,
  type UsageWindow,
} from "../lib/usage"
import { cn } from "../lib/utils"
import { Dialog, DialogPanel, DialogTitle } from "./ui/Dialog"

type Tone = "calm" | "warn" | "over"
const toneText: Record<Tone, string> = { calm: "text-lw-text", warn: "text-lw-needs", over: "font-semibold text-lw-error" }
const toneFill: Record<Tone, string> = { calm: "bg-lw-accent", warn: "bg-lw-needs", over: "bg-lw-error" }
const toneWords: Record<Tone, string> = { calm: "", warn: ", near limit", over: ", at limit" }

// How many accounts the collapsed card has a line for.
const SUMMARY_LINES = 4

// UsageStrip is the subscriptions' usage at the head of the agents list: a
// card with a line per account (a thin bar per window, the tightest window
// as a number) and the active usage profile. It opens a popout beside the
// sidebar, or a sheet on a phone, with a bar per window, when each resets,
// the profile and its pin, and the recent switches. Compact, for a phone's
// picker bar, it is only a button (marked when an account nears its limit
// or the profile switch failed) that opens the sheet.
export function UsageStrip({ className, sheet = false, compact = false }: { className?: string; sheet?: boolean; compact?: boolean }) {
  const { baseUrl } = useServer()
  const query = useQuery({
    queryKey: queryKeys.subscriptionUsage(baseUrl),
    queryFn: () => fetchSubscriptionUsage(baseUrl),
    // Times like "2m ago" move on their own.
    refetchInterval: 60_000,
    retry: false,
  })
  const [params] = useSearchParams()
  const requestedPin = params.get("pin")
  const requestedOpen = params.get("usage") === "open" || requestedPin !== null
  const [open, setOpen] = useState(requestedOpen)
  useEffect(() => { if (requestedOpen) setOpen(true) }, [requestedOpen, requestedPin])
  const triggerRef = useRef<HTMLButtonElement>(null)
  const data = query.data
  const providers = data?.providers ?? []
  const asSheet = sheet || compact

  if (query.isError) {
    if (compact) return (
      <button
        type="button"
        aria-label={`Subscription usage could not be read: ${query.error.message}. Retry`}
        title="Usage could not be read. Retry"
        disabled={query.isFetching}
        onClick={() => void query.refetch()}
        className={cn(compactButton, "text-lw-error", className)}
      >
        <OctagonAlert size={18} aria-hidden="true" />
      </button>
    )
    return <div className={cn("px-2 py-1.5 text-xs", className)}>
      <p role="alert" className="break-words text-lw-error">{query.error.message}</p>
      <button type="button" className="min-h-9 text-lw-link hover:underline" disabled={query.isFetching} onClick={() => void query.refetch()}>Retry usage</button>
    </div>
  }
  if (!data || (providers.length === 0 && !data.policy)) return null
  const now = new Date()
  const close = () => setOpen(false)
  const toggle = () => setOpen((o) => !o)
  const panel = <UsagePanel data={data} now={now} requestedPin={requestedPin} onClose={close} sheet={asSheet} />
  const details = open && (asSheet
    ? <Dialog open onClose={close} className="items-end p-0">
        <DialogPanel className="max-h-[85dvh] max-w-none overflow-y-auto rounded-b-none rounded-t-2xl border-lw-line-strong bg-lw-raised pb-[env(safe-area-inset-bottom)] text-xs text-lw-text shadow-[var(--shadow-lw-panel)]">
          <div aria-hidden="true" className="mx-auto mt-2 h-1 w-9 shrink-0 rounded-full bg-lw-track-off" />
          {panel}
        </DialogPanel>
      </Dialog>
    : <UsagePopout triggerRef={triggerRef} onClose={close}>{panel}</UsagePopout>)

  if (compact) return <>
    <UsageButton data={data} open={open} triggerRef={triggerRef} onToggle={toggle} className={className} />
    {details}
  </>
  return (
    <div className={cn("flex flex-col", className)}>
      <UsageSummary data={data} now={now} open={open} triggerRef={triggerRef} onToggle={toggle} />
      {details}
    </div>
  )
}

// The size and look of the picker bar's own square buttons.
const compactButton = "relative flex size-11 shrink-0 items-center justify-center rounded-lg transition-colors hover:bg-lw-raised focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-lw-accent-ring disabled:opacity-50"

// UsageButton is the usage on a phone: a gauge, with a dot when an account
// nears or reaches its limit, or an error mark when the profile switch
// failed. Its name says all of it, so the mark never relies on colour.
function UsageButton({ data, open, triggerRef, onToggle, className }: { data: SubscriptionUsage; open: boolean; triggerRef: RefObject<HTMLButtonElement | null>; onToggle: () => void; className?: string }) {
  const failed = Boolean(data.policy?.active.error)
  const tone = worstTone(data.providers)
  return (
    <button
      ref={triggerRef}
      type="button"
      aria-haspopup="dialog"
      aria-expanded={open}
      aria-label={summaryLabel(data)}
      title="Usage"
      onClick={onToggle}
      className={cn(compactButton, open ? "bg-lw-raised text-lw-text" : "text-[var(--theme-text,#c9c8d0)] hover:text-lw-text", className)}
    >
      {failed
        ? <OctagonAlert data-tone="error" size={18} aria-hidden="true" className="text-lw-error" />
        : <Gauge size={18} aria-hidden="true" />}
      {!failed && tone !== "calm" && (
        <span data-tone={tone} aria-hidden="true" className={cn("absolute right-2 top-2 size-2 rounded-full ring-2 ring-lw-ground", toneFill[tone])} />
      )}
    </button>
  )
}

// The popout beside the sidebar: not modal, so the agents stay usable. It
// takes focus when it opens, closes on Escape or a click outside, and gives
// focus back to the card.
function UsagePopout({ triggerRef, onClose, children }: { triggerRef: RefObject<HTMLButtonElement | null>; onClose: () => void; children: ReactNode }) {
  const panelRef = useRef<HTMLDivElement>(null)
  useMenuLayer({ open: true, onClose, containerRef: panelRef, triggerRef, placement: "side", keyboard: false, dismissOnOutside: true })
  useLayoutEffect(() => { panelRef.current?.focus({ preventScroll: true }) }, [])
  return (
    <div
      ref={panelRef}
      role="dialog"
      aria-label="Subscription usage"
      tabIndex={-1}
      onKeyDown={(event) => {
        if (event.key !== "Escape") return
        event.preventDefault()
        event.stopPropagation()
        triggerRef.current?.focus({ preventScroll: true })
        onClose()
      }}
      className="fixed z-50 flex w-[22.5rem] flex-col overflow-y-auto rounded-xl border border-lw-line-strong bg-lw-raised text-xs text-lw-text shadow-[var(--shadow-lw-panel)] outline-none"
    >
      {children}
    </div>
  )
}

function newest(providers: ProviderUsage[]): string | undefined {
  return providers.map((p) => p.as_of).filter((t): t is string => !!t).sort().at(-1)
}

function oldestStale(providers: ProviderUsage[]): string | undefined {
  return providers.filter((p) => p.stale && p.as_of).map((p) => p.as_of!).sort()[0]
}

function profileName(policy: UsagePolicy): string {
  return policy.active.profile.name || "Usual defaults"
}

function summaryLabel(data: SubscriptionUsage): string {
  const { policy, providers, catalog } = data
  const parts = providers.map((p) => {
    const name = catalogProviderName(catalog, p.provider)
    if (p.state === "unavailable") return `${name} unavailable`
    return p.windows.map((w) => {
      const percent = Math.round(w.used_percent)
      return `${name} ${catalogMeterLabel(catalog, w.meter)} ${percent}%${toneWords[usageTone(w.used_percent)]}${p.stale ? ", stale" : ""}`
    }).join("; ")
  })
  const profile = policy ? `Profile ${profileName(policy)}, ${policy.active.error ? "switch failed" : policy.active.pin ? "pinned" : "automatic"}. ` : ""
  return `Subscription usage. ${profile}${parts.join("; ")}`
}

function ModeChip({ policy }: { policy: UsagePolicy }) {
  const chip = "inline-flex h-[18px] shrink-0 items-center gap-1 whitespace-nowrap rounded-full px-1.5 text-[11px] font-medium"
  if (policy.active.error) return <span className={cn(chip, "bg-lw-error-soft text-lw-danger")}>Switch failed</span>
  if (policy.active.pin) return <span className={cn(chip, "bg-lw-accent-soft text-lw-accent-text")}><Pin size={10} aria-hidden="true" />Pinned</span>
  return <span className={cn(chip, "bg-lw-idle-soft text-lw-text-2")}>Auto</span>
}

function ToneIcon({ tone }: { tone: Tone }) {
  if (tone === "over") return <OctagonAlert size={11} strokeWidth={2.4} aria-hidden="true" className="shrink-0" />
  if (tone === "warn") return <TriangleAlert size={11} strokeWidth={2.4} aria-hidden="true" className="shrink-0" />
  return null
}

function UsageSummary({ data, now, open, triggerRef, onToggle }: { data: SubscriptionUsage; now: Date; open: boolean; triggerRef: RefObject<HTMLButtonElement | null>; onToggle: () => void }) {
  const { policy, providers, catalog } = data
  const { shown, hidden } = summaryProviders(providers, SUMMARY_LINES)
  const stale = oldestStale(providers)
  const fresh = newest(providers)
  return (
    <div className={cn(
      "rounded-lg border bg-lw-surface shadow-[var(--shadow-lw-control)]",
      open ? "border-lw-accent-ring bg-lw-raised" : "border-lw-line",
      policy?.active.error && !open && "border-lw-danger-line",
    )}>
      <button
        ref={triggerRef}
        type="button"
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={summaryLabel(data)}
        onClick={onToggle}
        className="flex w-full flex-col gap-[7px] rounded-lg px-2.5 pb-2.5 pt-[9px] text-left text-xs hover:bg-lw-raised focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-lw-accent-ring"
      >
        <span className="flex w-full min-w-0 items-center gap-1.5">
          {policy?.active.error
            ? <OctagonAlert size={13} aria-hidden="true" className="shrink-0 text-lw-error" />
            : <Gauge size={13} aria-hidden="true" className="shrink-0 text-lw-text-2" />}
          <span className="min-w-0 truncate font-semibold text-lw-text">{policy ? profileName(policy) : "Usage"}</span>
          {policy && <ModeChip policy={policy} />}
          <span className="ml-auto flex shrink-0 items-center gap-1 text-[11px] tabular-nums">
            {stale
              ? <span className="flex items-center gap-1 text-lw-needs"><Clock size={11} strokeWidth={2.4} aria-hidden="true" />stale {agoShort(stale, now)}</span>
              : fresh && <span className="text-lw-text-2">{agoShort(fresh, now)}</span>}
          </span>
          <ChevronRight size={13} aria-hidden="true" className="shrink-0 text-lw-text-2" />
        </span>
        {providers.length > 0 && (
          <span className="flex w-full flex-col gap-[5px]">
            {shown.map((p) => <SummaryLine key={p.provider} usage={p} catalog={catalog} />)}
            {hidden.length > 0 && (
              <span className="truncate text-[11px] text-lw-text-2">+{hidden.length} more: {hidden.map((p) => catalogProviderName(catalog, p.provider)).join(", ")}</span>
            )}
          </span>
        )}
        {providers.length === 0 && <span className="text-lw-text-2">Usage readers are off.</span>}
      </button>
    </div>
  )
}

function SummaryLine({ usage, catalog }: { usage: ProviderUsage; catalog: UsageCatalogProvider[] | undefined }) {
  const name = catalogProviderName(catalog, usage.provider)
  const top = tightestWindow(usage.windows)
  const unavailable = usage.state === "unavailable" || !top
  const tone: Tone = top ? usageTone(top.used_percent) : "calm"
  return (
    <span data-provider={usage.provider} className="grid min-h-4 w-full grid-cols-[minmax(0,1fr)_3.25rem_3.9rem] items-center gap-2">
      <span className="truncate text-lw-text" title={name}>{name}</span>
      {unavailable ? (
        <span className="col-span-2 flex min-w-0 items-center gap-1.5 text-lw-text-2" title={reasonPhrase(usage.reason)}>
          <span aria-hidden="true" className="min-w-2 flex-1 border-t border-dashed border-lw-line-strong" />
          <Ban size={11} aria-hidden="true" className="shrink-0" />unavailable
        </span>
      ) : (
        <>
          <span aria-hidden="true" className="flex flex-col gap-0.5">
            {usage.windows.map((w) => (
              <span key={w.meter} data-meter={w.meter} className="block h-[3px] overflow-hidden rounded-full bg-lw-track-off">
                <span className={cn("block h-full rounded-full", usage.stale ? "bg-lw-line-strong" : toneFill[usageTone(w.used_percent)])} style={{ width: `${clampPercent(w.used_percent)}%` }} />
              </span>
            ))}
          </span>
          <span className={cn("flex items-center justify-end gap-0.5 whitespace-nowrap font-lw-mono tabular-nums", usage.stale ? "text-lw-text-2" : toneText[tone])}>
            {!usage.stale && <ToneIcon tone={tone} />}
            <span>{Math.round(top.used_percent)}%</span>
            <span className="w-4 text-left text-[11px] font-normal text-lw-text-2">{catalogMeterLabel(catalog, top.meter, "short")}</span>
          </span>
        </>
      )}
    </span>
  )
}

function clampPercent(percent: number): number {
  return Math.max(0, Math.min(100, Math.round(percent)))
}

function UsagePanel({ data, now, requestedPin, onClose, sheet }: { data: SubscriptionUsage; now: Date; requestedPin: string | null; onClose: () => void; sheet: boolean }) {
  const { baseUrl } = useServer()
  const queryClient = useQueryClient()
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState("")
  const { providers, catalog, policy } = data
  const fresh = newest(providers)
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
  const iconButton = cn("flex shrink-0 items-center justify-center rounded-md text-lw-text-2 hover:bg-lw-selected hover:text-lw-text focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-lw-accent-ring disabled:opacity-50", sheet ? "size-11" : "size-7")
  const title = "Subscription usage"
  return (
    <>
      <div className="sticky top-0 z-[1] flex items-start justify-between gap-2 border-b border-lw-line bg-lw-raised py-2.5 pl-3.5 pr-2.5">
        <div className="min-w-0">
          {sheet ? <DialogTitle className="text-sm font-semibold leading-5 text-lw-text">{title}</DialogTitle> : <h2 className="text-sm font-semibold leading-5">{title}</h2>}
          {fresh && <p className="text-[11px] text-lw-text-2">Read {agoText(fresh, now)}</p>}
        </div>
        <div className="flex items-center">
          <button type="button" aria-label="Read now" title="Read now" disabled={refreshing} onClick={() => void refresh()} className={iconButton}>
            <RefreshCw size={14} aria-hidden="true" className={refreshing ? "animate-spin" : undefined} />
          </button>
          <button type="button" aria-label="Close" title="Close (Esc)" onClick={onClose} className={iconButton}>
            <X size={14} aria-hidden="true" />
          </button>
        </div>
      </div>
      {error && <p role="alert" className="m-0 break-words border-b border-lw-line px-3.5 py-2 text-lw-error">{error}</p>}

      {policy && <PolicyCard policy={policy} requestedPin={requestedPin} catalog={catalog} providers={providers} now={now} sheet={sheet} />}

      <section aria-label="Accounts" className={cn("flex flex-col gap-2.5 px-3.5 py-3", policy && "border-t border-lw-line")}>
        <span className="text-[11px] font-medium text-lw-text-2">Accounts</span>
        {providers.length === 0 && <p className="text-lw-text-2">Usage readers are off.</p>}
        {providers.map((p, i) => <ProviderCard key={p.provider} usage={p} catalog={catalog} now={now} first={i === 0} />)}
      </section>

      <div className="flex flex-wrap items-center justify-between gap-2 border-t border-lw-line px-3.5 py-2.5 text-[11px]">
        <Link to="/settings#usage" className="text-lw-link hover:underline">Usage settings</Link>
        <span className="text-lw-text-2">▲ near limit at 75% · ⬣ at limit at 90%</span>
      </div>
    </>
  )
}

function capitalize(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1)
}

function ProviderCard({ usage, catalog, now, first }: { usage: ProviderUsage; catalog: UsageCatalogProvider[] | undefined; now: Date; first: boolean }) {
  const name = catalogProviderName(catalog, usage.provider)
  const divider = !first && "border-t border-dashed border-lw-line pt-2.5"
  if (usage.state === "unavailable") {
    return (
      <div role="group" aria-label={name} className={cn("flex flex-col gap-1", divider)}>
        <span className="flex items-center gap-1.5 font-semibold text-lw-text"><Ban size={12} aria-hidden="true" className="text-lw-text-2" />{name}</span>
        <span className="text-lw-text-2">{reasonSentence(usage.provider, usage.reason, usage.account ? name : undefined)}</span>
      </div>
    )
  }
  const ago = usage.as_of ? agoText(usage.as_of, now) : ""
  return (
    <div role="group" aria-label={name} className={cn("flex flex-col gap-1.5", divider)}>
      <div className="flex min-w-0 items-baseline justify-between gap-2">
        <span className="min-w-0 truncate font-semibold text-lw-text" title={name}>
          {name}
          {usage.plan && <span className="ml-1.5 text-[11px] font-normal text-lw-text-2">{usage.plan}</span>}
        </span>
        <span className={cn("flex shrink-0 items-center gap-1 text-right text-[11px]", usage.stale ? "text-lw-needs" : "text-lw-text-2")} title={usage.as_of ? new Date(usage.as_of).toLocaleString() : undefined}>
          {usage.stale && <Clock size={11} strokeWidth={2.4} aria-hidden="true" />}
          {usage.stale ? ["stale", ago, usage.reason ? reasonPhrase(usage.reason) : ""].filter(Boolean).join(" · ") : ago}
        </span>
      </div>
      {usage.windows.map((w) => <WindowRow key={w.meter} provider={name} window={w} catalog={catalog} stale={usage.stale} now={now} />)}
      {usage.credits && (usage.credits.unlimited || usage.credits.balance) && (
        <span className="flex justify-between text-[11px] text-lw-text-2">Credits<span className="font-lw-mono font-medium tabular-nums text-lw-text">{usage.credits.unlimited ? "unlimited" : formatCredits(usage.credits.balance!)}</span></span>
      )}
      {usage.source === "logs" && <span className="text-[11px] text-lw-text-2">from Codex's session logs, as of its last turn</span>}
    </div>
  )
}

function WindowRow({ provider, window: w, catalog, stale, now }: { provider: string; window: UsageWindow; catalog: UsageCatalogProvider[] | undefined; stale: boolean; now: Date }) {
  const percent = clampPercent(w.used_percent)
  const tone = usageTone(w.used_percent)
  const label = catalogMeterLabel(catalog, w.meter)
  return (
    <div className="grid min-h-[18px] grid-cols-[3.5rem_minmax(0,1fr)_2.75rem_4.5rem] items-center gap-2">
      <span className="truncate text-lw-text-2" title={capitalize(label)}>{capitalize(label)}</span>
      <div
        role="progressbar"
        aria-label={`${provider} ${label}`}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={percent}
        aria-valuetext={`${percent}%${toneWords[tone]}${w.resets_at ? `, resets ${new Date(w.resets_at).toLocaleString()}` : ""}`}
        className="h-1.5 overflow-hidden rounded-full bg-lw-track-off"
      >
        <div className={cn("h-full rounded-full", stale ? "bg-lw-line-strong" : toneFill[tone])} style={{ width: `${percent}%` }} />
      </div>
      <span className={cn("flex items-center justify-end gap-0.5 font-lw-mono tabular-nums", stale ? "text-lw-text-2" : toneText[tone])}>
        {!stale && <ToneIcon tone={tone} />}{percent}%
      </span>
      <span className="truncate text-right font-lw-mono text-[11px] tabular-nums text-lw-text-2" title={w.resets_at ? new Date(w.resets_at).toLocaleString() : undefined}>
        {resetShort(w.resets_at, now)}
      </span>
    </div>
  )
}

function PolicyCard({ policy, requestedPin, catalog, providers, now, sheet }: { policy: UsagePolicy; requestedPin: string | null; catalog: UsageCatalogProvider[] | undefined; providers: ProviderUsage[]; now: Date; sheet: boolean }) {
  const { active } = policy
  const [switchesOpen, setSwitchesOpen] = useState(false)
  const switchesId = useId()
  const switches = policy.switches.slice(0, 5)
  return (
    <section aria-label="Active profile" className="flex min-w-0 flex-col gap-2.5 px-3.5 py-3">
      <div className="flex items-center justify-between gap-2">
        <span className="text-[11px] font-medium text-lw-text-2">Active profile</span>
        <ModeChip policy={policy} />
      </div>
      <p className="m-0 text-sm font-semibold leading-5">{profileName(policy)}</p>
      <dl className="m-0 grid grid-cols-[4rem_minmax(0,1fr)] gap-x-2.5 gap-y-1">
        {active.profile.agent && <><dt className="text-lw-text-2">Agent</dt><dd className="m-0">{[active.profile.agent, active.profile.tier].filter(Boolean).join(" · ")}</dd></>}
        {active.profile.reviewer && <><dt className="text-lw-text-2">Reviewer</dt><dd className="m-0">{[active.profile.reviewer.agent, active.profile.reviewer.tier].filter(Boolean).join(" · ")}</dd></>}
        <dt className="text-lw-text-2">Why</dt><dd className="m-0 break-words">{active.reason.text}</dd>
        {active.profile.name && <><dt className="text-lw-text-2">Since</dt><dd className="m-0" title={new Date(active.since).toLocaleString()}>{sinceText(active.since, now)}</dd></>}
      </dl>
      {active.error && <p role="alert" className="m-0 break-words text-lw-error">{active.error}</p>}
      {policy.profiles.length > 0
        ? <PinControls key={requestedPin} policy={policy} requestedPin={requestedPin} catalog={catalog} providers={providers} now={now} sheet={sheet} />
        : <Link to="/settings#usage" className="self-start text-lw-link hover:underline">Configure profiles</Link>}
      {switches.length > 0 && (
        <>
          <button
            type="button"
            aria-expanded={switchesOpen}
            aria-controls={switchesId}
            onClick={() => setSwitchesOpen((o) => !o)}
            className={cn("-ml-1.5 flex items-center gap-1 self-start rounded-md px-1.5 text-lw-link hover:bg-lw-selected focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-lw-accent-ring", sheet ? "min-h-11" : "min-h-7")}
          >
            <span>Recent switches</span><span className="text-lw-text-2">· {policy.switches.length}</span>
            <ChevronDown size={12} aria-hidden="true" className={cn("transition-transform motion-reduce:transition-none", switchesOpen && "rotate-180")} />
          </button>
          {switchesOpen && (
            <ol id={switchesId} aria-label="Recent switches" className="m-0 flex max-h-48 list-none flex-col gap-2 overflow-y-auto p-0">
              {switches.map((change) => (
                <li key={change.id} className="grid grid-cols-[4rem_minmax(0,1fr)] gap-2.5">
                  <time dateTime={change.since} title={new Date(change.since).toLocaleString()} className="font-lw-mono text-[11px] tabular-nums text-lw-text-2">{whenText(change.since, now)}</time>
                  <span className="min-w-0 break-words">{change.from_profile} → <strong className="font-semibold">{change.to_profile}</strong><span className="block text-lw-text-2">{change.reason}</span></span>
                </li>
              ))}
            </ol>
          )}
        </>
      )}
    </section>
  )
}

// A meter a pin can last until, named as people read it, with when it
// resets when a reading says so.
function meterOption(qualified: string, catalog: UsageCatalogProvider[] | undefined, providers: ProviderUsage[], now: Date): string {
  for (const p of catalog ?? []) {
    const m = p.meters.find((candidate) => candidate.qualified === qualified)
    if (!m) continue
    const reset = providers.find((u) => u.provider === p.provider)?.windows.find((w) => w.meter === m.window)?.resets_at
    return `${p.name} · ${m.long}${reset ? ` (${resetShort(reset, now)})` : ""}`
  }
  return qualified
}

function pinUntilText(pin: UsagePin, catalog: UsageCatalogProvider[] | undefined, now: Date): string {
  if (pin.until) return `until ${whenText(pin.until, now)}`
  if (pin.until_reset) {
    const label = meterOption(pin.until_reset, catalog, [], now)
    return `until ${label} resets`
  }
  return "until you unpin it"
}

const pinField = "w-full min-w-0 rounded-md border border-lw-line-strong bg-lw-ground px-2 text-xs text-lw-text focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-lw-accent-ring"
const expiries = [
  { value: "indefinite", label: "I unpin" },
  { value: "time", label: "A time" },
  { value: "reset", label: "A reset" },
] as const

function PinControls({ policy, requestedPin, catalog, providers, now, sheet }: { policy: UsagePolicy; requestedPin: string | null; catalog: UsageCatalogProvider[] | undefined; providers: ProviderUsage[]; now: Date; sheet: boolean }) {
  const { baseUrl } = useServer()
  const queryClient = useQueryClient()
  const pin = policy.active.pin
  const expiryName = useId()
  const [open, setOpen] = useState(requestedPin !== null)
  const [profile, setProfile] = useState(requestedPin && policy.profiles.some((p) => p.name === requestedPin) ? requestedPin : pin?.profile ?? policy.active.profile.name)
  const [expiry, setExpiry] = useState(pin?.until ? "time" : pin?.until_reset ? "reset" : "indefinite")
  const [until, setUntil] = useState(() => {
    if (!pin?.until) return ""
    const date = new Date(pin.until)
    return new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 16)
  })
  const meters = catalogQualifiedMeters(catalog)
  const [meter, setMeter] = useState(pin?.until_reset ?? meters[0] ?? "")
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
  const control = sheet ? "min-h-11" : "min-h-[30px]"
  const button = cn("inline-flex items-center gap-1.5 rounded-md border px-2.5 font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-lw-accent-ring disabled:opacity-50", control)
  const secondary = cn(button, "border-lw-line-strong bg-lw-selected text-lw-text shadow-[var(--shadow-lw-control)] hover:bg-lw-raised")
  return <div className="flex flex-col gap-2.5">
    {pin && <p className="m-0 flex items-start gap-1.5 text-lw-text-2"><Pin size={12} aria-hidden="true" className="mt-0.5 shrink-0 text-lw-accent-text" /><span>Pinned to <strong className="font-semibold text-lw-text">{pin.profile}</strong> {pinUntilText(pin, catalog, now)}.</span></p>}
    <div className="flex flex-wrap gap-1.5">
      <button type="button" className={secondary} aria-expanded={open} onClick={() => setOpen(!open)}>
        <Pin size={13} aria-hidden="true" />{pin ? "Change pin" : "Pin a profile"}
      </button>
      {pin && <button type="button" className={secondary} disabled={saving} onClick={() => void save(true)}>Unpin</button>}
    </div>
    {open && <form onSubmit={(e) => { e.preventDefault(); void save() }} className="rounded-lg border border-lw-line bg-lw-surface p-3">
      <fieldset disabled={saving} className="m-0 flex min-w-0 flex-col gap-2.5 border-0 p-0">
        <label className="flex flex-col gap-1 text-[11px] font-medium text-lw-text-2">Pin profile
          <select aria-label="Pin profile" className={cn(pinField, sheet ? "h-11" : "h-8")} value={profile} onChange={(e) => setProfile(e.target.value)}>
            {policy.profiles.map((p) => <option key={p.name} value={p.name}>{p.name}</option>)}
          </select>
        </label>
        <fieldset className="m-0 flex flex-col gap-1 border-0 p-0">
          <legend className="mb-1 p-0 text-[11px] font-medium text-lw-text-2">Keep it until</legend>
          <div className="grid grid-cols-3 gap-0.5 rounded-[7px] border border-lw-line-strong bg-lw-ground p-0.5">
            {expiries.map((option) => (
              <label key={option.value} className={cn(
                "flex cursor-pointer items-center justify-center rounded-[5px] text-xs text-lw-text-2 has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-lw-accent-ring",
                sheet ? "min-h-10" : "min-h-7",
                expiry === option.value && "bg-lw-selected text-lw-text shadow-[inset_0_0_0_1px_var(--color-lw-accent)]",
              )}>
                <input type="radio" name={expiryName} value={option.value} checked={expiry === option.value} onChange={() => setExpiry(option.value)} className="sr-only" />
                {option.label}
              </label>
            ))}
          </div>
        </fieldset>
        {expiry === "time" && <label className="flex flex-col gap-1 text-[11px] font-medium text-lw-text-2">Until (local time)<input type="datetime-local" className={cn(pinField, sheet ? "h-11" : "h-8")} required value={until} onChange={(e) => setUntil(e.target.value)} /></label>}
        {expiry === "reset" && <label className="flex flex-col gap-1 text-[11px] font-medium text-lw-text-2">Until this limit resets
          <select aria-label="Until this limit resets" className={cn(pinField, sheet ? "h-11" : "h-8")} value={meter} onChange={(e) => setMeter(e.target.value)}>
            {meters.map((m) => <option key={m} value={m}>{meterOption(m, catalog, providers, now)}</option>)}
          </select>
        </label>}
        <div className="flex flex-wrap gap-1.5">
          <button type="submit" className={cn(button, "border-lw-accent bg-lw-accent text-[var(--theme-canvas,#fff)]")} disabled={saving || !profile}>{saving ? "Saving…" : "Pin profile"}</button>
          <button type="button" className={cn(button, "border-transparent text-lw-text-2 hover:text-lw-text")} onClick={() => setOpen(false)}>Cancel</button>
        </div>
      </fieldset>
    </form>}
    {error && <p role="alert" className="m-0 break-words text-lw-error">{error}</p>}
    {notice && <p role="status" className="m-0 text-lw-text-2">{notice}</p>}
  </div>
}
