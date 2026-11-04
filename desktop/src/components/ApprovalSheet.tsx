import { useEffect, useRef, useState } from "react"
import { api, errorText, tintColor, type Option, type Request, type Server, type SheetOutcome, type Signed } from "../api"

// approvalId: the shell reads only the ID, and fetches the request itself.
// onClose says how it came out, which the server's page is told.
type Props = { server: Server; approvalId: string; onClose: (outcome: SheetOutcome) => void }

// What prepare says when the server knows a key and none is plugged in
// (NO_KEY_PLUGGED in src-tauri/src/app.rs): the sheet waits for one.
const NO_KEY_PLUGGED = "Plug in a key this server knows."

type SheetState =
  | { kind: "loading" }
  | { kind: "plug-in" }
  | { kind: "ready"; request: Request; touchId: boolean; busy?: boolean }
  | { kind: "signed"; request: Request; touchId: boolean; option: Option; out: Signed }
  | { kind: "denied" }
  | { kind: "failed"; message: string }

// The approval sheet, as the phone draws it: what is signed, from the request
// the app fetched and will sign (the page cannot change it), then the seal.
export function ApprovalSheet({ server, approvalId, onClose: closed }: Props) {
  const [state, setState] = useState<SheetState>({ kind: "loading" })
  const outcome = useRef<SheetOutcome>({ status: "cancelled" })
  const onClose = () => closed(outcome.current)

  // Fetches the request; while no key the server knows is plugged in, asks
  // again every second and a half, so plugging one in carries on.
  useEffect(() => {
    let stop = false
    let timer: number | undefined
    const attempt = () => {
      api.prepare(server.id, approvalId)
        .then((p) => { if (!stop) setState({ kind: "ready", request: p.request, touchId: p.with === "touch-id" }) })
        .catch((e) => {
          if (stop) return
          const message = errorText(e)
          if (message.startsWith(NO_KEY_PLUGGED)) {
            setState({ kind: "plug-in" })
            timer = window.setTimeout(attempt, 1500)
          } else {
            fail(message)
          }
        })
    }
    attempt()
    return () => { stop = true; window.clearTimeout(timer) }
  }, [server.id, approvalId])

  const fail = (message: string) => {
    outcome.current = { status: "error", error: message }
    setState({ kind: "failed", message })
  }

  const busy = state.kind === "ready" && state.busy
  const sign = async (option: Option) => {
    if (state.kind !== "ready") return
    const { request, touchId } = state
    setState({ kind: "ready", request, touchId, busy: true })
    try {
      const out = await api.sign(server.id, approvalId, option.id)
      outcome.current = { status: "decided", option: option.id }
      setState({ kind: "signed", request, touchId, option, out })
    } catch (e) {
      fail(errorText(e))
    }
  }
  const deny = async () => {
    try {
      await api.deny(server.id, approvalId)
      outcome.current = { status: "decided" }
      setState({ kind: "denied" })
    } catch (e) { fail(errorText(e)) }
  }

  return (
    <div className="scrim" onClick={() => !busy && onClose()}>
      <div className="sheet" onClick={(e) => e.stopPropagation()}>
        <header className="sheet-head">
          <span className="glyph">◈</span><span className="kicker">SIGNED APPROVAL</span>
          <span className="spacer" />
          <span className="tint-dot" style={{ background: tintColor(server.tint) }} />
          <span className="mono dim">{server.name}</span>
        </header>
        {state.kind === "loading" && <div className="center"><div className="spinner" /></div>}
        {state.kind === "plug-in" && <PlugIn onClose={onClose} />}
        {state.kind === "ready" && <RequestView request={state.request} touchId={state.touchId} busy={!!state.busy} onSign={sign} onDeny={deny} />}
        {state.kind === "signed" && <SealView request={state.request} touchId={state.touchId} option={state.option} out={state.out} onClose={onClose} />}
        {state.kind === "denied" && <Ended title="Denied" message="The agent is told you said no." onClose={onClose} />}
        {state.kind === "failed" && <Ended title="Not signed" message={state.message} error onClose={onClose} />}
      </div>
    </div>
  )
}

