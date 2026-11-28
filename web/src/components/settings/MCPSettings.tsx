import { useCallback, useEffect, useRef, useState, type FormEvent, type ReactNode } from "react"
import { Plug } from "lucide-react"
import { SettingsCard, Toggle, primaryButton, quietButton } from "./SettingsUI"
import { InlineNotice } from "../ui/InlineNotice"
import {
  addMCPConnection,
  appCanConnect,
  beginWebAuthorization,
  cancelAuthorization,
  changeMCPConnection,
  confirmAuthorization,
  connectThroughApp,
  disconnectMCPConnection,
  getAuthorization,
  getMCPConnection,
  listMCPConnections,
  removeMCPConnection,
  isPendingApproval,
  testMCPConnection,
  type MCPConnection,
  type MCPTestResult,
  type MCPRule,
  type OAuthAttempt,
} from "../../lib/mcpConnections"

const input = "min-h-10 min-w-0 rounded-lg border border-lw-line-strong bg-lw-raised px-3 text-sm text-lw-text outline-none placeholder:text-lw-faint focus:border-lw-accent disabled:opacity-60"
const dangerButton = "inline-flex min-h-10 items-center justify-center rounded-lg bg-red-600 px-3.5 text-sm font-medium text-white hover:bg-red-500 disabled:opacity-50"
const chip = "rounded border border-lw-line px-1.5 py-px font-lw-mono text-[10px] uppercase tracking-wide"

/** Shown before any browser-only connect, and again when one times out. */
export const browserOnly = "Browser-only connection setup requires Legato on this computer. For a remote server, use the Legato app."

const authorizationLabel: Record<string, string> = {
  not_required: "No authorization needed",
  required: "Not connected",
  pending: "Connecting",
  awaiting_confirmation: "Waiting for confirmation",
  authorized: "Authorized",
  reauth_required: "Needs connecting again",
}

const disconnected: Record<string, string> = {
  revoked: "Disconnected. The server revoked Legato's tokens.",
  not_supported: "Disconnected. The server cannot revoke tokens, so Legato forgot them.",
  failed: "Disconnected. The server could not be told, so Legato forgot its tokens.",
  none: "Disconnected.",
}

function message(reason: unknown): string {
  return reason instanceof Error ? reason.message : String(reason)
}

/** What an attempt that ended without an authorization means for the operator. */
function ended(attempt: OAuthAttempt): ReactNode {
  switch (attempt.status) {
    case "expired":
      return <>The consent page's answer did not reach Legato in time. {browserOnly}</>
    case "denied":
      return "Authorization was refused on the consent page."
    case "interrupted":
      return "Legato stopped listening for the consent page's answer, for example because it restarted. Connect again."
    case "failed":
      return attempt.error_code === "token_exchange_failed"
        ? "The authorization server did not accept the authorization. Connect again."
        : `Authorizing failed (${attempt.error_code || "unknown"}). Connect again.`
    default:
      return null
  }
}

/** What a change that runs a command waits for. */
function waitingText(name: string, what: "run" | "change"): string {
  return `Waiting for your approval to ${what === "run" ? "run" : "change what runs for"} ${name}. A local server runs on this computer as you, outside any sandbox, so Legato asks first: approve it in the approvals tray${what === "run" ? ", and it appears here" : ""}.`
}

/** A local server's command line, as the operator typed it. */
function commandLine(conn: MCPConnection): string {
  return [conn.command ?? "", ...(conn.args ?? [])].join(" ")
}

function lines(text: string): string[] {
  return text.split("\n").map((l) => l.trim()).filter(Boolean)
}

function envFromText(text: string): Record<string, string> {
  const env: Record<string, string> = {}
  for (const line of lines(text)) {
    const at = line.indexOf("=")
    if (at > 0) env[line.slice(0, at).trim()] = line.slice(at + 1)
  }
  return env
}

function envToText(env: Record<string, string> | undefined): string {
  return Object.entries(env ?? {}).map(([k, v]) => `${k}=${v}`).join("\n")
}

// MCPSettings is where the operator adds upstream MCP servers, authorizes
// Legato with them, and says which agents may use which of their tools.
// Agents never see a token: Legato calls the tools for them.
export function MCPSettings({ baseUrl, pollEvery = 2000 }: { baseUrl: string; pollEvery?: number }) {
  const { list, error, load, replace, removed, added, waitingFor, waitFor } = useConnections(baseUrl, pollEvery)
  const app = appCanConnect()
  return (
    <SettingsCard id="mcp-connections" icon={Plug} title="MCP servers"
      description="MCP servers whose tools agents can use through Legato: remote ones, which Legato holds the authorization for, and local commands, which Legato runs on this computer. Agents never see a token, and a sandboxed agent needs nothing of a local server's.">
      <div className="flex flex-col gap-3">
        {app
          ? <InlineNotice>Connecting opens the server's consent page in your browser. The Legato app receives its answer and hands it to this server.</InlineNotice>
          : <InlineNotice>{browserOnly}</InlineNotice>}
        {error && <p role="alert" className="text-sm text-lw-error">{error}</p>}
        {list && list.length === 0 && <p className="text-sm text-lw-text-2">No MCP servers yet.</p>}
        {list && list.length > 0 && (
          <ul className="divide-y divide-lw-line overflow-hidden rounded-lg border border-lw-line">
            {list.map((conn) => (
              <Connection key={conn.name} baseUrl={baseUrl} conn={conn} app={app} pollEvery={pollEvery}
                onChanged={replace} onReload={load} onRemoved={() => removed(conn.name)} />
            ))}
          </ul>
        )}
        {waitingFor.map((name) => (
          <InlineNotice key={name}>{waitingText(name, "run")}</InlineNotice>
        ))}
        <AddConnection baseUrl={baseUrl} onAdded={added} onPending={waitFor} />
      </div>
    </SettingsCard>
  )
}

