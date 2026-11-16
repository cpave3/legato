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
  type MCPConnection,
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

// MCPSettings is where the operator adds upstream MCP servers, authorizes
// Legato with them, and says which agents may use which of their tools.
// Agents never see a token: Legato calls the tools for them.
export function MCPSettings({ baseUrl, pollEvery = 2000 }: { baseUrl: string; pollEvery?: number }) {
  const [list, setList] = useState<MCPConnection[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const app = appCanConnect()

  const load = useCallback(async () => {
    try {
      setList(await listMCPConnections(baseUrl))
      setError(null)
    } catch (reason) {
      setError(message(reason))
    }
  }, [baseUrl])

  useEffect(() => { void load() }, [load])

  const replace = (conn: MCPConnection) => setList((current) => current && current.map((c) => (c.name === conn.name ? conn : c)))

  return (
    <SettingsCard id="mcp-connections" icon={Plug} title="MCP servers"
      description="Remote MCP servers whose tools agents can use through Legato. Legato holds the authorization and calls the tools; agents never see a token.">
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
                onChanged={replace} onReload={load}
                onRemoved={() => setList((current) => current && current.filter((c) => c.name !== conn.name))} />
            ))}
          </ul>
        )}
        <AddConnection baseUrl={baseUrl} onAdded={(conn) => setList((current) => [...(current ?? []), conn])} />
      </div>
    </SettingsCard>
  )
}

