import { useCallback, useEffect, useRef, useState } from "react"
import { KeySquare, Radar } from "lucide-react"
import { SettingsCard } from "./settings/SettingsUI"
import { BurnliteSettings, type ConnectOutcome } from "./BurnliteSettings"
import { fetchIntegrations, type ArgusOverview, type IntegrationToken, type IntegrationsOverview } from "../lib/integrations"
import { reasonText } from "../lib/nativeAgentView"

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
// callback puts in the address: ?integration=burnlite&result=connected (with
// mcp_created=<name> when it made agents' MCP connection), or
// result=failed&error=<why>.
function readOutcome(integration: string): ConnectOutcome | null {
  const params = new URLSearchParams(window.location.search)
  const result = params.get("result")
  if (params.get("integration") !== integration || (result !== "connected" && result !== "failed")) return null
  return { result, error: params.get("error") ?? undefined, mcpCreated: params.get("mcp_created") ?? undefined }
}

// Drops the callback's outcome from the address, so a reload does not show it again.
function forgetOutcome() {
  const params = new URLSearchParams(window.location.search)
  if (!params.has("integration")) return
  for (const name of ["integration", "result", "error", "mcp_created"]) params.delete(name)
  const search = params.toString()
  const { pathname, hash } = window.location
  window.history.replaceState(window.history.state, "", `${pathname}${search ? `?${search}` : ""}${hash}`)
}

// IntegrationsSettings shows services outside Legato that act on it: Argus
// (its settings, Legato's connection to it, the replies waiting for it and
// its cases in Attention), Burnlite (connected and set up here), and the
// scoped tokens integrations use. Tokens are made and revoked with
// `legato integration token`.
// only draws one of its cards, where Settings places it: Argus under
// Integrations, Burnlite under Board and tickets, the tokens under Security.
// argusSetHere says Argus's address can be set in Settings (config.yaml
// does not pin it), so the card points there instead of to the file.
export function IntegrationsSettings({ baseUrl, only, argusSetHere }: { baseUrl: string; only?: "argus" | "burnlite" | "tokens"; argusSetHere?: boolean }) {
  const shows = (part: "argus" | "burnlite" | "tokens") => !only || only === part
  const { overview, setOverview, error, load } = useIntegrations(baseUrl)
  const [burnliteOutcome] = useState(() => readOutcome("burnlite"))
  const root = useRef<HTMLDivElement>(null)
  useComeIntoView(overview, root)

  if (error) {
    return <p role="alert" className="rounded border border-red-900 bg-red-950/60 px-2 py-1.5 text-xs text-red-300">{error}</p>
  }
  if (!overview) return null

  return (
    <div ref={root} className="flex flex-col gap-4">
      {shows("argus") && <ArgusCard argus={overview.argus} setHere={argusSetHere} linkToken={only === "argus"} />}

      {shows("burnlite") && overview.burnlite && (
        <BurnliteSettings
          baseUrl={baseUrl}
          burnlite={overview.burnlite}
          outcome={burnliteOutcome}
          onChanged={(burnlite) => setOverview((o) => (o ? { ...o, burnlite } : o))}
          onDisconnected={() => { load() }}
        />
      )}

      {shows("tokens") && <TokensCard tokens={overview.tokens} />}
    </div>
  )
}

// useIntegrations reads the overview, and reads it again on load; an answer
// after the page moved on is dropped.
function useIntegrations(baseUrl: string) {
  const [overview, setOverview] = useState<IntegrationsOverview | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(() => {
    let live = true
    fetchIntegrations(baseUrl)
      .then((o) => { if (live) setOverview(o) })
      .catch((reason) => { if (live) setError(reasonText(reason)) })
    return () => { live = false }
  }, [baseUrl])

  useEffect(load, [load])
  useEffect(forgetOutcome, [])
  return { overview, setOverview, error, load }
}

// Attention's "connect again" item links here; come into view once shown.
function useComeIntoView(overview: IntegrationsOverview | null, root: React.RefObject<HTMLDivElement | null>) {
  useEffect(() => {
    if (overview !== null && (window.location.hash === "#integrations" || window.location.hash === "#burnlite")) {
      (document.getElementById(window.location.hash.slice(1)) ?? root.current)?.scrollIntoView?.({ block: "start" })
    }
  }, [overview, root])
}

