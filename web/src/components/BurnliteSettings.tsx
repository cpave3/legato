import { useState, type FormEvent, type ReactNode } from "react"
import { Flame } from "lucide-react"
import { SettingsCard, primaryButton, quietButton } from "./settings/SettingsUI"
import { InlineNotice } from "./ui/InlineNotice"
import {
  connectBurnlite,
  disconnectBurnlite,
  leaveFor,
  parseProjectKeys,
  saveBurnliteProjects,
  type BurnliteOverview,
} from "../lib/integrations"

const code = "rounded bg-lw-raised px-1 font-lw-mono text-lw-text-2"
const input = "min-h-10 min-w-0 flex-1 rounded-lg border border-lw-line-strong bg-lw-raised px-3 text-sm text-lw-text outline-none focus:border-lw-accent disabled:opacity-60"
const alert = "rounded border border-red-900 bg-red-950/60 px-2 py-1.5 text-xs text-red-300"
const dangerButton = "inline-flex min-h-10 items-center justify-center rounded-lg bg-red-600 px-3.5 text-sm font-medium text-white hover:bg-red-500 disabled:opacity-50"

/** How connecting went, from the address the server's OAuth callback sent the browser to. */
export interface ConnectOutcome {
  result: "connected" | "failed"
  error?: string
}

function when(iso?: string): string {
  return iso ? new Date(iso).toLocaleString() : ""
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5 px-3 py-2 sm:flex-row sm:items-baseline sm:gap-4">
      <dt className="w-40 shrink-0 text-xs text-lw-faint">{label}</dt>
      <dd className="min-w-0 flex-1 break-words text-sm text-lw-text">{children}</dd>
    </div>
  )
}

function message(reason: unknown): string {
  return reason instanceof Error ? reason.message : String(reason)
}

// BurnliteSettings connects Legato to Burnlite, whose tasks the import dialog
// searches and the board keeps in step; Legato only reads Burnlite. Connecting
// sends the browser to Burnlite to consent, and its callback comes back to
// Settings with the outcome, which this shows once.
export function BurnliteSettings({ baseUrl, burnlite, outcome, onChanged, onDisconnected }: {
  baseUrl: string
  burnlite: BurnliteOverview
  outcome: ConnectOutcome | null
  onChanged: (burnlite: BurnliteOverview) => void
  onDisconnected: () => void
}) {
  const { connection } = burnlite
  return (
    <SettingsCard
      label="Burnlite"
      icon={Flame}
      title="Burnlite"
      description="An issue tracker. Once connected, its tasks can be imported onto the board, where they follow Burnlite's state. Legato only reads Burnlite."
    >
      <div className="flex flex-col gap-3">
        {outcome?.result === "connected" && <InlineNotice variant="success">Connected to Burnlite.</InlineNotice>}
        {outcome?.result === "failed" && (
          <InlineNotice variant="error">Could not connect to Burnlite{outcome.error ? `: ${outcome.error}` : "."}</InlineNotice>
        )}
        {connection.state === "connected"
          ? <Connected baseUrl={baseUrl} burnlite={burnlite} onChanged={onChanged} onDisconnected={onDisconnected} />
          : <Connect key={connection.state} baseUrl={baseUrl} burnlite={burnlite} />}
      </div>
    </SettingsCard>
  )
}

