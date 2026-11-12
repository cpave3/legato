import { useState } from "react"
import type { Approval } from "../lib/approvals"
import { storeReeveSecret } from "../lib/reeve"
import { reeveName } from "../lib/reeveName"

// SecretRequestForm is the Reeve's request for a secret: the person pastes
// the value here, it goes to Legato's secret store, and the Reeve gets
// only its name.
export function SecretRequestForm({ approval, baseUrl, onDone }: { approval: Approval; baseUrl: string; onDone: () => Promise<void> }) {
  const [value, setValue] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const name = String(approval.details.name ?? "")
  const save = async () => {
    setBusy(true)
    setError("")
    try {
      await storeReeveSecret(baseUrl, approval.id, value)
      setValue("")
      await onDone()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="mt-2 space-y-2 text-sm md:text-xs" data-testid="secret-request">
      {approval.summary && <p className="text-lw-text-2">{approval.summary}</p>}
      <label className="flex flex-col gap-1 text-lw-text-2">
        The value of <code>{name}</code>
        <input type="password" autoComplete="off" aria-label={`Value of ${name}`} value={value} onChange={(e) => setValue(e.target.value)}
          className="min-h-10 rounded-lg border border-lw-line bg-lw-ground px-2 text-base text-lw-text md:min-h-8 md:text-xs" />
      </label>
      <p className="text-lw-faint">Kept in Legato's secret store. {reeveName(approval.details.reeve_name).start} gets its name, never its value.</p>
      <button type="button" disabled={busy || value === ""} onClick={() => void save()}
        className="inline-flex min-h-10 items-center rounded-lg bg-lw-accent px-3 font-semibold text-white disabled:opacity-50 md:min-h-8">
        Store the secret
      </button>
      {error && <p role="alert" className="text-lw-error">{error}</p>}
    </div>
  )
}