function Connection({ baseUrl, conn, app, pollEvery, onChanged, onReload, onRemoved }: {
  baseUrl: string
  conn: MCPConnection
  app: boolean
  pollEvery: number
  onChanged: (conn: MCPConnection) => void
  onReload: () => Promise<void>
  onRemoved: () => void
}) {
  const [attempt, setAttempt] = useState<OAuthAttempt | null>(null)
  const [consentUrl, setConsentUrl] = useState<string | null>(null)
  const [viaApp, setViaApp] = useState(false)
  const [notice, setNotice] = useState<{ variant: "info" | "success" | "error"; text: ReactNode } | null>(null)
  const [busy, setBusy] = useState(false)
  const [confirming, setConfirming] = useState<"disconnect" | "remove" | null>(null)
  const [error, setError] = useState<string | null>(null)
  const followed = useRef<string | null>(null)

  const refresh = useCallback(async () => {
    const fresh = await getMCPConnection(baseUrl, conn.name)
    onChanged(fresh)
    return fresh
  }, [baseUrl, conn.name, onChanged])

  const follow = useCallback(async (id: string) => {
    followed.current = id
    try {
      setAttempt(await getAuthorization(baseUrl, id))
    } catch {
      // Another operator token's attempt, or one already over.
      followed.current = null
    }
  }, [baseUrl])

  // A page reloaded mid-way finds its attempt again.
  useEffect(() => {
    if (conn.attempt && conn.attempt !== followed.current) void follow(conn.attempt)
  }, [conn.attempt, follow])

  // While consent is under way, ask how it stands.
  const waiting = attempt && (attempt.status === "pending" || attempt.status === "exchanging")
  useEffect(() => {
    if (!waiting || !attempt) return
    const timer = setInterval(() => {
      void getAuthorization(baseUrl, attempt.id).then((next) => {
        if (next.status === attempt.status) return
        setAttempt(next)
        const over = ended(next)
        if (over) {
          setNotice({ variant: "error", text: over })
          setAttempt(null)
          followed.current = null
          void refresh()
        }
      }).catch(() => undefined)
    }, pollEvery)
    return () => clearInterval(timer)
  }, [waiting, attempt, baseUrl, pollEvery, refresh])

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

  const connect = () => {
    setNotice(null)
    setConsentUrl(null)
    if (app) {
      setViaApp(true)
      void run(async () => {
        const outcome = await connectThroughApp(conn.name)
        setViaApp(false)
        if (outcome.error) throw new Error(outcome.error)
        const fresh = await refresh()
        if (fresh.attempt) await follow(fresh.attempt)
      })
      return
    }
    // Opened now, while the click still counts, and pointed at the consent
    // page once Legato is listening for its answer.
    const tab = window.open("", "_blank")
    void run(async () => {
      try {
        const begun = await beginWebAuthorization(baseUrl, conn.name)
        followed.current = begun.id
        setAttempt({ id: begun.id, connection: conn.name, status: "pending", expires_at: begun.expires_at })
        if (tab) {
          tab.opener = null
          tab.location.href = begun.authorize_url
        } else {
          setConsentUrl(begun.authorize_url)
        }
      } catch (reason) {
        tab?.close()
        throw reason
      }
    })
  }

  const confirm = () => void run(async () => {
    if (!attempt) return
    await confirmAuthorization(baseUrl, attempt.id)
    setAttempt(null)
    followed.current = null
    setNotice({ variant: "success", text: `Legato is authorized to use ${conn.name}.` })
    await refresh()
  })

  const cancel = () => void run(async () => {
    if (!attempt) return
    await cancelAuthorization(baseUrl, attempt.id)
    setAttempt(null)
    followed.current = null
    await refresh()
  })

  const disconnect = () => void run(async () => {
    const out = await disconnectMCPConnection(baseUrl, conn.name)
    setConfirming(null)
    setNotice({ variant: "info", text: disconnected[out.upstream] ?? disconnected.none })
    await refresh()
  })

  const remove = () => void run(async () => {
    await removeMCPConnection(baseUrl, conn.name)
    onRemoved()
  })

  const change = (patch: Omit<Parameters<typeof changeMCPConnection>[2], "expected_revision">) => run(async () => {
    try {
      onChanged(await changeMCPConnection(baseUrl, conn.name, { ...patch, expected_revision: conn.revision }))
    } catch (reason) {
      await onReload()
      throw reason
    }
  })

  const oauth = conn.auth === "oauth"
  const authorized = conn.authorization === "authorized"
  return (
    <li aria-label={conn.name} className="flex flex-col gap-2 px-3 py-3">
      <div className="flex items-start gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-lw-mono text-sm text-lw-text">{conn.name}</span>
            <span className={`${chip} ${authorized || conn.authorization === "not_required" ? "text-lw-live" : "text-lw-text-2"}`}>{authorizationLabel[conn.authorization] ?? conn.authorization}</span>
            {conn.connection === "failed" && <span className={`${chip} text-lw-error`}>unreachable</span>}
            {!conn.enabled && <span className={`${chip} text-lw-faint`}>off</span>}
          </div>
          <p className="mt-0.5 truncate font-lw-mono text-[11px] text-lw-faint">{conn.url}</p>
          {conn.problem && <p className="mt-0.5 text-xs text-lw-text-2">{conn.problem.message}</p>}
        </div>
        <Toggle label={`Use ${conn.name}`} on={conn.enabled} disabled={busy} onChange={() => void change({ enabled: !conn.enabled })} />
      </div>

      {notice && <InlineNotice variant={notice.variant}>{notice.text}</InlineNotice>}
      {error && <p role="alert" className="text-xs text-lw-error">{error}</p>}

      {oauth && attempt?.status === "awaiting_confirmation" && (
        <div className="flex flex-col gap-2 rounded-lg border border-lw-line p-2">
          <p className="text-xs text-lw-text-2">Check that this is the account and server you meant, then confirm. Until then Legato does not use it.</p>
          <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-xs">
            {attempt.evidence?.account && <><dt className="text-lw-faint">Account</dt><dd className="break-words text-lw-text">{attempt.evidence.account}</dd></>}
            <dt className="text-lw-faint">Authorization server</dt><dd className="break-words font-lw-mono text-lw-text">{attempt.evidence?.issuer}</dd>
            <dt className="text-lw-faint">Resource</dt><dd className="break-words font-lw-mono text-lw-text">{attempt.evidence?.resource}</dd>
            <dt className="text-lw-faint">Scope</dt><dd className="break-words font-lw-mono text-lw-text">{attempt.evidence?.granted_scope || attempt.evidence?.requested_scope || attempt.scope || "none named"}</dd>
            <dt className="text-lw-faint">Renews itself</dt><dd className="text-lw-text">{attempt.evidence?.refreshable ? "Yes" : "No: connect again when it expires"}</dd>
          </dl>
          <div className="flex flex-wrap gap-2">
            <button type="button" className={primaryButton} aria-label={`Confirm ${conn.name}`} disabled={busy} onClick={confirm}>Confirm</button>
            <button type="button" className={quietButton} disabled={busy} onClick={cancel}>Cancel</button>
          </div>
        </div>
      )}

      {oauth && (waiting || viaApp) && (
        <div className="flex flex-wrap items-center gap-2">
          <p className="text-xs text-lw-text-2">
            {viaApp
              ? "The Legato app opened the consent page in your browser. Waiting for you to consent there."
              : "Waiting for you to consent in the tab Legato opened."}
            {consentUrl && <> <a href={consentUrl} target="_blank" rel="noopener noreferrer" className="text-lw-link hover:underline">Open the consent page</a>.</>}
          </p>
          {attempt && <button type="button" className={quietButton} disabled={busy} onClick={cancel}>Cancel</button>}
        </div>
      )}

      {oauth && !attempt && !viaApp && (
        <div className="flex flex-wrap items-center gap-2">
          {confirming === "disconnect" ? (
            <>
              <span className="text-xs text-lw-text-2">Stop Legato using {conn.name}, and ask the server to revoke its tokens?</span>
              <button type="button" className={dangerButton} disabled={busy} onClick={disconnect}>Disconnect {conn.name}</button>
              <button type="button" className={quietButton} disabled={busy} onClick={() => setConfirming(null)}>Keep</button>
            </>
          ) : (
            <>
              <button type="button" className={authorized ? quietButton : primaryButton} aria-label={`Connect ${conn.name}`} disabled={busy || !conn.enabled} onClick={connect}>
                {authorized || conn.authorization === "reauth_required" ? "Connect again" : "Connect"}
              </button>
              {authorized && <button type="button" className={quietButton} disabled={busy} onClick={() => setConfirming("disconnect")}>Disconnect</button>}
            </>
          )}
        </div>
      )}

      <Rules conn={conn} busy={busy} onSave={(rules) => change({ rules })} />

      <div className="flex flex-wrap items-center gap-2">
        {confirming === "remove" ? (
          <>
            <span className="text-xs text-lw-text-2">Remove {conn.name}, its authorization and its rules?</span>
            <button type="button" className={dangerButton} disabled={busy} onClick={remove}>Remove {conn.name}</button>
            <button type="button" className={quietButton} disabled={busy} onClick={() => setConfirming(null)}>Keep</button>
          </>
        ) : (
          <button type="button" className={quietButton} disabled={busy} onClick={() => setConfirming("remove")}>Remove</button>
        )}
      </div>
    </li>
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

function AddConnection({ baseUrl, onAdded }: { baseUrl: string; onAdded: (conn: MCPConnection) => void }) {
  const [name, setName] = useState("")
  const [url, setUrl] = useState("")
  const [auth, setAuth] = useState<"oauth" | "none">("oauth")
  const [issuer, setIssuer] = useState("")
  const [networks, setNetworks] = useState("")
  const [clientID, setClientID] = useState("")
  const [redirect, setRedirect] = useState("")
  const [secret, setSecret] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    setError(null)
    try {
      const privateNetworks = networks.split(",").map((n) => n.trim()).filter(Boolean)
      onAdded(await addMCPConnection(baseUrl, {
        name: name.trim(), url: url.trim(), auth,
        ...(issuer.trim() ? { issuer: issuer.trim() } : {}),
        ...(privateNetworks.length ? { private_networks: privateNetworks } : {}),
        ...(clientID.trim() ? { static_client_id: clientID.trim(), static_redirect_uri: redirect.trim() } : {}),
        ...(clientID.trim() && secret ? { static_client_secret: secret } : {}),
      }))
      setName("")
      setUrl("")
      setIssuer("")
      setNetworks("")
      setClientID("")
      setRedirect("")
      setSecret("")
    } catch (reason) {
      setError(message(reason))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={(event) => void submit(event)} aria-label="Add an MCP server" className="flex flex-col gap-2">
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-[9rem_minmax(0,1fr)_auto_auto]">
        <input aria-label="MCP server name" value={name} onChange={(e) => setName(e.target.value)} placeholder="notion" className={`${input} font-lw-mono`} />
        <input aria-label="MCP server address" type="url" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://mcp.example.com/mcp" className={`${input} font-lw-mono`} />
        <select aria-label="Authorization" value={auth} onChange={(e) => setAuth(e.target.value as "oauth" | "none")} className={input}>
          <option value="oauth">OAuth</option>
          <option value="none">None</option>
        </select>
        <button type="submit" className={quietButton} disabled={busy || !name.trim() || !url.trim()}>Add</button>
      </div>
      <details>
        <summary className="cursor-pointer text-xs text-lw-text-2">Advanced</summary>
        <div className="mt-2 grid grid-cols-1 gap-2 sm:grid-cols-2">
          <input aria-label="Authorization server" type="url" value={issuer} onChange={(e) => setIssuer(e.target.value)} placeholder="Authorization server, if not the one the server names" className={`${input} font-lw-mono`} />
          <input aria-label="Private networks" value={networks} onChange={(e) => setNetworks(e.target.value)} placeholder="10.0.0.0/8, for a server on your network" className={`${input} font-lw-mono`} />
        </div>
        <p className="mt-2 text-[11px] text-lw-faint">A client registered for Legato by hand, for a server that offers no registration Legato can use. Register the redirect http://127.0.0.1:&lt;port&gt;/callback with a free port; connecting listens on exactly that port.</p>
        <div className="mt-1 grid grid-cols-1 gap-2 sm:grid-cols-3">
          <input aria-label="Client ID" value={clientID} onChange={(e) => setClientID(e.target.value)} placeholder="Client ID" className={`${input} font-lw-mono`} />
          <input aria-label="Registered redirect" value={redirect} onChange={(e) => setRedirect(e.target.value)} placeholder="http://127.0.0.1:47123/callback" className={`${input} font-lw-mono`} />
          <input aria-label="Client secret" type="password" autoComplete="off" value={secret} onChange={(e) => setSecret(e.target.value)} placeholder="Client secret, if it has one" className={`${input} font-lw-mono`} />
        </div>
      </details>
      {error && <p role="alert" className="text-xs text-lw-error">{error}</p>}
    </form>
  )
}