function Connect({ baseUrl, burnlite }: { baseUrl: string; burnlite: BurnliteOverview }) {
  const [address, setAddress] = useState(burnlite.connection.url || burnlite.configured_url || "")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const connect = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    setError(null)
    try {
      leaveFor(await connectBurnlite(baseUrl, address.trim()))
    } catch (reason) {
      setError(message(reason))
      setBusy(false)
    }
  }

  return (
    <form onSubmit={(event) => void connect(event)} className="flex flex-col gap-2">
      {burnlite.connection.state === "disconnected" ? (
        <p className="text-xs text-lw-danger">Burnlite refused Legato's tokens, so its tasks are not synced. Connect again to carry on.</p>
      ) : (
        <p className="text-xs text-lw-text-2">Not connected. Connecting opens Burnlite, which asks you to let Legato read its tasks.</p>
      )}
      <div className="flex flex-col gap-2 sm:flex-row">
        <input
          aria-label="Burnlite address"
          type="url"
          required
          value={address}
          onChange={(event) => setAddress(event.target.value)}
          placeholder="https://burnlite.example.com"
          className={`${input} font-lw-mono`}
        />
        <button type="submit" className={primaryButton} disabled={busy || !address.trim()}>{busy ? "Connecting…" : "Connect"}</button>
      </div>
      {error && <p role="alert" className={alert}>{error}</p>}
    </form>
  )
}

function Connected({ baseUrl, burnlite, onChanged, onDisconnected }: {
  baseUrl: string
  burnlite: BurnliteOverview
  onChanged: (burnlite: BurnliteOverview) => void
  onDisconnected: () => void
}) {
  const [projects, setProjects] = useState(burnlite.projects.join(", "))
  const [saving, setSaving] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [disconnecting, setDisconnecting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const url = burnlite.connection.url || burnlite.configured_url

  const save = async (event: FormEvent) => {
    event.preventDefault()
    setSaving(true)
    setError(null)
    try {
      const saved = await saveBurnliteProjects(baseUrl, parseProjectKeys(projects))
      setProjects(saved.projects.join(", "))
      onChanged(saved)
    } catch (reason) {
      setError(message(reason))
    } finally {
      setSaving(false)
    }
  }

  const disconnect = async () => {
    setDisconnecting(true)
    setError(null)
    try {
      await disconnectBurnlite(baseUrl)
      setConfirming(false)
      onDisconnected()
    } catch (reason) {
      setError(message(reason))
    } finally {
      setDisconnecting(false)
    }
  }

  return (
    <>
      <dl className="divide-y divide-lw-line overflow-hidden rounded-lg border border-lw-line">
        {url && (
          <Row label="Address">
            <a href={url} target="_blank" rel="noopener noreferrer" className="font-lw-mono text-lw-link hover:underline">{url}</a>
          </Row>
        )}
        <Row label="Connection">Connected since {when(burnlite.connection.connected_at)}</Row>
        <Row label="Projects to search">
          <form onSubmit={(event) => void save(event)} className="flex flex-col gap-1.5">
            <div className="flex flex-col gap-2 sm:flex-row">
              <input
                aria-label="Projects"
                value={projects}
                disabled={burnlite.projects_from_config}
                onChange={(event) => setProjects(event.target.value)}
                placeholder="Every project"
                className={`${input} font-lw-mono`}
              />
              {!burnlite.projects_from_config && (
                <button type="submit" className={quietButton} disabled={saving}>{saving ? "Saving…" : "Save"}</button>
              )}
            </div>
            <span className="text-xs text-lw-text-2">
              {burnlite.projects_from_config
                ? <>Set in config.yaml (<code className={code}>burnlite.projects</code>); change them there.</>
                : <>Project keys, separated by commas, whose open tasks import searches.</>}
              {" "}{burnlite.projects.length === 0 ? "Now searching every project." : `Now searching ${burnlite.projects.join(", ")}.`}
            </span>
          </form>
        </Row>
      </dl>
      {error && <p role="alert" className={alert}>{error}</p>}
      <div className="flex flex-wrap items-center gap-2">
        {confirming ? (
          <>
            <span className="text-xs text-lw-text-2">Stop reading tasks from Burnlite?</span>
            <button type="button" className={dangerButton} disabled={disconnecting} onClick={() => void disconnect()}>Disconnect from Burnlite</button>
            <button type="button" className={quietButton} disabled={disconnecting} onClick={() => setConfirming(false)}>Keep</button>
          </>
        ) : (
          <button type="button" className={quietButton} onClick={() => setConfirming(true)}>Disconnect</button>
        )}
      </div>
    </>
  )
}