/** The names of servers not yet in the list. */
function unlisted(names: string[], list: MCPConnection[]): string[] {
  return names.filter((n) => !list.some((c) => c.name === n))
}

function withConnection(list: MCPConnection[], conn: MCPConnection): MCPConnection[] {
  return list.map((c) => (c.name === conn.name ? conn : c))
}

function withoutConnection(list: MCPConnection[], name: string): MCPConnection[] {
  return list.filter((c) => c.name !== name)
}

// useConnections lists the connections. Local servers waiting for the
// operator's approval are listed by name, and the list is read again until
// each appears.
function useConnections(baseUrl: string, pollEvery: number) {
  const [list, setList] = useState<MCPConnection[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      setList(await listMCPConnections(baseUrl))
      setError(null)
    } catch (reason) {
      setError(message(reason))
    }
  }, [baseUrl])

  useEffect(() => { void load() }, [load])

  const [waitingFor, setWaitingFor] = useState<string[]>([])
  useEffect(() => {
    if (list) setWaitingFor((names) => unlisted(names, list))
  }, [list])
  useEffect(() => {
    if (waitingFor.length === 0) return
    const timer = setInterval(() => void load(), pollEvery)
    return () => clearInterval(timer)
  }, [waitingFor.length, load, pollEvery])

  return {
    list, error, load, waitingFor,
    replace: (conn: MCPConnection) => setList((current) => current && withConnection(current, conn)),
    removed: (name: string) => setList((current) => current && withoutConnection(current, name)),
    added: (conn: MCPConnection) => setList((current) => [...(current ?? []), conn]),
    waitFor: (name: string) => setWaitingFor((names) => (names.includes(name) ? names : [...names, name])),
  }
}

type ConnectionNotice = { variant: "info" | "success" | "error"; text: ReactNode }

/** What a disconnect says, with whether the server revoked Legato's tokens. */
function disconnectNotice(conn: MCPConnection, upstream: string): string {
  const said = disconnected[upstream] ?? disconnected.none
  return conn.shared_with === "burnlite" ? `${said} The Burnlite board is disconnected too.` : said
}

/** Whether consent is under way: the page is open, or its answer is being exchanged. */
function consentUnderWay(attempt: OAuthAttempt | null): attempt is OAuthAttempt {
  return attempt?.status === "pending" || attempt?.status === "exchanging"
}

// useAttempt follows the connection's authorization attempt: one begun here,
// or found again after a reload, asked after while consent is under way. An
// attempt that ends without an authorization is said and forgotten.
function useAttempt({ baseUrl, conn, pollEvery, refresh, setNotice }: {
  baseUrl: string; conn: MCPConnection; pollEvery: number
  refresh: () => Promise<MCPConnection>; setNotice: (notice: ConnectionNotice) => void
}) {
  const [attempt, setAttempt] = useState<OAuthAttempt | null>(null)
  const followed = useRef<string | null>(null)

  const follow = useCallback(async (id: string) => {
    followed.current = id
    try {
      setAttempt(await getAuthorization(baseUrl, id))
    } catch {
      // Another operator token's attempt, or one already over.
      followed.current = null
    }
  }, [baseUrl])

  const forget = useCallback(() => {
    setAttempt(null)
    followed.current = null
  }, [])

  // A page reloaded mid-way finds its attempt again.
  useEffect(() => {
    if (conn.attempt && conn.attempt !== followed.current) void follow(conn.attempt)
  }, [conn.attempt, follow])

  // moved takes how an attempt stands now; one that ended without an
  // authorization is said and forgotten.
  const moved = useCallback((before: OAuthAttempt, next: OAuthAttempt) => {
    if (next.status === before.status) return
    setAttempt(next)
    const over = ended(next)
    if (!over) return
    setNotice({ variant: "error", text: over })
    forget()
    void refresh()
  }, [forget, refresh, setNotice])

  // While consent is under way, ask how it stands.
  const waiting = consentUnderWay(attempt)
  useEffect(() => {
    if (!waiting || !attempt) return
    const poll = () => void getAuthorization(baseUrl, attempt.id).then((next) => moved(attempt, next)).catch(() => undefined)
    const timer = setInterval(poll, pollEvery)
    return () => clearInterval(timer)
  }, [waiting, attempt, baseUrl, pollEvery, moved])

  const begun = (id: string, expiresAt: number) => {
    followed.current = id
    setAttempt({ id, connection: conn.name, status: "pending", expires_at: expiresAt })
  }
  return { attempt, waiting, follow, forget, begun }
}

