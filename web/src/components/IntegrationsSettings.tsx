import { useCallback, useEffect, useRef, useState } from "react"
import { KeySquare, Radar } from "lucide-react"
import { SettingsCard } from "./settings/SettingsUI"
import { BurnliteSettings, type ConnectOutcome } from "./BurnliteSettings"
import { fetchIntegrations, type IntegrationsOverview } from "../lib/integrations"

const code = "rounded bg-lw-raised px-1 font-lw-mono text-lw-text-2"

function when(iso?: string): string {
  return iso ? new Date(iso).toLocaleString() : ""
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5 px-3 py-2 sm:flex-row sm:items-baseline sm:gap-4">
      <dt className="w-40 shrink-0 text-xs text-lw-faint">{label}</dt>
      <dd className="min-w-0 break-words text-sm text-lw-text">{children}</dd>
    </div>
  )
}

// The outcome of connecting to an integration, which the server's OAuth
// callback puts in the address: ?integration=burnlite&result=connected, or
// result=failed&error=<why>.
function readOutcome(integration: string): ConnectOutcome | null {
  const params = new URLSearchParams(window.location.search)
  const result = params.get("result")
  if (params.get("integration") !== integration || (result !== "connected" && result !== "failed")) return null
  return { result, error: params.get("error") ?? undefined }
}

// Drops the callback's outcome from the address, so a reload does not show it again.
function forgetOutcome() {
  const params = new URLSearchParams(window.location.search)
  if (!params.has("integration")) return
  for (const name of ["integration", "result", "error"]) params.delete(name)
  const search = params.toString()
  const { pathname, hash } = window.location
  window.history.replaceState(window.history.state, "", `${pathname}${search ? `?${search}` : ""}${hash}`)
}

// IntegrationsSettings shows services outside Legato that act on it: Argus
// (its settings, Legato's connection to it, the replies waiting for it and
// its cases in Attention), Burnlite (connected and set up here), and the
// scoped tokens integrations use. Tokens are made and revoked with
// `legato integration token`.
export function IntegrationsSettings({ baseUrl }: { baseUrl: string }) {
  const [overview, setOverview] = useState<IntegrationsOverview | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [burnliteOutcome] = useState(() => readOutcome("burnlite"))
  const root = useRef<HTMLDivElement>(null)

  const load = useCallback(() => {
    let live = true
    fetchIntegrations(baseUrl)
      .then((o) => { if (live) setOverview(o) })
      .catch((reason) => { if (live) setError(reason instanceof Error ? reason.message : String(reason)) })
    return () => { live = false }
  }, [baseUrl])

  useEffect(load, [load])
  useEffect(forgetOutcome, [])

  // Attention's "connect again" item links here; come into view once shown.
  useEffect(() => {
    if (overview !== null && window.location.hash === "#integrations") {
      (document.getElementById("integrations") ?? root.current)?.scrollIntoView?.({ block: "start" })
    }
  }, [overview])

  if (error) {
    return <p role="alert" className="rounded border border-red-900 bg-red-950/60 px-2 py-1.5 text-xs text-red-300">{error}</p>
  }
  if (!overview) return null

  const { argus, tokens } = overview
  const replies = argus.connection.replies ?? {}
  return (
    <div ref={root} className="flex flex-col gap-4">
      <SettingsCard label="Argus" icon={Radar} title="Argus" description="The operations service: it looks up who owns work, hands briefs to agents, hears their replies, and shows its CI cases in Attention.">
        {!argus.configured ? (
          <p className="text-xs text-lw-text-2">
            Not set up. Set <code className={code}>integrations.argus.url</code> in config.yaml to Argus's address, and restart Legato.
          </p>
        ) : (
          <dl className="divide-y divide-lw-line overflow-hidden rounded-lg border border-lw-line">
            <Row label="Address">
              <a href={argus.url} target="_blank" rel="noopener noreferrer" className="font-lw-mono text-lw-link hover:underline">{argus.url}</a>
            </Row>
            <Row label="Briefs reach agents">{argus.delivery === "ask" ? "after you approve each one" : "at once"}</Row>
            <Row label="Agent for new work"><span className="font-lw-mono">{argus.agent}</span></Row>
            <Row label="Replies to Argus">
              {argus.connection.state === "connected" && <>Connected since {when(argus.connection.connected_at)}</>}
              {argus.connection.state === "disconnected" && (
                <span className="text-lw-danger">Argus refused Legato. Run <code className={code}>legato integration connect argus</code> to connect again.</span>
              )}
              {argus.connection.state === "none" && (
                <>Not connected. Run <code className={code}>legato integration connect argus</code> so agents' replies reach Argus.</>
              )}
              {((replies.pending ?? 0) > 0 || (replies.failed ?? 0) > 0) && (
                <span className="mt-1 block text-xs text-lw-text-2">
                  {(replies.pending ?? 0) > 0 && `${replies.pending} waiting to send`}
                  {(replies.pending ?? 0) > 0 && (replies.failed ?? 0) > 0 && " · "}
                  {(replies.failed ?? 0) > 0 && `${replies.failed} failed`}
                </span>
              )}
            </Row>
            <Row label="Cases in Attention">{argus.open_cases} open</Row>
          </dl>
        )}
      </SettingsCard>

      {overview.burnlite && (
        <BurnliteSettings
          baseUrl={baseUrl}
          burnlite={overview.burnlite}
          outcome={burnliteOutcome}
          onChanged={(burnlite) => setOverview((o) => (o ? { ...o, burnlite } : o))}
          onDisconnected={() => { load() }}
        />
      )}

      <SettingsCard
        label="Integration tokens"
        icon={KeySquare}
        title="Integration tokens"
        description={<>Tokens that reach only <code className={code}>/api/integrations/</code>, each with its scopes. Make one with <code className={code}>legato integration token create argus --scope owner:read,delegate,attention</code>, which shows it once; revoke one with <code className={code}>legato integration token revoke &lt;name&gt;</code>.</>}
      >
        {tokens.length === 0 ? (
          <p className="text-xs text-lw-faint">No integration tokens.</p>
        ) : (
          <ul className="divide-y divide-lw-line overflow-hidden rounded-lg border border-lw-line">
            {tokens.map((t) => (
              <li key={t.id} className={`flex flex-col gap-1 px-3 py-2 ${t.revoked_at ? "opacity-60" : ""}`}>
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-lw-mono text-sm text-lw-text">{t.name}</span>
                  {t.scopes.map((s) => (
                    <span key={s} className="rounded bg-lw-raised px-1.5 py-0.5 font-lw-mono text-[10px] text-lw-text-2">{s}</span>
                  ))}
                  {t.revoked_at && <span className="rounded bg-lw-raised px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-lw-danger">revoked</span>}
                </div>
                <span className="text-xs text-lw-faint">
                  Made {when(t.created_at)}
                  {t.revoked_at ? ` · revoked ${when(t.revoked_at)}` : t.last_used_at ? ` · last used ${when(t.last_used_at)}` : " · not used yet"}
                </span>
              </li>
            ))}
          </ul>
        )}
      </SettingsCard>
    </div>
  )
}
