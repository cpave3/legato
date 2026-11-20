import type { ReactNode } from "react"
import { Link } from "react-router-dom"
import { useCallback, useEffect, useRef, useState } from "react"
import { SettingsCard, quietButton } from "./settings/SettingsUI"
import { KeyRound } from "lucide-react"
import { endCredentialGrant, grantLasts, listCredentialAudit, listCredentialGrants, listCredentials, type Credential, type CredentialAuditEntry, type CredentialGrant } from "../lib/credentials"

// CredentialsSettings shows the credentials Legato holds for agents (named
// in config.yaml), what each agent session may do with them now, and the
// latest of what was asked and done. A grant can be ended early here.
// editors are the credentials' and sources' lists and the channels'
// settings, drawn inside this one card when Settings gives them.
export function CredentialsSettings({ baseUrl, editors }: { baseUrl: string; editors?: ReactNode }) {
  const [credentials, setCredentials] = useState<Credential[] | null>(null)
  const [grants, setGrants] = useState<CredentialGrant[]>([])
  const [audit, setAudit] = useState<CredentialAuditEntry[]>([])
  const [error, setError] = useState<string | null>(null)

  const request = useRef(0)
  const [loading, setLoading] = useState(true)
  const load = useCallback(async () => {
    const id = ++request.current
    setLoading(true)
    setError(null)
    try {
      const [c, g, a] = await Promise.all([listCredentials(baseUrl), listCredentialGrants(baseUrl), listCredentialAudit(baseUrl)])
      if (request.current !== id) return
      setCredentials(c)
      setGrants(g)
      setAudit(a)
    } catch (reason) {
      if (request.current !== id) return
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally { if (request.current === id) setLoading(false) }
  }, [baseUrl])

  useEffect(() => {
    const requests = request
    void load()
    return () => { requests.current++ }
  }, [load])

  const end = async (id: string) => {
    setError(null)
    try {
      await endCredentialGrant(baseUrl, id)
      await load()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }

  return (
    <SettingsCard id="credentials" label="Credentials" icon={KeyRound} title="Credentials" description="Secrets Legato holds for agents, their access rules, and current grants.">
      <div className="flex flex-col gap-3">
        {loading && <p role="status" className="text-sm text-lw-text-2">Loading credentials…</p>}
        {error && <div><p role="alert" className="text-sm text-lw-error">{error}{credentials ? " Showing last loaded credentials and grants." : ""}</p><button className={quietButton + " mt-2"} onClick={() => void load()}>Retry credentials</button></div>}
        {credentials && credentials.length === 0 && !error && (
          <p className="text-xs text-zinc-500">No credentials yet. Add one below; store its secret with legato secret add.</p>
        )}
        {credentials && credentials.length > 0 && (
          <ul className="divide-y divide-zinc-800 rounded border border-zinc-800">
            {credentials.map((c) => (
              <li key={c.name} className="flex flex-col gap-0.5 px-3 py-2">
                <div className="flex items-center gap-2">
                  <span className="text-sm text-zinc-200">{c.name}</span>
                  <span className="rounded bg-zinc-800 px-1.5 py-0.5 font-mono text-[10px] text-zinc-400">{c.profile}</span>
                </div>
                <span className="truncate font-mono text-xs text-zinc-500" title={c.secret}>{c.secret}</span>
                <span className="text-[11px] text-zinc-600">{c.rules.map((r) => `${(r.operation ?? ["any"]).join("/")}${r.resource ? " " + r.resource : ""}: ${r.approve}`).join(" · ")}</span>
              </li>
            ))}
          </ul>
        )}

        {grants.length > 0 && (
          <div>
            <h3 className="mb-1 text-xs font-medium text-zinc-400">Granted now</h3>
            <ul className="divide-y divide-zinc-800 rounded border border-zinc-800">
              {grants.map((g) => (
                <li key={g.id} className="flex items-center gap-2 px-3 py-2 text-xs">
                  <span className="min-w-0 flex-1">
                    <span className="text-zinc-200">{g.credential}</span>
                    <span className="text-zinc-500"> · {g.operations.join(", ")}{g.resources.length > 0 ? " on " + g.resources.join(", ") : ""} · {g.task_id}</span>
                    <span className="block text-zinc-500">{grantLasts(g)}</span>
                  </span>
                  <button type="button" aria-label={`End grant of ${g.credential} for ${g.task_id}`} onClick={() => void end(g.id)}
                    className="rounded px-2 py-1 text-zinc-400 hover:bg-zinc-800 hover:text-red-300">End</button>
                </li>
              ))}
            </ul>
          </div>
        )}

        {audit.length > 0 && (
          <details>
            <summary className="cursor-pointer text-xs font-medium text-zinc-400">Latest uses and decisions</summary>
            <ul className="mt-1 space-y-0.5 font-mono text-[11px] text-zinc-500">
              {audit.map((e) => (
                <li key={e.seq} className="flex gap-2">
                  <span className="shrink-0 text-zinc-600">{new Date(e.at).toLocaleTimeString()}</span>
                  <span className="shrink-0 text-zinc-300">{e.event}</span>
                  <span className="truncate">{[e.credential, e.operation, e.resource, e.task_id, e.approval_id].filter(Boolean).join(" · ")}</span>
                </li>
              ))}
            </ul>
          </details>
        )}
        {editors ?? <Link to="/settings#list-credentials" className={quietButton + " w-fit"}>Edit credentials</Link>}
        <Link to="/audit" className="text-xs text-lw-link hover:underline">Open the audit log →</Link>
      </div>
    </SettingsCard>
  )
}