// useConnectionActions is everything a connection's row does, one action at
// a time: connect (through the app, or a tab of this browser), confirm or
// cancel the attempt, disconnect, remove, change, and test.
function useConnectionActions({ baseUrl, conn, app, pollEvery, onChanged, onReload, onRemoved }: ConnectionProps) {
  const [notice, setNotice] = useState<ConnectionNotice | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [confirming, setConfirming] = useState<"disconnect" | "remove" | null>(null)
  const [tested, setTested] = useState<MCPTestResult | null>(null)

  const refresh = useCallback(async () => {
    const fresh = await getMCPConnection(baseUrl, conn.name)
    onChanged(fresh)
    return fresh
  }, [baseUrl, conn.name, onChanged])
  const attempts = useAttempt({ baseUrl, conn, pollEvery, refresh, setNotice })
  const { attempt, forget } = attempts

  const run = async (work: () => Promise<void>) => {
    setBusy(true)
    setError(null)
    try {
      await work()
    } catch (reason) {
      setError(message(reason))
    } finally {
      setBusy(false)
    }
  }
  const connecting = useConnect({ baseUrl, conn, app, run, refresh, attempts, setNotice })

  // settle confirms or cancels the attempt under way.
  const settle = async (action: typeof confirmAuthorization | typeof cancelAuthorization, after?: () => void) => {
    if (!attempt) return
    await action(baseUrl, attempt.id)
    forget()
    after?.()
    await refresh()
  }

  // A change is saved against the revision shown; one refused (changed
  // elsewhere first) reads the list again. One that runs a command waits for
  // the operator's approval.
  const saveChange = async (patch: ConnectionPatch) => {
    try {
      const out = await changeMCPConnection(baseUrl, conn.name, { ...patch, expected_revision: conn.revision })
      if (isPendingApproval(out)) setNotice({ variant: "info", text: waitingText(conn.name, "change") })
      else onChanged(out)
    } catch (reason) {
      await onReload()
      throw reason
    }
  }
  const change = (patch: ConnectionPatch) => run(() => saveChange(patch))

  return {
    ...connecting, attempt, waiting: attempts.waiting, notice, busy, error, confirming, setConfirming, tested, change,
    confirm: () => void run(() => settle(confirmAuthorization, () => setNotice({ variant: "success", text: `Legato is authorized to use ${conn.name}.` }))),
    cancel: () => void run(() => settle(cancelAuthorization)),
    disconnect: () => void run(async () => {
      const out = await disconnectMCPConnection(baseUrl, conn.name)
      setConfirming(null)
      setNotice({ variant: "info", text: disconnectNotice(conn, out.upstream) })
      await refresh()
    }),
    remove: () => void run(async () => {
      await removeMCPConnection(baseUrl, conn.name)
      onRemoved()
    }),
    test: () => void run(async () => {
      setTested(null)
      setTested(await testMCPConnection(baseUrl, conn.name))
      await refresh()
    }),
  }
}

type ConnectionPatch = Omit<Parameters<typeof changeMCPConnection>[2], "expected_revision">

// useConnect begins an authorization: through the Legato app, which catches
// the consent page's answer, or in a tab of this browser.
function useConnect({ baseUrl, conn, app, run, refresh, attempts, setNotice }: {
  baseUrl: string; conn: MCPConnection; app: boolean
  run: (work: () => Promise<void>) => Promise<void>; refresh: () => Promise<MCPConnection>
  attempts: ReturnType<typeof useAttempt>; setNotice: (notice: ConnectionNotice | null) => void
}) {
  const [consentUrl, setConsentUrl] = useState<string | null>(null)
  const [viaApp, setViaApp] = useState(false)

  const throughApp = () => {
    setViaApp(true)
    void run(async () => {
      const outcome = await connectThroughApp(conn.name)
      setViaApp(false)
      if (outcome.error) throw new Error(outcome.error)
      const fresh = await refresh()
      if (fresh.attempt) await attempts.follow(fresh.attempt)
    })
  }

  // Opened now, while the click still counts, and pointed at the consent
  // page once Legato is listening for its answer.
  const inBrowser = () => {
    const tab = window.open("", "_blank")
    void run(async () => {
      try {
        const begun = await beginWebAuthorization(baseUrl, conn.name)
        attempts.begun(begun.id, begun.expires_at)
        if (!tab) {
          setConsentUrl(begun.authorize_url)
          return
        }
        tab.opener = null
        tab.location.href = begun.authorize_url
      } catch (reason) {
        tab?.close()
        throw reason
      }
    })
  }

  const connect = () => {
    setNotice(null)
    setConsentUrl(null)
    if (app) throughApp()
    else inBrowser()
  }
  return { connect, consentUrl, viaApp }
}

