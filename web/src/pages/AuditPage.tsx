import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Link } from "react-router-dom"
import { useServer } from "../hooks/useServer"
import { apiFetch } from "../lib/api"
import { agentKindLabel } from "../lib/agentKind"
import { AUDIT_EVENTS, eventTone, fetchAudit, foldAuditRows, verifyAudit, type AuditChain, type AuditFilter, type AuditRow } from "../lib/audit"
import { LoadingState } from "../components/ui/LoadingState"
import { endCredentialGrant, grantLasts, listCredentialGrants, type CredentialGrant } from "../lib/credentials"

const PAGE = 100
const REFRESH_MS = 15_000

// AuditPage is the credential audit log: which agent asked for, was given,
// was refused or used which credential, for what, and when. Every channel
// writes to the same log, so GitHub, SSH and every other credential are here.
export function AuditPage() {
  const { baseUrl } = useServer()
  const [credentials, setCredentials] = useState<string[]>([])
  const [filter, setFilter] = useState<AuditFilter>({})
  const [rows, setRows] = useState<AuditRow[] | null>(null)
  const [more, setMore] = useState(false)
  const [chain, setChain] = useState<AuditChain | null>(null)
  const [error, setError] = useState("")
  const [loadingOlder, setLoadingOlder] = useState(false)
  const pagedBack = useRef(false)

  useEffect(() => {
    apiFetch(baseUrl, "/api/credentials")
      .then(async (resp) => (resp.ok ? resp.json() : { credentials: [] }))
      .then((body: { credentials?: { name: string }[] }) => setCredentials((body.credentials ?? []).map((c) => c.name)))
      .catch(() => {})
    verifyAudit(baseUrl).then(setChain).catch(() => setChain(null))
  }, [baseUrl])

  const load = useCallback(async () => {
    try {
      const page = await fetchAudit(baseUrl, { ...filter, limit: PAGE })
      setRows(page)
      setMore(page.length === PAGE)
      setError("")
      pagedBack.current = false
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }, [baseUrl, filter])

  useEffect(() => {
    setRows(null)
    void load()
    // New rows arrive while an agent works; older pages are left alone.
    const timer = window.setInterval(() => { if (!pagedBack.current) void load() }, REFRESH_MS)
    return () => window.clearInterval(timer)
  }, [load])

  const loadOlder = async () => {
    if (!rows?.length) return
    setLoadingOlder(true)
    try {
      const page = await fetchAudit(baseUrl, { ...filter, limit: PAGE, before: rows[rows.length - 1].seq })
      pagedBack.current = true
      setRows([...rows, ...page])
      setMore(page.length === PAGE)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setLoadingOlder(false)
    }
  }

  const folded = useMemo(() => foldAuditRows(rows ?? []), [rows])
  const set = (key: keyof AuditFilter) => (value: string) => setFilter((current) => ({ ...current, [key]: value || undefined }))

  return (
    <div className="flex h-full flex-col overflow-y-auto bg-lw-ground font-lw text-lw-text">
      <header className="flex flex-wrap items-center gap-3 border-b border-lw-line px-4 py-4 sm:px-6">
        <div className="min-w-0 flex-1">
          <h1 className="text-lg font-semibold">Audit log</h1>
          <p className="text-sm text-lw-text-2">Every request, decision and use of a credential by an agent: GitHub, SSH keys and every other credential.</p>
        </div>
        {chain && (
          <span
            title="Each row holds the hash of the row before it, so a row changed or removed afterwards breaks the chain."
            className={`rounded-md px-2 py-1 text-xs font-medium ${chain.intact ? "bg-lw-live-soft text-lw-live" : "bg-lw-error-soft text-lw-error"}`}
          >
            {chain.intact ? "✓ Chain intact" : `✕ Chain broken at #${chain.broken_at}`}
          </span>
        )}
        <button type="button" onClick={() => void load()} className="h-8 rounded-md border border-lw-line-strong px-3 text-[13px] text-lw-text-2 hover:text-lw-text">
          Refresh
        </button>
      </header>

      <StandingGrants baseUrl={baseUrl} onRevoked={() => void load()} />

      <div className="flex flex-wrap gap-2 px-4 py-3 sm:px-6">
        <Select label="Credential" value={filter.credential ?? ""} onChange={set("credential")} options={credentials} all="All credentials" />
        <Select label="Event" value={filter.event ?? ""} onChange={set("event")} options={[...AUDIT_EVENTS]} all="All events" />
        <input
          aria-label="Agent's task ID"
          placeholder="Agent's task ID"
          defaultValue=""
          onKeyDown={(event) => { if (event.key === "Enter") set("task")((event.target as HTMLInputElement).value.trim()) }}
          onBlur={(event) => set("task")(event.target.value.trim())}
          className="h-9 w-44 rounded-md border border-lw-line-strong bg-lw-ground px-3 font-lw-mono text-sm text-lw-text outline-none placeholder:font-lw placeholder:text-lw-faint"
        />
      </div>

      {error && <div role="alert" className="mx-4 rounded-md border border-lw-danger-line bg-lw-error-soft px-3 py-2 text-sm text-lw-danger sm:mx-6">{error}</div>}

      <div className="px-4 pb-6 sm:px-6">
        {rows === null ? (
          <LoadingState label="Loading the audit log…" />
        ) : folded.length === 0 ? (
          <p className="rounded-lg border border-dashed border-lw-line-strong p-8 text-center text-sm text-lw-text-2">Nothing recorded{filter.credential || filter.event || filter.task ? " for these filters" : " yet"}.</p>
        ) : (
          <table aria-label="Audit log" className="w-full border-separate border-spacing-0 text-sm">
            <thead className="hidden md:table-header-group">
              <tr className="text-left text-xs text-lw-text-2">
                {["When", "Agent", "Credential", "Event", "Operation", "Resource", "Decision"].map((h) => (
                  <th key={h} className="border-b border-lw-line px-2 py-2 font-medium">{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {folded.map(({ first, last, count }) => (
                <tr key={first.seq} className="mb-2 grid grid-cols-2 gap-x-3 gap-y-1 rounded-lg border border-lw-line p-3 md:mb-0 md:table-row md:rounded-none md:border-0 md:p-0">
                  <Cell className="text-lw-text-2"><When first={first} last={last} /></Cell>
                  <Cell>
                    {first.task_id ? (
                      <Link to={`/agents?task=${encodeURIComponent(first.task_id)}`} className="font-lw-mono text-[13px] text-lw-link hover:underline">{first.task_id}</Link>
                    ) : <span className="text-lw-faint">—</span>}
                    {first.task_title && <span className="block truncate text-xs text-lw-text-2">{first.task_title}</span>}
                    {first.agent_kind && <span className="block text-[11px] text-lw-faint">{agentKindLabel(first.agent_kind)}</span>}
                  </Cell>
                  <Cell className="font-lw-mono text-[13px]">{first.credential ?? "—"}</Cell>
                  <Cell>
                    <span className={`rounded px-1.5 py-0.5 text-xs font-medium ${eventTone[first.event] ?? "bg-lw-line text-lw-text-2"}`}>{first.event}</span>
                    {count > 1 && <span className="ml-1.5 text-xs text-lw-text-2">×{count}</span>}
                  </Cell>
                  <Cell className="font-lw-mono text-[13px]">{first.operation ?? "—"}</Cell>
                  <Cell className="col-span-2 break-all font-lw-mono text-[13px] text-lw-text-2">{first.resource ?? "—"}</Cell>
                  <Cell className="col-span-2 text-xs text-lw-text-2">
                    {[first.decided_by && `by ${first.decided_by}`, first.approval_id, first.amount, first.detail].filter(Boolean).join(" · ") || "—"}
                  </Cell>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {more && (
          <button type="button" disabled={loadingOlder} onClick={() => void loadOlder()} className="mt-3 h-9 w-full rounded-md border border-lw-line-strong text-[13px] text-lw-text-2 hover:text-lw-text disabled:opacity-40">
            {loadingOlder ? "Loading…" : "Load older"}
          </button>
        )}
      </div>
    </div>
  )
}

// StandingGrants lists what the person allowed and an agent may still do
// without asking (for its session, for a while, or once), and ends any of
// them. Grants a rule gives without asking are left out: they would be
// given again at the next use.
function StandingGrants({ baseUrl, onRevoked }: { baseUrl: string; onRevoked: () => void }) {
  const [grants, setGrants] = useState<CredentialGrant[] | null>(null)
  const [error, setError] = useState("")
  const [ending, setEnding] = useState("")

  const load = useCallback(async () => {
    try {
      setGrants((await listCredentialGrants(baseUrl)).filter((g) => g.mode !== "auto"))
      setError("")
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }, [baseUrl])

  useEffect(() => {
    void load()
    const timer = window.setInterval(() => void load(), REFRESH_MS)
    return () => window.clearInterval(timer)
  }, [load])

  const revoke = async (grant: CredentialGrant) => {
    setEnding(grant.id)
    try {
      await endCredentialGrant(baseUrl, grant.id)
      await load()
      onRevoked()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setEnding("")
    }
  }

  return (
    <section aria-label="Standing grants" className="border-b border-lw-line px-4 py-3 sm:px-6">
      <h2 className="text-sm font-medium">Standing grants</h2>
      <p className="mb-2 text-xs text-lw-text-2">What you allowed that an agent may still do without asking. Revoke one and the agent must ask again.</p>
      {error && <p role="alert" className="mb-2 text-xs text-lw-danger">{error}</p>}
      {grants === null ? (
        <LoadingState size="inline" label="Loading grants…" />
      ) : grants.length === 0 ? (
        <p className="text-xs text-lw-faint">No standing grants.</p>
      ) : (
        <ul className="flex flex-col gap-1.5">
          {grants.map((grant) => (
            <li key={grant.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg border border-lw-line bg-lw-surface px-3 py-2 text-sm">
              <span className="min-w-0">
                <Link to={`/agents?task=${encodeURIComponent(grant.task_id)}`} className="font-lw-mono text-[13px] text-lw-link hover:underline">{grant.task_id}</Link>
                {grant.task_title && <span className="ml-2 text-xs text-lw-text-2">{grant.task_title}</span>}
              </span>
              <span className="min-w-0 flex-1 break-all font-lw-mono text-[13px]">
                {grant.credential}: {grant.operations.join(", ")}
                {grant.resources.length > 0 && <span className="text-lw-text-2"> on {grant.resources.join(", ")}</span>}
              </span>
              <span className="rounded bg-lw-accent/15 px-1.5 py-0.5 text-xs font-medium text-lw-accent">{grantLasts(grant)}</span>
              <button
                type="button"
                disabled={ending === grant.id}
                onClick={() => void revoke(grant)}
                className="h-8 rounded-md border border-lw-danger-line px-3 text-[13px] text-lw-danger hover:bg-lw-error-soft disabled:opacity-40"
              >
                Revoke
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

function Cell({ children, className = "" }: { children: React.ReactNode; className?: string }) {
  return <td className={`min-w-0 align-top md:border-b md:border-lw-line md:px-2 md:py-2 ${className}`}>{children}</td>
}

function Select({ label, value, onChange, options, all }: { label: string; value: string; onChange: (value: string) => void; options: string[]; all: string }) {
  return (
    <select
      aria-label={label}
      value={value}
      onChange={(event) => onChange(event.target.value)}
      className="h-9 rounded-md border border-lw-line-strong bg-lw-ground px-2 text-sm text-lw-text"
    >
      <option value="">{all}</option>
      {options.map((option) => <option key={option} value={option}>{option}</option>)}
    </select>
  )
}

// When a row happened: the time, the date when it is not today, and for a
// folded run the span from its oldest to its newest use.
function When({ first, last }: { first: AuditRow; last: AuditRow }) {
  const newest = new Date(first.at)
  const oldest = new Date(last.at)
  const time = (d: Date) => d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" })
  const today = newest.toDateString() === new Date().toDateString()
  return (
    <span title={first === last ? newest.toLocaleString() : `${oldest.toLocaleString()} to ${newest.toLocaleString()}`} className="tabular-nums text-[13px]">
      {!today && <span className="block text-xs">{newest.toLocaleDateString()}</span>}
      {first === last ? time(newest) : `${time(oldest)}–${time(newest)}`}
    </span>
  )
}

export default AuditPage