function ArgusCard({ argus, setHere, linkToken }: { argus: ArgusOverview; setHere?: boolean; linkToken: boolean }) {
  return <SettingsCard id="argus" label="Argus" icon={Radar} title="Argus" description="The operations service: it looks up who owns work, hands briefs to agents, hears their replies, and shows its CI cases in Attention.">
    {!argus.configured ? (
      <p className="text-xs text-lw-text-2">
        {setHere
          ? <>Not set up. Give Argus's address below; it applies when Legato restarts.</>
          : <>Not set up. Set <code className={code}>integrations.argus.url</code> in config.yaml to Argus's address, and restart Legato.</>}
      </p>
    ) : (
      <dl className="divide-y divide-lw-line overflow-hidden rounded-lg border border-lw-line">
        <Row label="Address">
          <a href={argus.url} target="_blank" rel="noopener noreferrer" className="font-lw-mono text-lw-link hover:underline">{argus.url}</a>
        </Row>
        <Row label="Briefs reach agents">{argus.delivery === "ask" ? "after you approve each one" : "at once"}</Row>
        <Row label="Agent for new work"><span className="font-lw-mono">{argus.agent}</span></Row>
        <Row label="Replies to Argus"><ArgusReplies connection={argus.connection} /></Row>
        <Row label="Cases in Attention">{argus.open_cases} open</Row>
      </dl>
    )}
    {linkToken && <a href="#integration-tokens" className="mt-3 inline-block text-xs text-lw-link hover:underline">Argus's token is under Security › Integration tokens</a>}
  </SettingsCard>
}

// ArgusReplies is Legato's connection to Argus and the agents' replies still
// waiting for it or failed.
function ArgusReplies({ connection }: { connection: ArgusOverview["connection"] }) {
  const replies = connection.replies ?? {}
  return <>
    {connection.state === "connected" && <>Connected since {when(connection.connected_at)}</>}
    {connection.state === "disconnected" && (
      <span className="text-lw-danger">Argus refused Legato. Run <code className={code}>legato integration connect argus</code> to connect again.</span>
    )}
    {connection.state === "none" && (
      <>Not connected. Run <code className={code}>legato integration connect argus</code> so agents' replies reach Argus.</>
    )}
    <ReplyCounts pending={replies.pending ?? 0} failed={replies.failed ?? 0} />
  </>
}

function ReplyCounts({ pending, failed }: { pending: number; failed: number }) {
  if (pending <= 0 && failed <= 0) return null
  return (
    <span className="mt-1 block text-xs text-lw-text-2">
      {pending > 0 && `${pending} waiting to send`}
      {pending > 0 && failed > 0 && " · "}
      {failed > 0 && `${failed} failed`}
    </span>
  )
}

function TokensCard({ tokens }: { tokens: IntegrationToken[] }) {
  return <SettingsCard
    id="integration-tokens"
    label="Integration tokens"
    icon={KeySquare}
    title="Integration tokens"
    description={<>Tokens that reach only <code className={code}>/api/integrations/</code>, each with its scopes. Make one with <code className={code}>legato integration token create argus --scope owner:read,delegate,attention</code>, which shows it once; revoke one with <code className={code}>legato integration token revoke &lt;name&gt;</code>.</>}
  >
    {tokens.length === 0 ? (
      <p className="text-xs text-lw-faint">No integration tokens.</p>
    ) : (
      <ul className="divide-y divide-lw-line overflow-hidden rounded-lg border border-lw-line">
        {tokens.map((t) => <TokenRow key={t.id} token={t} />)}
      </ul>
    )}
  </SettingsCard>
}

// tokenUse says when a token was revoked, else last used.
function tokenUse(t: IntegrationToken): string {
  if (t.revoked_at) return ` · revoked ${when(t.revoked_at)}`
  return t.last_used_at ? ` · last used ${when(t.last_used_at)}` : " · not used yet"
}

function TokenRow({ token: t }: { token: IntegrationToken }) {
  return (
    <li className={`flex flex-col gap-1 px-3 py-2 ${t.revoked_at ? "opacity-60" : ""}`}>
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-lw-mono text-sm text-lw-text">{t.name}</span>
        {t.scopes.map((s) => (
          <span key={s} className="rounded bg-lw-raised px-1.5 py-0.5 font-lw-mono text-[10px] text-lw-text-2">{s}</span>
        ))}
        {t.revoked_at && <span className="rounded bg-lw-raised px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-lw-danger">revoked</span>}
      </div>
      <span className="text-xs text-lw-faint">
        Made {when(t.created_at)}
        {tokenUse(t)}
      </span>
    </li>
  )
}