type ConnectionProps = {
  baseUrl: string
  conn: MCPConnection
  app: boolean
  pollEvery: number
  onChanged: (conn: MCPConnection) => void
  onReload: () => Promise<void>
  onRemoved: () => void
}

function Connection(props: ConnectionProps) {
  const { conn } = props
  const actions = useConnectionActions(props)
  const { busy } = actions
  const local = conn.kind === "stdio"
  return (
    <li aria-label={conn.name} className="flex flex-col gap-2 px-3 py-3">
      <div className="flex items-start gap-3">
        <ConnectionSummary conn={conn} />
        <Toggle label={`Use ${conn.name}`} on={conn.enabled} disabled={busy} onChange={() => void actions.change({ enabled: !conn.enabled })} />
      </div>

      {actions.notice && <InlineNotice variant={actions.notice.variant}>{actions.notice.text}</InlineNotice>}
      {actions.error && <p role="alert" className="text-xs text-lw-error">{actions.error}</p>}

      {conn.auth === "oauth" && <Authorization conn={conn} actions={actions} />}

      <div className="flex flex-wrap items-center gap-2">
        <button type="button" className={quietButton} aria-label={`Test ${conn.name}`} disabled={busy || !conn.enabled} onClick={actions.test}>Test</button>
        {local && <span className="text-[11px] text-lw-faint">Starts it, lists its tools, and stops it.</span>}
      </div>
      {actions.tested && <TestResult result={actions.tested} />}

      {local && <EditLocal conn={conn} busy={busy}
        onSave={(command, args, dir, env) => actions.change({ command, args, dir, env })} />}

      <Rules conn={conn} busy={busy} onSave={(rules) => actions.change({ rules })} />

      <RemoveConnection name={conn.name} busy={busy} confirming={actions.confirming === "remove"}
        onAsk={() => actions.setConfirming("remove")} onKeep={() => actions.setConfirming(null)} onRemove={actions.remove} />
    </li>
  )
}

// Authorization is an OAuth connection's consent: confirming what it gave,
// waiting for it, or Connect and Disconnect.
function Authorization({ conn, actions }: { conn: MCPConnection; actions: ReturnType<typeof useConnectionActions> }) {
  const { attempt, viaApp, busy } = actions
  return <>
    {attempt?.status === "awaiting_confirmation" && <ConfirmAttempt name={conn.name} attempt={attempt} busy={busy} onConfirm={actions.confirm} onCancel={actions.cancel} />}
    {(actions.waiting || viaApp) && <ConsentWaiting viaApp={viaApp} consentUrl={actions.consentUrl} busy={busy} onCancel={attempt ? actions.cancel : undefined} />}
    {!attempt && !viaApp && (actions.confirming === "disconnect"
      ? <ConfirmDisconnect conn={conn} busy={busy} onDisconnect={actions.disconnect} onKeep={() => actions.setConfirming(null)} />
      : <ConnectButtons conn={conn} busy={busy} onConnect={actions.connect} onDisconnect={() => actions.setConfirming("disconnect")} />)}
  </>
}

// ConnectionSummary is a connection's name and where it stands, what it
// runs or where it is, and what is wrong with it.
function ConnectionSummary({ conn }: { conn: MCPConnection }) {
  const local = conn.kind === "stdio"
  return (
    <div className="min-w-0 flex-1">
      <ConnectionChips conn={conn} />
      {local ? (
        <>
          <p className="mt-0.5 break-all font-lw-mono text-[11px] text-lw-text-2">{commandLine(conn)}</p>
          <p className="break-all font-lw-mono text-[11px] text-lw-faint">in {conn.dir}</p>
        </>
      ) : (
        <p className="mt-0.5 truncate font-lw-mono text-[11px] text-lw-faint">{conn.url}</p>
      )}
      {conn.problem && <p className="mt-0.5 text-xs text-lw-text-2">{conn.problem.message}</p>}
      {conn.shared_with === "burnlite" && (
        <p className="mt-0.5 text-xs text-lw-text-2">Also the Burnlite board's connection (Settings › Board and tickets): connecting or disconnecting here does both. Turning it off is only for agents.</p>
      )}
    </div>
  )
}

function ConnectionChips({ conn }: { conn: MCPConnection }) {
  const local = conn.kind === "stdio"
  const ready = conn.authorization === "authorized" || conn.authorization === "not_required"
  return (
    <div className="flex flex-wrap items-center gap-2">
      <span className="font-lw-mono text-sm text-lw-text">{conn.name}</span>
      <span className={`${chip} ${ready ? "text-lw-live" : "text-lw-text-2"}`}>{authorizationLabel[conn.authorization] ?? conn.authorization}</span>
      {local && <span className={`${chip} text-lw-text-2`}>local</span>}
      {conn.connection === "failed" && <span className={`${chip} text-lw-error`}>{local ? "failed" : "unreachable"}</span>}
      {!conn.enabled && <span className={`${chip} text-lw-faint`}>off</span>}
    </div>
  )
}

