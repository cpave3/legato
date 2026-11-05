import { useCallback, useEffect, useState } from "react"
import { api, errorText, initials, tintColor, type Key, type Server, type Status, type TouchId } from "../api"
import { PairForm } from "./PairForm"
import { Fingerprint, KeyIcon, Lock, Plus, Servers } from "./Icons"
import { ImportTargetSection } from "./importTarget"
import { UpdatesSection, type Updates } from "./Updates"

type Props = {
  servers: Server[]
  statuses: Record<string, Status>
  startPairing: boolean
  updates: Updates
  onChanged: () => void
  onClose: () => void
}

// Servers and keys. A key is added once (a touch, and its PIN if it insists;
// on a Mac, Touch ID can be one) and enrolled on every server; revoking it
// here asks every server to drop it.
export function ServersPanel({ servers, statuses, startPairing, updates, onChanged, onClose }: Props) {
  const [pairing, setPairing] = useState(startPairing)
  const [keys, setKeys] = useState<Key[] | null>(null)
  const [error, setError] = useState("")
  const [note, setNote] = useState("")
  const [touchId, setTouchId] = useState<TouchId | null>(null)
  useEffect(() => { api.touchId().then(setTouchId).catch(() => {}) }, [])

  const loadKeys = useCallback(async () => {
    try { setKeys(await api.keys()) } catch (e) { setError(errorText(e)) }
  }, [])
  useEffect(() => {
    loadKeys()
    const id = setInterval(loadKeys, 4000)
    return () => clearInterval(id)
  }, [loadKeys])

  return (
    <section className="panel">
      <header className="panel-head">
        <h1>Servers &amp; keys</h1>
        <span className="spacer" />
        <button className="btn" onClick={onClose}>Done</button>
      </header>

      <div className="panel-section">
        <div className="section-head">
          <Servers /><h2>Servers</h2><span className="spacer" />
          {!pairing && <button className="btn small" onClick={() => setPairing(true)}><Plus size={14} /> Pair a server</button>}
        </div>
        {pairing && (
          <div className="card">
            <PairForm
              onPaired={(s) => { setPairing(false); setNote(`Paired with ${s.name}.`); onChanged(); loadKeys() }}
              onCancel={servers.length ? () => setPairing(false) : undefined}
            />
          </div>
        )}
        {servers.map((s) => <ServerRow key={s.id} server={s} status={statuses[s.id]} onChanged={onChanged} />)}
        {!servers.length && !pairing && <p className="dim">No servers yet.</p>}
      </div>

      <div className="panel-section">
        <div className="section-head">
          <KeyIcon /><h2>Keys</h2><span className="spacer" />
        </div>
        <p className="hint">
          A security key signs approvals with a touch{touchId?.mac ? ", and Touch ID with your fingerprint" : ""}. Each key is enrolled on every server, so any of them can sign anywhere, and revoking one here removes it everywhere.
        </p>
        {keys === null ? <p className="dim">Looking for keys…</p> : (
          <div className="keys">
            {keys.map((k) => <KeyRow key={k.id} keyItem={k} servers={servers} onChanged={() => { loadKeys(); onChanged() }} onNote={setNote} />)}
            {!keys.length && <p className="dim">No keys yet.</p>}
          </div>
        )}
        <div className="row gap">
          <AddKey onAdded={(msg) => { setNote(msg); loadKeys(); onChanged() }} />
          {touchId?.available && keys !== null && !keys.some((k) => k.kind === "touch-id") && (
            <AddTouchId onAdded={(msg) => { setNote(msg); loadKeys(); onChanged() }} />
          )}
        </div>
        {touchId?.mac && !touchId.available && <p className="dim small">Touch ID can't sign here: {touchId.reason}</p>}
      </div>

      <ImportTargetSection />
      <UpdatesSection updates={updates} />

      {note && <p className="note">{note}</p>}
      {error && <p className="error">{error}</p>}
    </section>
  )
}

function ServerRow({ server, status, onChanged }: { server: Server; status?: Status; onChanged: () => void }) {
  const [renaming, setRenaming] = useState(false)
  const [name, setName] = useState(server.name)
  const [confirm, setConfirm] = useState(false)
  return (
    <div className="server-row">
      <span className="mini-tile" style={{ "--tint": tintColor(server.tint) } as React.CSSProperties}>{initials(server.name)}</span>
      <div className="grow">
        {renaming ? (
          <form className="row gap" onSubmit={async (e) => { e.preventDefault(); await api.renameServer(server.id, name); setRenaming(false); onChanged() }}>
            <input value={name} onChange={(e) => setName(e.target.value)} autoFocus />
            <button className="btn small" type="submit">Save</button>
          </form>
        ) : (
          <div className="server-name">{server.name}</div>
        )}
        <div className="mono dim">
          {server.url}
          {server.pinned && <span className="pinned" title="Trusts only the certificate this server sealed when it paired"><Lock size={11} /> pinned</span>}
        </div>
      </div>
      <span className={"chip " + (status ? (status.online ? "ok" : "warn") : "")}>
        {status ? (status.online ? status.version ?? "online" : "offline") : "…"}
      </span>
      {!renaming && <button className="btn small" onClick={() => setRenaming(true)}>Rename</button>}
      {confirm ? (
        <>
          <button className="btn small danger" onClick={async () => { await api.removeServer(server.id); onChanged() }}>Forget it</button>
          <button className="btn small" onClick={() => setConfirm(false)}>Keep</button>
        </>
      ) : (
        <button className="btn small" onClick={() => setConfirm(true)}>Remove</button>
      )}
    </div>
  )
}

function KeyRow({ keyItem: k, servers, onChanged, onNote }: { keyItem: Key; servers: Server[]; onChanged: () => void; onNote: (s: string) => void }) {
  const [confirm, setConfirm] = useState(false)
  const [busy, setBusy] = useState(false)
  return (
    <div className="key-row">
      <div className="key-head">
        <span className={"led " + (k.plugged ? "on" : "")} title={keyState(k)} />
        <span className="key-name">{k.name}</span>
        <span className="dim small">{keyState(k)}</span>
        <span className="spacer" />
        {confirm ? (
          <>
            <span className="small">Each server will ask for a signature to remove it.</span>
            <button className="btn small danger" disabled={busy} onClick={async () => {
              setBusy(true)
              try {
                await api.revokeKey(k.id)
                onNote(`${k.name} is revoked. Sign each server's removal when it asks.`)
                onChanged()
              } catch (e) { onNote(errorText(e)) } finally { setBusy(false) }
            }}>Revoke everywhere</button>
            <button className="btn small" onClick={() => setConfirm(false)}>Keep</button>
          </>
        ) : (
          <button className="btn small" onClick={() => setConfirm(true)}>Revoke</button>
        )}
      </div>
      <div className="key-servers">
        {servers.map((s) => {
          const st = k.servers[s.id]
          return (
            <span key={s.id} className={"key-server " + (st ?? "none")}>
              <span className="tint-dot" style={{ background: tintColor(s.tint) }} />
              {s.name}
              <em>{st === "active" ? "enrolled" : st === "pending" ? "waiting for a signature" : "not enrolled"}</em>
              {!st && (
                <button className="link" onClick={async () => {
                  try { await api.enrollKey(k.id, s.id); onChanged() } catch (e) { onNote(errorText(e)) }
                }}>enrol</button>
              )}
            </span>
          )
        })}
      </div>
    </div>
  )
}

function keyState(k: Key) {
  if (k.kind === "touch-id") return k.plugged ? "Touch ID on this Mac" : "Touch ID is not available"
  return k.plugged ? "plugged in" : "not plugged in"
}

// What adding a key did on each server, in a sentence.
function added(name: string, servers: Record<string, { status?: string; error?: string }>) {
  const failed = Object.values(servers).filter((r) => r.error).length
  const waiting = Object.values(servers).filter((r) => r.status === "pending").length
  return (
    `${name} is added.` +
    (waiting ? ` ${waiting} server${waiting > 1 ? "s" : ""} will ask for a signature from a key they already know.` : "") +
    (failed ? ` ${failed} server${failed > 1 ? "s" : ""} could not be reached; enrol it there later.` : "")
  )
}

// Touch ID as a key: the Mac makes a key in its Secure Enclave that signs
// only after a fingerprint. Making it asks for nothing.
function AddTouchId({ onAdded }: { onAdded: (msg: string) => void }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  return (
    <>
      <button className="btn" disabled={busy} onClick={async () => {
        setBusy(true); setError("")
        try {
          const out = await api.addTouchId("Touch ID")
          onAdded(added("Touch ID", out.servers))
        } catch (e) { setError(errorText(e)) } finally { setBusy(false) }
      }}><Fingerprint size={14} /> Add Touch ID</button>
      {error && <p className="error">{error}</p>}
    </>
  )
}

function AddKey({ onAdded }: { onAdded: (msg: string) => void }) {
  const [open, setOpen] = useState(false)
  const [name, setName] = useState("")
  const [pin, setPin] = useState("")
  const [needPin, setNeedPin] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")

  const add = async () => {
    setBusy(true)
    setError("")
    try {
      const out = await api.addKey(name || "YubiKey", needPin ? pin : null)
      onAdded(added(name || "YubiKey", out.servers))
      setOpen(false); setName(""); setNeedPin(false)
    } catch (e) {
      const msg = errorText(e)
      if (/wants its PIN/.test(msg) && !needPin) {
        setNeedPin(true)
        setError("This key wants its PIN once, to make the credential. Signing will still be a touch.")
      } else {
        setError(msg)
      }
    } finally {
      setBusy(false); setPin("")
    }
  }

  if (!open) return <button className="btn" onClick={() => setOpen(true)}><Plus size={14} /> Add a security key</button>
  return (
    <div className="card">
      <p className="hint">Leave only the key to add plugged in. It blinks: touch it.</p>
      <div className="row gap">
        <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Name, such as YubiKey (desk)" autoFocus />
        {needPin && <input type="password" value={pin} onChange={(e) => setPin(e.target.value)} placeholder="FIDO2 PIN" autoComplete="off" />}
        <button className="btn primary" disabled={busy || (needPin && !pin)} onClick={add}>{busy ? "Touch the key…" : "Add"}</button>
        <button className="btn" disabled={busy} onClick={() => { setOpen(false); setError("") }}>Cancel</button>
      </div>
      {error && <p className="error">{error}</p>}
    </div>
  )
}
