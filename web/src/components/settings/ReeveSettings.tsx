import { useCallback, useEffect, useState } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { Crown, Link2 } from "lucide-react"
import { deleteReeve, endReeve, fetchReeve, listReevePeers, pairReevePeer, removeReevePeer, resumeReeve, startReeve, type ReevePeer, type ReeveSession } from "../../lib/reeve"
import { PairDeviceDialog } from "../PairDeviceDialog"
import { queryKeys } from "../../lib/query"
import { SettingsCard, primaryButton, quietButton } from "./SettingsUI"
import { useReeveName } from "../../lib/reeveName"

const stateLabel: Record<ReeveSession["state"], string> = { running: "Running", suspended: "Suspended", ended: "Ended" }

// ReeveSessions starts the Reeve (with an optional override of its agent
// tool and model), ends its session, and lists its history.
export function ReeveSessions({ baseUrl }: { baseUrl: string }) {
  const queryClient = useQueryClient()
  const overview = useQuery({ queryKey: queryKeys.reeve(baseUrl), queryFn: () => fetchReeve(baseUrl), retry: false })
  const reeve = useReeveName()
  const [agent, setAgent] = useState("")
  const [model, setModel] = useState("")
  const [busy, setBusy] = useState("")
  const [error, setError] = useState("")
  const run = async (what: string, action: () => Promise<unknown>) => {
    setBusy(what)
    setError("")
    try {
      await action()
      await queryClient.invalidateQueries({ queryKey: queryKeys.reeve(baseUrl) })
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setBusy("")
    }
  }
  if (overview.isError) {
    return <SettingsCard icon={Crown} title="The Reeve" description="This server has no Reeve: it is older, or runs without tmux." />
  }
  const data = overview.data
  if (!data) return null
  const current = data.current
  const past = data.history.filter((s) => s.task_id !== current?.task_id)
  return (
    <SettingsCard icon={Crown} title={reeve.start}
      description={<>The agent of this Legato itself. It runs in Legato's sandbox, in <code>{data.folder}</code>, and reads freely; every change it proposes comes to you to approve.</>}>
      {current ? (
        <div className="flex flex-wrap items-center gap-2" data-testid="reeve-current">
          <span className="text-sm text-lw-text">{stateLabel[current.state]} · {current.agent_kind || "default tool"}{current.model ? ` · ${current.model}` : ""}</span>
          <button type="button" className={quietButton} disabled={busy !== ""} onClick={() => void run("end", () => endReeve(baseUrl, current.task_id))}>End session</button>
        </div>
      ) : (
        <div className="flex flex-wrap items-end gap-2">
          <label className="flex flex-col gap-1 text-xs text-lw-text-2">Agent tool (this session)
            <input aria-label="Agent tool for this session" placeholder="the default" value={agent} onChange={(e) => setAgent(e.target.value)}
              className="min-h-9 w-40 rounded-lg border border-lw-line bg-lw-ground px-2 text-sm text-lw-text" />
          </label>
          <label className="flex flex-col gap-1 text-xs text-lw-text-2">Model (this session)
            <input aria-label="Model for this session" placeholder="the default" value={model} onChange={(e) => setModel(e.target.value)}
              className="min-h-9 w-40 rounded-lg border border-lw-line bg-lw-ground px-2 text-sm text-lw-text" />
          </label>
          <button type="button" className={primaryButton} disabled={busy !== ""}
            onClick={() => void run("start", () => startReeve(baseUrl, { agent: agent.trim() || undefined, model: model.trim() || undefined }))}>
            Start {reeve.mid}
          </button>
        </div>
      )}
      {error && <p role="alert" className="mt-2 text-xs text-lw-error">{error}</p>}
      {past.length > 0 && (
        <section className="mt-4 border-t border-lw-line pt-3" aria-label={`${reeve.start}'s history`}>
          <h3 className="text-sm text-lw-text">History</h3>
          <ul className="mt-1 flex flex-col divide-y divide-lw-line">
            {past.map((s) => (
              <li key={s.task_id} className="flex flex-wrap items-center gap-2 py-2 text-sm" data-testid={`reeve-session-${s.task_id}`}>
                <span className="text-lw-text">{new Date(s.created_at).toLocaleString()}</span>
                <span className="text-xs text-lw-text-2">{s.agent_kind || "default tool"} · {stateLabel[s.state]}</span>
                <span className="ml-auto flex gap-1">
                  <button type="button" className={quietButton} disabled={busy !== "" || Boolean(current)}
                    onClick={() => void run(`resume:${s.task_id}`, () => resumeReeve(baseUrl, s.task_id))}>Resume</button>
                  <button type="button" className={quietButton} disabled={busy !== ""}
                    onClick={() => void run(`delete:${s.task_id}`, () => deleteReeve(baseUrl, s.task_id))}>Delete</button>
                </span>
              </li>
            ))}
          </ul>
        </section>
      )}
    </SettingsCard>
  )
}

const viaLabel: Record<ReevePeer["via"], string> = { worker: "Worker", main: "Main instance", pairing: "Paired Reeve", prime: "Paired Reeve (answers when it calls)" }

// ReevePairings lists the instances whose Reeves this one can message — its
// workers and mains, and instances paired here — and pairs or removes one.
export function ReevePairings({ baseUrl }: { baseUrl: string }) {
  const [peers, setPeers] = useState<ReevePeer[] | null>(null)
  const [address, setAddress] = useState("")
  const [phrase, setPhrase] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [showPair, setShowPair] = useState(false)
  const load = useCallback(async () => {
    try {
      setPeers(await listReevePeers(baseUrl))
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }, [baseUrl])
  useEffect(() => { void load() }, [load])
  const act = async (action: () => Promise<unknown>) => {
    setBusy(true)
    setError("")
    try {
      await action()
      await load()
      return true
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
      return false
    } finally {
      setBusy(false)
    }
  }
  const pair = async () => {
    if (await act(() => pairReevePeer(baseUrl, { address: address.trim(), phrase: phrase.trim() }))) {
      setAddress("")
      setPhrase("")
    }
  }
  return (
    <SettingsCard id="reeve-pairings" icon={Link2} title="Reeve pairings"
      description="Instances whose Reeves this one can message with message_reeve: its workers and mains, and instances paired here. A Reeve only asks another; each instance's own person approves what its Reeve changes.">
      <div className="flex flex-col gap-3">
        {peers && peers.length === 0 && <p className="text-xs text-lw-faint">No other instance is paired with this one.</p>}
        {peers && peers.length > 0 && (
          <ul className="flex flex-col divide-y divide-lw-line rounded-lg border border-lw-line">
            {peers.map((p) => (
              <li key={p.instance_id} data-testid={`reeve-peer-${p.instance_id}`} className="flex flex-wrap items-center gap-2 px-3 py-2 text-sm">
                <span className="text-lw-text">{p.name}</span>
                <span className="text-xs text-lw-text-2">{viaLabel[p.via] ?? p.via}</span>
                <span className="font-lw-mono text-xs text-lw-faint">{p.instance_id}</span>
                {(p.via === "pairing" || p.via === "prime") && (
                  <button type="button" className={quietButton + " ml-auto"} disabled={busy} aria-label={`Remove ${p.name}`}
                    onClick={() => void act(() => removeReevePeer(baseUrl, p.instance_id))}>Remove</button>
                )}
              </li>
            ))}
          </ul>
        )}
        <div className="flex flex-wrap items-end gap-2">
          <label className="flex flex-col gap-1 text-xs text-lw-text-2">Address
            <input aria-label="Address of the other instance" placeholder="https://nas.lan:3080" value={address} onChange={(e) => setAddress(e.target.value)}
              className="min-h-9 w-56 rounded-lg border border-lw-line bg-lw-ground px-2 text-sm text-lw-text" />
          </label>
          <label className="flex flex-col gap-1 text-xs text-lw-text-2">Phrase
            <input aria-label="Its phrase" placeholder="four words" value={phrase} onChange={(e) => setPhrase(e.target.value)}
              className="min-h-9 w-48 rounded-lg border border-lw-line bg-lw-ground px-2 text-sm text-lw-text" />
          </label>
          <button type="button" className={primaryButton} disabled={busy || !address.trim() || !phrase.trim()} onClick={() => void pair()}>Pair</button>
          <button type="button" className={quietButton} onClick={() => setShowPair(true)}>Let another instance pair with this one</button>
        </div>
        {error && <p role="alert" className="text-xs text-lw-error">{error}</p>}
      </div>
      {showPair && <PairDeviceDialog baseUrl={baseUrl} purpose="reeve" onClose={() => { setShowPair(false); void load() }} />}
    </SettingsCard>
  )
}