// ConfirmAttempt shows what consent gave, for the operator to check before
// Legato uses it.
function ConfirmAttempt({ name, attempt, busy, onConfirm, onCancel }: { name: string; attempt: OAuthAttempt; busy: boolean; onConfirm: () => void; onCancel: () => void }) {
  const evidence = attempt.evidence
  return (
    <div className="flex flex-col gap-2 rounded-lg border border-lw-line p-2">
      <p className="text-xs text-lw-text-2">Check that this is the account and server you meant, then confirm. Until then Legato does not use it.</p>
      <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-xs">
        {evidence?.account && <><dt className="text-lw-faint">Account</dt><dd className="break-words text-lw-text">{evidence.account}</dd></>}
        <dt className="text-lw-faint">Authorization server</dt><dd className="break-words font-lw-mono text-lw-text">{evidence?.issuer}</dd>
        <dt className="text-lw-faint">Resource</dt><dd className="break-words font-lw-mono text-lw-text">{evidence?.resource}</dd>
        <dt className="text-lw-faint">Scope</dt><dd className="break-words font-lw-mono text-lw-text">{evidence?.granted_scope || evidence?.requested_scope || attempt.scope || "none named"}</dd>
        <dt className="text-lw-faint">Renews itself</dt><dd className="text-lw-text">{evidence?.refreshable ? "Yes" : "No: connect again when it expires"}</dd>
      </dl>
      <div className="flex flex-wrap gap-2">
        <button type="button" className={primaryButton} aria-label={`Confirm ${name}`} disabled={busy} onClick={onConfirm}>Confirm</button>
        <button type="button" className={quietButton} disabled={busy} onClick={onCancel}>Cancel</button>
      </div>
    </div>
  )
}

function ConsentWaiting({ viaApp, consentUrl, busy, onCancel }: { viaApp: boolean; consentUrl: string | null; busy: boolean; onCancel?: () => void }) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <p className="text-xs text-lw-text-2">
        {viaApp
          ? "The Legato app opened the consent page in your browser. Waiting for you to consent there."
          : "Waiting for you to consent in the tab Legato opened."}
        {consentUrl && <> <a href={consentUrl} target="_blank" rel="noopener noreferrer" className="text-lw-link hover:underline">Open the consent page</a>.</>}
      </p>
      {onCancel && <button type="button" className={quietButton} disabled={busy} onClick={onCancel}>Cancel</button>}
    </div>
  )
}

function ConfirmDisconnect({ conn, busy, onDisconnect, onKeep }: { conn: MCPConnection; busy: boolean; onDisconnect: () => void; onKeep: () => void }) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <span className="text-xs text-lw-text-2">
        Stop Legato using {conn.name}, and ask the server to revoke its tokens?
        {conn.shared_with === "burnlite" && " The Burnlite board stops syncing too."}
      </span>
      <button type="button" className={dangerButton} disabled={busy} onClick={onDisconnect}>Disconnect {conn.name}</button>
      <button type="button" className={quietButton} disabled={busy} onClick={onKeep}>Keep</button>
    </div>
  )
}

function ConnectButtons({ conn, busy, onConnect, onDisconnect }: { conn: MCPConnection; busy: boolean; onConnect: () => void; onDisconnect: () => void }) {
  const authorized = conn.authorization === "authorized"
  return (
    <div className="flex flex-wrap items-center gap-2">
      <button type="button" className={authorized ? quietButton : primaryButton} aria-label={`Connect ${conn.name}`} disabled={busy || !conn.enabled} onClick={onConnect}>
        {authorized || conn.authorization === "reauth_required" ? "Connect again" : "Connect"}
      </button>
      {authorized && <button type="button" className={quietButton} disabled={busy} onClick={onDisconnect}>Disconnect</button>}
    </div>
  )
}

function RemoveConnection({ name, busy, confirming, onAsk, onKeep, onRemove }: {
  name: string; busy: boolean; confirming: boolean; onAsk: () => void; onKeep: () => void; onRemove: () => void
}) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      {confirming ? (
        <>
          <span className="text-xs text-lw-text-2">Remove {name}, its authorization and its rules?</span>
          <button type="button" className={dangerButton} disabled={busy} onClick={onRemove}>Remove {name}</button>
          <button type="button" className={quietButton} disabled={busy} onClick={onKeep}>Keep</button>
        </>
      ) : (
        <button type="button" className={quietButton} disabled={busy} onClick={onAsk}>Remove</button>
      )}
    </div>
  )
}

