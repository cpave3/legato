import { useCallback, useEffect, useState } from "react"
import { Laptop, Plus, ServerCog } from "lucide-react"
import { SettingsCard, primaryButton, quietButton } from "./settings/SettingsUI"
import { PairDeviceDialog } from "./PairDeviceDialog"
import { listMains, listWorkers, pairWorker, unpairMain, type PairedMain, type WorkerList } from "../lib/workers"

// Handoff: a main instance hands tasks to a worker instance it paired with
// by phrase. Settings › Workers is the main's side; Settings › Main
// instances the worker's. A Legato can be both.

function when(iso: string): string {
  const date = new Date(iso)
  return Number.isNaN(date.getTime()) ? iso : date.toLocaleString(undefined, { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" })
}

const inputClass = "min-h-10 rounded-lg border border-lw-line-strong bg-lw-raised px-3 text-sm text-lw-text outline-none placeholder:text-lw-faint focus:border-lw-accent"
const alertClass = "rounded-md border border-lw-danger-line bg-lw-error-soft px-3 py-2 text-xs text-lw-danger"
const listClass = "overflow-hidden rounded-lg border border-lw-line divide-y divide-lw-line"
const metaClass = "flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-0.5 text-xs text-lw-text-2"

// WorkersSettings is the main's side: the workers it hands tasks to, and
// Add a worker (the worker's address, the phrase it shows, this main's name
// on it).
export function WorkersSettings({ baseUrl }: { baseUrl: string }) {
  const [list, setList] = useState<WorkerList | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [draft, setDraft] = useState<{ address: string; phrase: string; name: string } | null>(null)
  const [pairing, setPairing] = useState(false)

  const load = useCallback(async () => {
    try {
      setList(await listWorkers(baseUrl))
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }, [baseUrl])
  useEffect(() => { void load() }, [load])

  const pair = async () => {
    if (!draft) return
    setPairing(true)
    setError(null)
    setNotice(null)
    try {
      const worker = await pairWorker(baseUrl, { address: draft.address.trim(), phrase: draft.phrase.trim(), name: draft.name.trim() })
      setDraft(null)
      setNotice(`Paired with ${worker.name}. Tasks can now be handed off to it.`)
      await load()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setPairing(false)
    }
  }

  const workers = list?.workers ?? []
  const canPair = draft !== null && draft.address.trim() !== "" && draft.phrase.trim() !== "" && draft.name.trim() !== "" && !pairing
  return (
    <SettingsCard id="workers" icon={ServerCog} title="Workers" description="Other Legatos this one hands tasks to, to be worked on while this one sleeps. Each gave this Legato a token that can only hand off, pull and return.">
      <div className="flex flex-col gap-3">
        {error && <p role="alert" className={alertClass}>{error}</p>}
        {notice && <p role="status" className="text-sm text-lw-live">{notice}</p>}
        {list && workers.length === 0 && <p className="text-xs text-lw-faint">No workers yet.</p>}
        {workers.length > 0 && (
          <ul className={listClass}>
            {workers.map((w) => (
              <li key={w.instance_id} aria-label={w.name} className="flex flex-col gap-1 px-3 py-2.5">
                <div className="flex flex-wrap items-baseline gap-x-2">
                  <span className="text-sm font-medium text-lw-text">{w.name}</span>
                  <span className="truncate font-lw-mono text-xs text-lw-faint">{w.address}</span>
                </div>
                <div className={metaClass}>
                  <span className="font-lw-mono">{w.instance_id}</span>
                  <span>{w.last_pull_at ? `Pulled ${when(w.last_pull_at)}` : "Not pulled yet"}</span>
                  <span className={w.sandbox ? "" : "text-lw-needs"}>{w.sandbox ? "Sandbox on" : "Sandbox off: it refuses handoffs"}</span>
                  <span>This main is <span className="font-lw-mono">{w.main_name}</span> there</span>
                </div>
              </li>
            ))}
          </ul>
        )}
        {draft ? (
          <form
            onSubmit={(e) => { e.preventDefault(); void pair() }}
            className="flex flex-col gap-3 rounded-lg border border-lw-line-strong p-3"
          >
            <p className="text-xs text-lw-text-2">
              On the worker, open Settings › Main instances › Pair a main instance (or run{" "}
              <code className="rounded bg-lw-raised px-1 font-lw-mono">legato pair --main</code>). It shows its address and a phrase. Enter them here.
            </p>
            <label className="flex flex-col gap-1 text-xs text-lw-text-2">
              Worker address
              <input value={draft.address} onChange={(e) => setDraft({ ...draft, address: e.target.value })} placeholder="https://vm-1.lan:3080" className={inputClass + " font-lw-mono"} />
            </label>
            <label className="flex flex-col gap-1 text-xs text-lw-text-2">
              Phrase
              <input value={draft.phrase} onChange={(e) => setDraft({ ...draft, phrase: e.target.value })} placeholder="four words from the worker" autoComplete="off" className={inputClass + " font-lw-mono"} />
            </label>
            <label className="flex flex-col gap-1 text-xs text-lw-text-2">
              This main's name on the worker
              <input value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} className={inputClass + " font-lw-mono"} />
            </label>
            <div className="flex justify-end gap-2">
              <button type="button" onClick={() => setDraft(null)} className={quietButton}>Cancel</button>
              <button type="submit" disabled={!canPair} className={primaryButton}>{pairing ? "Pairing…" : "Pair"}</button>
            </div>
          </form>
        ) : (
          <button onClick={() => { setNotice(null); setError(null); setDraft({ address: "", phrase: "", name: list?.main_name ?? "" }) }} className={quietButton + " w-fit"}>
            <Plus size={14} />
            Add a worker
          </button>
        )}
      </div>
    </SettingsCard>
  )
}

// MainInstancesSettings is the worker's side: the main instances that hand
// tasks to this Legato, Pair a main instance (a phrase for the main to
// type), and Unpair, which revokes the main's token.
export function MainInstancesSettings({ baseUrl }: { baseUrl: string }) {
  const [mains, setMains] = useState<PairedMain[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [confirming, setConfirming] = useState<PairedMain | null>(null)
  const [showPair, setShowPair] = useState(false)

  const load = useCallback(async () => {
    try {
      setMains(await listMains(baseUrl))
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }, [baseUrl])
  useEffect(() => { void load() }, [load])

  const unpair = async (main: PairedMain) => {
    setError(null)
    try {
      await unpairMain(baseUrl, main.instance_id)
      setConfirming(null)
      await load()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }

  return (
    <SettingsCard id="main-instances" icon={Laptop} title="Main instances" description="Other Legatos that hand tasks to this one. Each is known by its instance ID and holds a token that can only hand off, pull and return.">
      <div className="flex flex-col gap-3">
        {error && <p role="alert" className={alertClass}>{error}</p>}
        {mains && mains.length === 0 && <p className="text-xs text-lw-faint">No main instance is paired.</p>}
        {mains && mains.length > 0 && (
          <ul className={listClass}>
            {mains.map((m) => (
              <li key={m.instance_id} aria-label={m.name} className="flex flex-col gap-2 px-3 py-2.5">
                <div className="flex items-start gap-3">
                  <div className="flex min-w-0 flex-1 flex-col gap-1">
                    <span className="text-sm font-medium text-lw-text">{m.name}</span>
                    <div className={metaClass}>
                      <span className="font-lw-mono">{m.instance_id}</span>
                      <span>Paired {when(m.paired_at)}</span>
                      <span>{m.last_pull_at ? `Last pulled ${when(m.last_pull_at)}` : "Never pulled"}</span>
                      <span>{m.open_handoffs} open handoffs</span>
                    </div>
                  </div>
                  {confirming?.instance_id !== m.instance_id && (
                    <button onClick={() => setConfirming(m)} aria-label={`Unpair ${m.name}`} className="min-h-10 shrink-0 rounded-lg px-3 text-sm text-lw-danger hover:bg-lw-raised">
                      Unpair
                    </button>
                  )}
                </div>
                {confirming?.instance_id === m.instance_id && (
                  <div className="flex flex-col gap-2 rounded-lg border border-lw-danger-line p-3">
                    <p className="text-xs text-lw-text-2">
                      {m.open_handoffs > 0
                        ? `${m.name} has ${m.open_handoffs} open handoffs here. Unpairing revokes its token and closes them; their cards stay here as ordinary cards, agents still running.`
                        : `${m.name} has no open handoffs here. Unpairing revokes its token: it can no longer hand tasks to this Legato until it pairs again.`}
                    </p>
                    <div className="flex justify-end gap-2">
                      <button onClick={() => setConfirming(null)} className={quietButton}>Cancel</button>
                      <button onClick={() => void unpair(m)} className="inline-flex min-h-10 items-center justify-center rounded-lg border border-lw-danger-line bg-lw-error-soft px-3.5 text-sm font-medium text-lw-danger hover:opacity-90">
                        Unpair
                      </button>
                    </div>
                  </div>
                )}
              </li>
            ))}
          </ul>
        )}
        <button onClick={() => setShowPair(true)} className={primaryButton + " w-fit"}>
          <Laptop size={14} />
          Pair a main instance
        </button>
      </div>
      {showPair && <PairDeviceDialog baseUrl={baseUrl} purpose="main" onClose={() => { setShowPair(false); void load() }} />}
    </SettingsCard>
  )
}