function RequestView({ request, touchId, busy, onSign, onDeny }: { request: Request; touchId: boolean; busy: boolean; onSign: (o: Option) => void; onDeny: () => void }) {
  const d = request.details ?? {}
  const allows = (request.options ?? []).filter((o) => o.kind === "allow")
  const asker = [d.agent_kind, d.task_title].filter(Boolean).join(" · ")
  const lines: [string, string, string?][] = []
  if (d.credential) lines.push(["credential", d.credential])
  if (d.operations?.length) lines.push(["operation", d.operations.join(", "), "link"])
  for (const r of d.resources ?? []) lines.push(["resource", r, "text"])
  if (!d.credential && request.summary) lines.push(["request", request.summary, "text"])
  lines.push(["source", request.source])
  lines.push(["server", request.server])
  return (
    <>
      <h2 className="sheet-title">{request.title}</h2>
      {asker && <p className="asker">{asker}</p>}
      <div className="scope">
        {lines.map(([k, v, tone]) => (
          <div className="scope-line" key={k + v}><span className="k">{k}</span><span className={"v " + (tone ?? "")}>{v}</span></div>
        ))}
      </div>
      {d.reason && <div className="reason"><div className="kicker small">AGENT'S REASON</div><blockquote>“{d.reason}”</blockquote></div>}
      <div className="actions">
        {allows.map((o, i) => (
          <button key={o.id} className={"big " + (i === 0 ? "primary" : "")} disabled={busy} onClick={() => onSign(o)}>
            {i === 0 && <span className="dot">◉</span>}{o.label}
          </button>
        ))}
        <button className="big danger" disabled={busy} onClick={onDeny}>Deny</button>
      </div>
      {busy ? <TouchPrompt touchId={touchId} /> : (
        <p className="foot">
          {touchId ? "Signs with Touch ID · the key never leaves this Mac's Secure Enclave" : "Signs with a touch of your security key · the key never leaves it"}
        </p>
      )}
    </>
  )
}

// Waiting for a key: the sheet carries on by itself when one is plugged in.
function PlugIn({ onClose }: { onClose: () => void }) {
  return (
    <>
      <h2 className="sheet-title dim">Plug in your key</h2>
      <p className="asker">This server knows a key that is not plugged in. Plug it in, and the request shows here.</p>
      <div className="touch">
        <svg viewBox="0 0 120 64" className="key plugging" aria-hidden="true">
          <rect x="6" y="14" width="78" height="36" rx="9" className="key-body" />
          <rect x="84" y="22" width="22" height="20" rx="4" className="key-plug" />
          <circle cx="38" cy="32" r="11" className="key-pad idle" />
        </svg>
        <span>Waiting for a key…</span>
      </div>
      <button className="big" onClick={onClose}>Close</button>
    </>
  )
}

function TouchPrompt({ touchId }: { touchId: boolean }) {
  if (touchId) {
    return (
      <div className="touch">
        <svg viewBox="0 0 64 64" className="fingerprint" aria-hidden="true">
          <circle cx="32" cy="32" r="14" className="fp-ring" />
          <path
            className="fp-lines"
            d="M18 34c0-8 6-14 14-14s14 6 14 14M23 42c-1-3-1-5-1-8 0-6 4-10 10-10s10 4 10 10c0 4-1 8-2 11M28 46c-1-4-2-8-2-12 0-3 3-6 6-6s6 3 6 6c0 5-1 9-3 13M32 34c0 6-1 11-3 15"
          />
        </svg>
        <span>Use Touch ID</span>
      </div>
    )
  }
  return (
    <div className="touch">
      <svg viewBox="0 0 120 64" className="key" aria-hidden="true">
        <rect x="6" y="14" width="78" height="36" rx="9" className="key-body" />
        <rect x="84" y="22" width="22" height="20" rx="4" className="key-plug" />
        <circle cx="38" cy="32" r="11" className="key-pad" />
        <circle cx="38" cy="32" r="11" className="key-ring" />
      </svg>
      <span>Touch your key</span>
    </div>
  )
}

const ACCENT = [0x6d, 0x5e, 0xf6]
const GREEN = [0x3e, 0xcf, 0x8e]
const mix = (a: number[], b: number[], t: number) => `rgb(${a.map((v, i) => Math.round(v + (b[i] - v) * t)).join(",")})`

// The reward for signing, as on the phone: a dial of ticks lights up as the
// sweep passes, the arc runs violet to green, the hexagon seal fills, a
// check lands, the readout types out; then Close counts down 5 seconds.
function SealView({ request, touchId, option, out, onClose }: { request: Request; touchId: boolean; option: Option; out: Signed; onClose: () => void }) {
  const [t, setT] = useState(0)
  const [typed, setTyped] = useState(0)
  const readout = [
    `option   ${option.label}`,
    `key      ${out.device}`,
    touchId ? `touch id fingerprint ✓` : `touch    present ✓   pin  ${out.verified ? "verified" : "not asked"}`,
    `counter  ${out.counter}`,
    `sha256   ${out.message_sha256.slice(0, 24)}…`,
    `sig      ${out.signature.slice(0, 24)}…`,
    `server   ${request.server}`,
  ]
  const total = readout.reduce((n, l) => n + l.length + 1, 0)

  useEffect(() => {
    let raf = 0
    let start: number | undefined
    const step = (now: number) => {
      start ??= now
      const p = Math.min(1, (now - start) / 900)
      setT(1 - Math.pow(1 - p, 3))
      if (p < 1) raf = requestAnimationFrame(step)
    }
    raf = requestAnimationFrame(step)
    return () => cancelAnimationFrame(raf)
  }, [])
  useEffect(() => {
    if (t < 1 || typed >= total) return
    const id = setTimeout(() => setTyped((n) => n + 3), 12)
    return () => clearTimeout(id)
  }, [t, typed, total])

  const R = 98, c = 98
  const arcR = R * 0.72, circ = 2 * Math.PI * arcR
  const hexagon = Array.from({ length: 6 }, (_, k) => {
    const a = (k * Math.PI) / 3 - Math.PI / 2
    return `${c + R * 0.5 * Math.cos(a)},${c + R * 0.5 * Math.sin(a)}`
  }).join(" ")
  const done = t >= 1
  let left = typed

  return (
    <div className="seal">
      <div className="dial">
        <svg viewBox="0 0 196 196" width="196" height="196">
          <defs>
            <radialGradient id="glow"><stop offset="0" stopColor="#6D5EF6" stopOpacity="0.5" /><stop offset="1" stopColor="#6D5EF6" stopOpacity="0" /></radialGradient>
            <linearGradient id="sweep" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stopColor="#6D5EF6" /><stop offset="0.5" stopColor="#8A6CFF" /><stop offset="1" stopColor="#3ECF8E" /></linearGradient>
          </defs>
          <circle cx={c} cy={c} r={R} fill="url(#glow)" className="pulse" />
          {Array.from({ length: 60 }, (_, i) => {
            const a = (i / 60) * 2 * Math.PI - Math.PI / 2
            const long = i % 5 === 0
            const inner = R * (long ? 0.8 : 0.84), outer = R * 0.9
            return <line key={i} x1={c + inner * Math.cos(a)} y1={c + inner * Math.sin(a)} x2={c + outer * Math.cos(a)} y2={c + outer * Math.sin(a)}
              stroke={i / 60 <= t ? mix(ACCENT, GREEN, i / 60) : "#26262D"} strokeWidth={long ? 3 : 2} strokeLinecap="round" />
          })}
          <circle cx={c} cy={c} r={arcR} fill="none" stroke="url(#sweep)" strokeWidth="6" strokeLinecap="round"
            strokeDasharray={circ} strokeDashoffset={circ * (1 - t)} transform={`rotate(-90 ${c} ${c})`} />
          <polygon points={hexagon} fill="#3ECF8E" fillOpacity={done ? 0.16 : 0} stroke={done ? "#3ECF8E" : "#31313A"} strokeWidth="3" className="hex" />
        </svg>
        <span className={"check " + (done ? "in" : "")}>✓</span>
      </div>
      <div className="signed-word">SIGNED</div>
      <div className="signed-title">{request.title}</div>
      <pre className="readout">
        {readout.map((line) => {
          const shown = line.slice(0, Math.max(0, left))
          left -= line.length + 1
          return <div key={line}>{shown || " "}</div>
        })}
      </pre>
      <button className="big countdown" onClick={onClose}>
        Close
        {typed >= total && <span className="countdown-bar" onAnimationEnd={onClose} />}
      </button>
    </div>
  )
}

function Ended({ title, message, error, onClose }: { title: string; message: string; error?: boolean; onClose: () => void }) {
  return (
    <>
      <h2 className={"sheet-title " + (error ? "err" : "dim")}>{title}</h2>
      <p className="asker">{message}</p>
      <button className="big" onClick={onClose}>Close</button>
    </>
  )
}