// What a test found: the tools, or why it failed, with the server's error output.
function TestResult({ result }: { result: MCPTestResult }) {
  return (
    <div className="flex flex-col gap-1 rounded-lg border border-lw-line p-2 text-xs">
      {result.error
        ? <p role="alert" className="break-words text-lw-error">{result.error}</p>
        : <p className="text-lw-text-2">{result.tools.length} tool{result.tools.length === 1 ? "" : "s"}:</p>}
      {result.tools.length > 0 && (
        <ul className="flex flex-col gap-0.5">
          {result.tools.map((tool) => (
            <li key={tool.name} className="flex gap-2">
              <span className="font-lw-mono text-lw-text">{tool.name}</span>
              {tool.description && <span className="min-w-0 truncate text-lw-faint">{tool.description}</span>}
            </li>
          ))}
        </ul>
      )}
      {(result.rejected ?? []).map((r) => <p key={r.name} className="text-lw-text-2">Not offered: <span className="font-lw-mono">{r.name}</span>, {r.reason}</p>)}
      {result.stderr && (
        <details open={Boolean(result.error)}>
          <summary className="cursor-pointer text-lw-text-2">Error output</summary>
          <pre className="mt-1 max-h-48 overflow-auto whitespace-pre-wrap break-words rounded bg-lw-ground p-2 font-lw-mono text-[11px]">{result.stderr}</pre>
        </details>
      )}
    </div>
  )
}

// What a local server runs. A change is asked of the operator as an approval.
function EditLocal({ conn, busy, onSave }: {
  conn: MCPConnection
  busy: boolean
  onSave: (command: string, args: string[], dir: string, env: Record<string, string>) => Promise<void>
}) {
  const [command, setCommand] = useState(conn.command ?? "")
  const [args, setArgs] = useState((conn.args ?? []).join("\n"))
  const [dir, setDir] = useState(conn.dir ?? "")
  const [env, setEnv] = useState(envToText(conn.env))
  const submit = (event: FormEvent) => {
    event.preventDefault()
    void onSave(command.trim(), lines(args), dir.trim(), envFromText(env))
  }
  return (
    <details className="rounded-lg border border-lw-line">
      <summary className="cursor-pointer px-2 py-1.5 text-xs text-lw-text-2">Change what it runs</summary>
      <form onSubmit={submit} className="flex flex-col gap-2 p-2">
        <LocalFields name={conn.name} command={command} setCommand={setCommand} args={args} setArgs={setArgs} dir={dir} setDir={setDir} env={env} setEnv={setEnv} />
        <button type="submit" className={quietButton} aria-label={`Ask to change ${conn.name}`} disabled={busy || !command.trim() || !dir.trim()}>Ask to change</button>
      </form>
    </details>
  )
}

function LocalFields({ name, command, setCommand, args, setArgs, dir, setDir, env, setEnv }: {
  name?: string
  command: string
  setCommand: (v: string) => void
  args: string
  setArgs: (v: string) => void
  dir: string
  setDir: (v: string) => void
  env: string
  setEnv: (v: string) => void
}) {
  const of = name ? ` of ${name}` : ""
  return (
    <>
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
        <input aria-label={`Command${of}`} value={command} onChange={(e) => setCommand(e.target.value)} placeholder="/usr/bin/node, or node with PATH below" className={`${input} font-lw-mono`} />
        <input aria-label={`Working directory${of}`} value={dir} onChange={(e) => setDir(e.target.value)} placeholder="~/Projects/my-repo" className={`${input} font-lw-mono`} />
        <textarea aria-label={`Arguments${of}, one per line`} value={args} onChange={(e) => setArgs(e.target.value)} placeholder="tools/mcp.mjs" rows={2} className={`${input} py-2 font-lw-mono`} />
        <textarea aria-label={`Environment${of}, one NAME=value per line`} value={env} onChange={(e) => setEnv(e.target.value)} placeholder={"PATH=/home/me/.local/share/mise/installs/node/24/bin:/usr/bin\nAPI_KEY=legato://my-secret"} rows={2} className={`${input} py-2 font-lw-mono`} />
      </div>
      <p className="text-[11px] text-lw-faint">
        Runs on this computer as you, outside any sandbox, one process for each agent session that uses it. A bare command is found in the PATH set here, else in Legato's own. It gets only HOME, the user, locale and XDG folders, PATH and these variables; a value legato://name is that secret.
      </p>
    </>
  )
}

const approvals: Array<[string, string]> = [
  ["request", "Ask each time"],
  ["session", "Ask once per agent session"],
  ["window", "Ask, then allow for 30 minutes"],
  ["auto", "Allow without asking"],
  ["deny", "Refuse"],
]

function describeRule(rule: MCPRule): string {
  const access = rule.access === "read" ? "read-only tools" : rule.access === "write" ? "tools that change things" : "tools"
  const approve = approvals.find(([value]) => value === rule.approve)?.[1] ?? rule.approve
  return `${access} matching ${rule.tools}, for agents in ${rule.repository}: ${approve.toLowerCase()}`
}

