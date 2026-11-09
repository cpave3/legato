import { useCallback, useEffect, useMemo, useState } from "react"
import { Laptop, Plus, ServerCog } from "lucide-react"
import { SettingsCard, primaryButton, quietButton } from "./settings/SettingsUI"
import { PairDeviceDialog } from "./PairDeviceDialog"
import { listMains, listWorkers, pairWorker, removeWarning, removeWorker, removedText, unpairMain, setMainReeve, workerCandidates, type PairedMain, type Worker, type WorkerCandidate, type WorkerList } from "../lib/workers"

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

// WorkersSettings is the main's side: the workers it hands tasks to, Add a
// worker (the worker's address, the phrase it shows, this main's name on
// it), and Remove, which warns how many away tasks the worker holds. Add a
// worker also offers the servers this app (or browser) already knows: one
// chosen opens its own pairing request, after a confirm, and is paired as if
// its address and phrase had been typed.
export function WorkersSettings({ baseUrl }: { baseUrl: string }) {
  const [list, setList] = useState<WorkerList | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [draft, setDraft] = useState<{ address: string; phrase: string; name: string } | null>(null)
  const [pairing, setPairing] = useState(false)
  const [removing, setRemoving] = useState<Worker | null>(null)
  const [busy, setBusy] = useState(false)
  const source = useMemo(() => workerCandidates(baseUrl), [baseUrl])
  const [candidates, setCandidates] = useState<WorkerCandidate[]>([])
  const adding = draft !== null

  const load = useCallback(async () => {
    try {
      setList(await listWorkers(baseUrl))
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }, [baseUrl])
  useEffect(() => { void load() }, [load])

  useEffect(() => {
    if (!adding) return
    let live = true
    source.list().then((list) => { if (live) setCandidates(list) }, () => { if (live) setCandidates([]) })
    return () => { live = false }
  }, [adding, source])

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

  // pairKnown pairs a server this app knows: it opens the request there
  // (after the app's or the browser's confirm), and this main pairs with
  // the address and phrase it gives. On failure the form keeps the address.
  const pairKnown = async (candidate: WorkerCandidate) => {
    if (!draft) return
    const name = draft.name.trim() || list?.main_name || ""
    setPairing(true)
    setError(null)
    setNotice(null)
    try {
      const opened = await source.open(candidate, name)
      if (!opened) return
      const worker = await pairWorker(baseUrl, { address: opened.address, phrase: opened.phrase, name })
      setDraft(null)
      setNotice(`Paired with ${worker.name}. Tasks can now be handed off to it.`)
      await load()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
      setDraft((d) => (d ? { ...d, address: candidate.url } : d))
    } finally {
      setPairing(false)
    }
  }

  const remove = async (worker: Worker) => {
    setBusy(true)
    setError(null)
    setNotice(null)
    try {
      setNotice(removedText(await removeWorker(baseUrl, worker.instance_id)))
      setRemoving(null)
      await load()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setBusy(false)
    }
  }

  const workers = list?.workers ?? []
  const canPair = draft !== null && draft.address.trim() !== "" && draft.phrase.trim() !== "" && draft.name.trim() !== "" && !pairing
  return (
    <SettingsCard id="workers" icon={ServerCog} title="Workers" description="Other Legatos this one hands tasks to, to be worked on while this one sleeps. Each gave this Legato a token that can only hand off, pull and return.">
      <div className="flex flex-col gap-3">
        {error && <p role="alert" className={alertClass}>{error}</p>}
        {notice && <p role="status" className="text-sm text-lw-live">{notice}</p>}
        {list && workers.length === 0 && (
          <div className="flex flex-col gap-1.5 rounded-lg border border-lw-line px-3 py-2.5 text-xs text-lw-text-2">
            <p className="m-0 text-sm text-lw-text">No workers yet.</p>
            <p className="m-0">
              A worker is another Legato, such as an always-on VM, that works on tasks while this one sleeps or travels. Once one is
              paired, you can hand a task off to a worker from its card's menu or its task panel: you choose the purpose (make a
              change, or find out), the repository and the agent tool, and the worker starts a sandboxed agent on it at once.
            </p>
            <p className="m-0">
              While it is away, the task stays here, read-only, in an Away lane at the end of the board, and you can talk to its
              agent on the worker. To pair one, open Settings › Main instances on the worker, then choose Add a worker here.
            </p>
          </div>
        )}
        {workers.length > 0 && (
          <ul className={listClass}>
            {workers.map((w) => (
              <li key={w.instance_id} aria-label={w.name} className="flex flex-col gap-1 px-3 py-2.5">
                <div className="flex items-start gap-3">
                  <div className="flex min-w-0 flex-1 flex-wrap items-baseline gap-x-2">
                    <span className="text-sm font-medium text-lw-text">{w.name}</span>
                    <span className="truncate font-lw-mono text-xs text-lw-faint">{w.address}</span>
                  </div>
                  {removing?.instance_id !== w.instance_id && (
                    <button onClick={() => setRemoving(w)} aria-label={`Remove ${w.name}`} className="min-h-10 shrink-0 rounded-lg px-3 text-sm text-lw-danger hover:bg-lw-raised">
                      Remove
                    </button>
                  )}
                </div>
                <div className={metaClass}>
                  <span className="font-lw-mono">{w.instance_id}</span>
                  <span>{w.last_pull_at ? `Pulled ${when(w.last_pull_at)}` : "Not pulled yet"}</span>
                  {w.refused_at ? (
                    <span className="text-lw-error">No longer accepts this main; pair again</span>
                  ) : w.unreachable_since ? (
                    <span className="text-lw-error">Unreachable since {when(w.unreachable_since)}</span>
                  ) : null}
                  <span className={w.sandbox ? "" : "text-lw-needs"}>{w.sandbox ? "Sandbox on" : "Sandbox off: it refuses handoffs"}</span>
                  <span>This main is <span className="font-lw-mono">{w.main_name}</span> there</span>
                  <span>{w.away ?? 0} away</span>
                </div>
                {removing?.instance_id === w.instance_id && (
                  <div className="mt-1 flex flex-col gap-2 rounded-lg border border-lw-danger-line p-3">
                    <p className="text-xs text-lw-text-2">{removeWarning(w.name, w.away ?? 0)}</p>
                    <div className="flex justify-end gap-2">
                      <button onClick={() => setRemoving(null)} disabled={busy} className={quietButton}>Cancel</button>
                      <button onClick={() => void remove(w)} disabled={busy} className="inline-flex min-h-10 items-center justify-center rounded-lg border border-lw-danger-line bg-lw-error-soft px-3.5 text-sm font-medium text-lw-danger hover:opacity-90">
                        {busy ? "Removing…" : "Remove"}
                      </button>
                    </div>
                  </div>
                )}
              </li>
            ))}
          </ul>
        )}
        {draft ? (
          <form
            onSubmit={(e) => { e.preventDefault(); void pair() }}
            className="flex flex-col gap-3 rounded-lg border border-lw-line-strong p-3"
          >
            {candidates.length > 0 && (
              <div className="flex flex-col gap-1.5">
                <p id="worker-candidates" className="text-xs font-medium text-lw-text-2">{source.from === "app" ? "From this app" : "From this browser"}</p>
                <ul aria-labelledby="worker-candidates" className={listClass}>
                  {candidates.map((c) => (
                    <li key={c.key}>
                      <button
                        type="button"
                        onClick={() => void pairKnown(c)}
                        disabled={pairing}
                        className="flex min-h-10 w-full flex-wrap items-baseline gap-x-2 px-3 py-2 text-left hover:bg-lw-raised disabled:opacity-60"
                      >
                        <span className="text-sm font-medium text-lw-text">{c.name}</span>
                        <span className="truncate font-lw-mono text-xs text-lw-faint">{c.url}</span>
                      </button>
                    </li>
                  ))}
                </ul>
                <p className="text-xs text-lw-faint">Or enter another worker's address and phrase.</p>
              </div>
            )}
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

  const toggleReeve = async (main: PairedMain, enabled: boolean) => {
    setError(null)
    try {
      await setMainReeve(baseUrl, main.instance_id, enabled)
      await load()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }

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
                    <label className="flex items-center gap-2 text-xs text-lw-text-2">
                      <input type="checkbox" checked={Boolean(m.reeve)} onChange={(e) => void toggleReeve(m, e.target.checked)}
                        aria-label={`Let the Reeve on ${m.name} message this one`} />
                      Its Reeve may message this one's
                    </label>
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
                        ? `${m.name} has ${m.open_handoffs} open handoffs here. Unpairing revokes its token and closes them; their cards stay here as ordinary cards, agents still running, each with a note that ${m.name} was unpaired.`
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