// Rules say which agents may use which tools. With none, no agent may.
function Rules({ conn, busy, onSave }: { conn: MCPConnection; busy: boolean; onSave: (rules: MCPRule[]) => Promise<void> }) {
  const [tools, setTools] = useState("*")
  const [repository, setRepository] = useState("*/*")
  const [access, setAccess] = useState("")
  const [approve, setApprove] = useState("request")

  const add = (event: FormEvent) => {
    event.preventDefault()
    const rule: MCPRule = { tools: tools.trim(), repository: repository.trim(), approve }
    if (access) rule.access = access
    void onSave([...conn.rules, rule])
  }

  return (
    <details className="rounded-lg border border-lw-line">
      <summary className="cursor-pointer px-2 py-1.5 text-xs text-lw-text-2">
        Agent access: {conn.rules.length === 0 ? "no agent may use its tools" : `${conn.rules.length} rule${conn.rules.length === 1 ? "" : "s"}`}
      </summary>
      <div className="flex flex-col gap-2 p-2">
        {conn.rules.length > 0 && (
          <ul aria-label={`Rules for ${conn.name}`} className="flex flex-col gap-1">
            {conn.rules.map((rule, i) => (
              <li key={i} className="flex items-center gap-2 text-xs text-lw-text">
                <span className="min-w-0 flex-1 break-words">{describeRule(rule)}</span>
                <button type="button" className={quietButton} disabled={busy} aria-label={`Remove rule ${i + 1} of ${conn.name}`}
                  onClick={() => void onSave(conn.rules.filter((_, j) => j !== i))}>Remove</button>
              </li>
            ))}
          </ul>
        )}
        <form onSubmit={add} className="grid grid-cols-1 gap-2 sm:grid-cols-2">
          <input aria-label={`Tools of ${conn.name}`} value={tools} onChange={(e) => setTools(e.target.value)} placeholder="search, list_*" className={`${input} font-lw-mono`} />
          <input aria-label={`Repository for ${conn.name}`} value={repository} onChange={(e) => setRepository(e.target.value)} placeholder="owner/name" className={`${input} font-lw-mono`} />
          <select aria-label={`Access to ${conn.name}`} value={access} onChange={(e) => setAccess(e.target.value)} className={input}>
            <option value="">Any tool</option>
            <option value="read">Read-only tools</option>
            <option value="write">Tools that change things</option>
          </select>
          <select aria-label={`Approval for ${conn.name}`} value={approve} onChange={(e) => setApprove(e.target.value)} className={input}>
            {approvals.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
          </select>
          <button type="submit" className={quietButton} disabled={busy || !tools.trim() || !repository.trim()}>Add rule</button>
        </form>
        <p className="text-[11px] text-lw-faint">Tools and repositories take * patterns; agents working in no repository are -/-. A tool counts as read-only only when you list it as one.</p>
      </div>
    </details>
  )
}

type RemoteDraft = { url: string; auth: "oauth" | "none"; issuer: string; networks: string; clientID: string; redirect: string; secret: string }
type LocalDraft = { command: string; args: string; dir: string; env: string }
const emptyRemote: RemoteDraft = { url: "", auth: "oauth", issuer: "", networks: "", clientID: "", redirect: "", secret: "" }
const emptyLocal: LocalDraft = { command: "", args: "", dir: "", env: "" }
type ConnectionInput = Parameters<typeof addMCPConnection>[1]

/** A remote server as the form gives it: only the advanced settings typed. */
function remoteInput(name: string, d: RemoteDraft): ConnectionInput {
  const privateNetworks = d.networks.split(",").map((n) => n.trim()).filter(Boolean)
  const clientID = d.clientID.trim()
  return {
    name, url: d.url.trim(), auth: d.auth,
    ...(d.issuer.trim() ? { issuer: d.issuer.trim() } : {}),
    ...(privateNetworks.length ? { private_networks: privateNetworks } : {}),
    ...(clientID ? { static_client_id: clientID, static_redirect_uri: d.redirect.trim() } : {}),
    ...(clientID && d.secret ? { static_client_secret: d.secret } : {}),
  }
}

/** A local server as the form gives it: arguments one per line, NAME=value variables. */
function localInput(name: string, d: LocalDraft): ConnectionInput {
  const environment = envFromText(d.env)
  return {
    name, kind: "stdio", command: d.command.trim(), args: lines(d.args), dir: d.dir.trim(),
    ...(Object.keys(environment).length ? { env: environment } : {}),
  }
}

/** Whether the form names enough to add: a name, and an address or a command and its folder. */
function canAdd(kind: "http" | "stdio", name: string, remote: RemoteDraft, local: LocalDraft): boolean {
  if (!name.trim()) return false
  return kind === "http" ? remote.url.trim() !== "" : local.command.trim() !== "" && local.dir.trim() !== ""
}

// useAddConnection adds a server: one that runs a command waits for the
// operator's approval before it is saved. added runs once it is sent.
function useAddConnection(baseUrl: string, onAdded: (conn: MCPConnection) => void, onPending: (name: string) => void) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const add = async (input: ConnectionInput, added: () => void) => {
    setBusy(true)
    setError(null)
    try {
      const out = await addMCPConnection(baseUrl, input)
      if (isPendingApproval(out)) onPending(input.name)
      else onAdded(out)
      added()
    } catch (reason) {
      setError(message(reason))
    } finally {
      setBusy(false)
    }
  }
  return { busy, error, add }
}

function AddConnection({ baseUrl, onAdded, onPending }: { baseUrl: string; onAdded: (conn: MCPConnection) => void; onPending: (name: string) => void }) {
  const [kind, setKind] = useState<"http" | "stdio">("http")
  const [name, setName] = useState("")
  const [remote, setRemote] = useState(emptyRemote)
  const [local, setLocal] = useState(emptyLocal)
  const { busy, error, add } = useAddConnection(baseUrl, onAdded, onPending)
  const setLocalField = (key: keyof LocalDraft) => (value: string) => setLocal((d) => ({ ...d, [key]: value }))

  // What was typed for the kind added is cleared once it is sent; the
  // authorization chosen stays.
  const clear = () => {
    setName("")
    if (kind === "stdio") setLocal(emptyLocal)
    else setRemote((d) => ({ ...emptyRemote, auth: d.auth }))
  }
  const submit = (event: FormEvent) => {
    event.preventDefault()
    void add(kind === "stdio" ? localInput(name.trim(), local) : remoteInput(name.trim(), remote), clear)
  }

  return (
    <form onSubmit={submit} aria-label="Add an MCP server" className="flex flex-col gap-2">
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-[auto_9rem_minmax(0,1fr)_auto_auto]">
        <select aria-label="Kind of server" value={kind} onChange={(e) => setKind(e.target.value as "http" | "stdio")} className={input}>
          <option value="http">Remote</option>
          <option value="stdio">Local command</option>
        </select>
        <input aria-label="MCP server name" value={name} onChange={(e) => setName(e.target.value)} placeholder={kind === "stdio" ? "design" : "notion"} className={`${input} font-lw-mono`} />
        {kind === "http" ? <RemoteFields draft={remote} onChange={setRemote} /> : <span className="sm:col-span-2" />}
        <button type="submit" className={quietButton} disabled={busy || !canAdd(kind, name, remote, local)}>Add</button>
      </div>
      {kind === "stdio" && <LocalFields command={local.command} setCommand={setLocalField("command")} args={local.args} setArgs={setLocalField("args")}
        dir={local.dir} setDir={setLocalField("dir")} env={local.env} setEnv={setLocalField("env")} />}
      {kind === "http" && <RemoteAdvanced draft={remote} onChange={setRemote} />}
      {error && <p role="alert" className="text-xs text-lw-error">{error}</p>}
    </form>
  )
}

function RemoteFields({ draft, onChange }: { draft: RemoteDraft; onChange: (draft: RemoteDraft) => void }) {
  return (
    <>
      <input aria-label="MCP server address" type="url" value={draft.url} onChange={(e) => onChange({ ...draft, url: e.target.value })} placeholder="https://mcp.example.com/mcp" className={`${input} font-lw-mono`} />
      <select aria-label="Authorization" value={draft.auth} onChange={(e) => onChange({ ...draft, auth: e.target.value as "oauth" | "none" })} className={input}>
        <option value="oauth">OAuth</option>
        <option value="none">None</option>
      </select>
    </>
  )
}

// RemoteAdvanced is a remote server's authorization server, private
// networks, and a client registered for Legato by hand.
function RemoteAdvanced({ draft, onChange }: { draft: RemoteDraft; onChange: (draft: RemoteDraft) => void }) {
  const set = (key: keyof RemoteDraft) => (e: { target: { value: string } }) => onChange({ ...draft, [key]: e.target.value })
  return (
    <details>
      <summary className="cursor-pointer text-xs text-lw-text-2">Advanced</summary>
      <div className="mt-2 grid grid-cols-1 gap-2 sm:grid-cols-2">
        <input aria-label="Authorization server" type="url" value={draft.issuer} onChange={set("issuer")} placeholder="Authorization server, if not the one the server names" className={`${input} font-lw-mono`} />
        <input aria-label="Private networks" value={draft.networks} onChange={set("networks")} placeholder="10.0.0.0/8, for a server on your network" className={`${input} font-lw-mono`} />
      </div>
      <p className="mt-2 text-[11px] text-lw-faint">A client registered for Legato by hand, for a server that offers no registration Legato can use. Register the redirect http://127.0.0.1:&lt;port&gt;/callback with a free port; connecting listens on exactly that port.</p>
      <div className="mt-1 grid grid-cols-1 gap-2 sm:grid-cols-3">
        <input aria-label="Client ID" value={draft.clientID} onChange={set("clientID")} placeholder="Client ID" className={`${input} font-lw-mono`} />
        <input aria-label="Registered redirect" value={draft.redirect} onChange={set("redirect")} placeholder="http://127.0.0.1:47123/callback" className={`${input} font-lw-mono`} />
        <input aria-label="Client secret" type="password" autoComplete="off" value={draft.secret} onChange={set("secret")} placeholder="Client secret, if it has one" className={`${input} font-lw-mono`} />
      </div>
    </details>
  )
}
