import { useModalLayer } from "../ui/useModalLayer"
import { useCallback, useEffect, useRef, useState } from "react"
import { Check, Eye, X } from "lucide-react"
import { encode } from "uqr"
import { apiFetch } from "../../lib/api"
import { useWebSocket } from "../../hooks/useWebSocket"

// A pairing request of the server (GET/POST/DELETE /api/pairing): a phone in
// the Legato app pairs by typing its four words. See mobile/README.md § Pairing.
// A request for a main instance is typed into the main's Settings › Connections › Workers;
// the main gets a token that can only hand tasks off to this server.
export type PairingPurpose = "phone" | "main" | "reeve"

interface PairingStatus {
  state: "none" | "open" | "paired" | "expired" | "exhausted" | "cancelled"
  purpose?: PairingPurpose
  phrase?: string[]
  expires_at?: string
  attempts_left: number
  device?: string
  // The QR code the app scans: the address and this phrase, never the token.
  // A main instance's request has none.
  pair_uri?: string
  // A main instance's request: the address to type on the main.
  address?: string
}

const purposeName: Record<PairingPurpose, string> = { phone: "a phone", main: "a main instance", reeve: "another instance's Reeve" }

const ended: Record<string, string> = {
  expired: "The phrase expired before a device used it.",
  exhausted: "Too many wrong phrases were tried, so this one no longer works.",
  cancelled: "This phrase was cancelled.",
}

function remaining(expiresAt: string | undefined, now: number): string {
  if (!expiresAt) return ""
  const seconds = Math.max(0, Math.round((Date.parse(expiresAt) - now) / 1000))
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`
}

// A QR code drawn as one SVG path, dark on white whatever the theme, which is
// what phone cameras read best.
function QrCode({ text, label }: { text: string; label?: string }) {
  const { data, size } = encode(text, { ecc: "M", border: 2 })
  let d = ""
  data.forEach((row, y) =>
    row.forEach((dark, x) => {
      if (dark) d += `M${x} ${y}h1v1h-1z`
    }),
  )
  return (
    <svg
      viewBox={`0 0 ${size} ${size}`}
      shapeRendering="crispEdges"
      className="h-full w-full"
      {...(label ? { role: "img", "aria-label": label } : { "aria-hidden": true })}
    >
      <rect width={size} height={size} fill="#fff" />
      <path d={d} fill="#000" />
    </svg>
  )
}

// The QR code stays blurred until it is clicked: anyone who sees it in the
// next few minutes, on a shared screen too, can pair with it.
function HiddenQrCode({ text }: { text: string }) {
  const [shown, setShown] = useState(false)
  if (shown) {
    return (
      <div className="mx-auto h-44 w-44 overflow-hidden rounded-lg">
        <QrCode text={text} label="QR code to pair a phone" />
      </div>
    )
  }
  return (
    <button
      onClick={() => setShown(true)}
      aria-label="Show QR code"
      className="group relative mx-auto block h-44 w-44 overflow-hidden rounded-lg"
    >
      <div className="h-full w-full scale-110 blur-md">
        <QrCode text={text} />
      </div>
      <span className="absolute inset-0 flex flex-col items-center justify-center gap-1 bg-black/40 text-xs font-medium text-white transition-colors group-hover:bg-black/30">
        <Eye size={16} />
        Show QR code
      </span>
    </button>
  )
}

// The words for each purpose of a request: the dialog's title, where to type
// the phrase, and what an open phrase waits for.
const purposeText: Record<PairingPurpose, { title: string; how: string; waiting: string }> = {
  phone: {
    title: "Pair a device",
    how: "In the Legato app, add a server, choose this one and type the words.",
    waiting: "Waiting for a device…",
  },
  main: {
    title: "Pair a main instance",
    how: "On the main instance, open Settings › Connections › Workers › Add a worker and enter this address and phrase. The main gets a token that can only hand tasks off here.",
    waiting: "Waiting for a main instance…",
  },
  reeve: {
    title: "Pair another Reeve",
    how: "On the other instance, open Settings › Reeve › Reeve pairings and enter this address and phrase. Its Reeve can then message this one's, and only that: whatever either proposes, its own person approves.",
    waiting: "Waiting for the other instance…",
  },
}

function pairedMessage(status: PairingStatus): string {
  if (status.purpose === "main") return `Paired with main instance ${status.device}.`
  if (status.purpose === "reeve") return `Paired with the Reeve on ${status.device || "the other instance"}.`
  return `Paired with ${status.device || "a device"}.`
}

// A phone's request is opened with no body, as older servers expect.
function pairingInit(method: "GET" | "POST" | "DELETE", purpose: PairingPurpose): RequestInit {
  if (method === "POST" && purpose !== "phone") {
    return { method, headers: { "Content-Type": "application/json" }, body: JSON.stringify({ purpose }) }
  }
  return { method }
}

// pairingError is what to say of a refused request, or null for an answer.
function pairingError(res: Response, body: { error?: string }): string | null {
  if (res.status === 404) return "This server cannot pair by phrase. Update Legato on it."
  if (!res.ok) return body.error ?? `The server answered ${res.status}.`
  return null
}

// usePairing opens this dialog's request and follows it: on pairing_changed,
// and when its countdown runs out, which is the server's to decide.
function usePairing(baseUrl: string, purpose: PairingPurpose) {
  const { subscribe } = useWebSocket()
  const [status, setStatus] = useState<PairingStatus | null>(null)
  const [error, setError] = useState("")
  const [now, setNow] = useState(() => Date.now())

  const request = useCallback(
    async (method: "GET" | "POST" | "DELETE") => {
      try {
        const res = await apiFetch(baseUrl, "/api/pairing", pairingInit(method, purpose))
        const body = await res.json().catch(() => ({}))
        const refused = pairingError(res, body)
        if (refused !== null) {
          setError(refused)
          return
        }
        setError("")
        setStatus(body as PairingStatus)
      } catch {
        setError("The server did not answer.")
      }
    },
    [baseUrl, purpose],
  )

  useEffect(() => {
    void request("POST")
  }, [request])

  useEffect(
    () =>
      subscribe((msg) => {
        if (msg.type === "pairing_changed") void request("GET")
      }),
    [subscribe, request],
  )

  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [])

  const expired = status?.state === "open" && status.expires_at !== undefined && Date.parse(status.expires_at) <= now
  useEffect(() => {
    if (expired) void request("GET")
  }, [expired, request])

  return { status, error, request, now }
}

export function PairDeviceDialog({ baseUrl, purpose = "phone", onClose }: { baseUrl: string; purpose?: PairingPurpose; onClose: () => void }) {
  const { status, error, request, now } = usePairing(baseUrl, purpose)

  const close = useCallback(() => {
    if (status?.state === "open") void request("DELETE")
    onClose()
  }, [onClose, request, status?.state])

  const dialogRef = useRef<HTMLDivElement>(null)
  useModalLayer({ open: true, onClose: close, containerRef: dialogRef })

  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center bg-black/60 sm:items-center sm:p-4" onClick={close}>
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="pair-device-title"
        onClick={(e) => e.stopPropagation()}
        className="flex max-h-[calc(100dvh-1rem)] w-full flex-col overflow-hidden rounded-t-2xl border-t border-lw-line-strong bg-lw-surface font-lw text-[15px] leading-5 text-lw-text shadow-[0_24px_60px_rgba(0,0,0,.5)] sm:w-[480px] sm:rounded-xl sm:border sm:text-[13px]"
      >
        <div className="flex shrink-0 items-center justify-between gap-3 border-b border-lw-line px-5 py-3.5">
          <div>
            <h2 id="pair-device-title" className="text-base font-semibold sm:text-sm">{purposeText[purpose].title}</h2>
            <p className="text-xs text-lw-text-2">{purposeText[purpose].how}</p>
          </div>
          <button
            onClick={close}
            aria-label="Close"
            className="flex h-11 w-11 items-center justify-center rounded-md text-lw-text-2 transition-colors hover:bg-lw-raised hover:text-lw-text sm:h-8 sm:w-8"
          >
            <X size={16} />
          </button>
        </div>

        <div className="flex min-h-0 flex-col gap-4 overflow-y-auto overscroll-contain px-5 py-5">
          {error ? (
            <div role="alert" className="rounded-md border border-lw-danger-line bg-lw-error-soft px-3 py-2 text-xs text-lw-danger">
              {error}
            </div>
          ) : <PairingState status={status} purpose={purpose} now={now} />}
        </div>

        <div className="flex shrink-0 justify-end gap-2 border-t border-lw-line px-5 py-3">
          {status && ended[status.state] && (
            <button
              onClick={() => void request("POST")}
              className="h-11 rounded-md border border-lw-line-strong px-4 text-sm text-lw-text hover:bg-lw-raised sm:h-8 sm:px-3"
            >
              New phrase
            </button>
          )}
          <button onClick={close} className="h-11 rounded-md bg-lw-accent px-4 text-sm font-medium text-[var(--theme-canvas,#fff)] hover:opacity-90 sm:h-8 sm:px-3">
            Done
          </button>
        </div>
      </div>
    </div>
  )
}

// PairingState is how the request stands: asked for, open, paired or ended.
function PairingState({ status, purpose, now }: { status: PairingStatus | null; purpose: PairingPurpose; now: number }) {
  if (!status) return <p className="text-sm text-lw-text-2">Asking the server for a phrase…</p>
  // One request at a time: one opened elsewhere for something else replaces this one.
  const shownPurpose: PairingPurpose = status.purpose ?? "phone"
  const replaced = status.state === "open" && shownPurpose !== purpose
  return <>
    {replaced && (
      <p role="status" className="rounded-md border border-lw-line-strong bg-lw-ground px-3 py-2 text-xs text-lw-needs">
        Another request replaced this one: the phrase below now pairs {purposeName[shownPurpose]}.
      </p>
    )}

    {status.state === "open" && status.phrase && <OpenPhrase status={status} phrase={status.phrase} purpose={shownPurpose} now={now} />}

    {status.state === "paired" && (
      <p className="flex items-center gap-2 text-sm text-lw-live">
        <Check size={16} />
        {pairedMessage(status)}
      </p>
    )}

    {ended[status.state] && <p className="text-sm text-lw-text-2">{ended[status.state]}</p>}
  </>
}

// OpenPhrase is an open request's phrase, with where to type it (the address
// for an instance, the QR code for a phone), its countdown and tries left.
function OpenPhrase({ status, phrase, purpose, now }: { status: PairingStatus; phrase: string[]; purpose: PairingPurpose; now: number }) {
  return <>
    {purpose !== "phone" && status.address && (
      <div className="flex flex-col gap-1">
        <span className="text-xs text-lw-text-2">Address</span>
        <span className="rounded-lg border border-lw-line-strong bg-lw-ground px-3 py-2 font-lw-mono text-sm text-lw-text">{status.address}</span>
      </div>
    )}
    {purpose === "phone" && status.pair_uri && (
      <div className="flex flex-col gap-2">
        <HiddenQrCode key={status.pair_uri} text={status.pair_uri} />
        <p className="text-center text-xs text-lw-text-2">Scan it in the Legato app, or type the words.</p>
      </div>
    )}
    <ol aria-label="Pairing phrase" className="grid grid-cols-2 gap-2">
      {phrase.map((word, i) => (
        <li
          key={i}
          className="flex items-baseline gap-2 rounded-lg border border-lw-line-strong bg-lw-ground px-3 py-2.5"
        >
          <span className="font-lw-mono text-xs text-lw-faint">{i + 1}</span>
          <span className="font-lw-mono text-lg font-medium text-lw-text">{word}</span>
        </li>
      ))}
    </ol>
    <p className="flex justify-between text-xs text-lw-text-2">
      <span>Expires in {remaining(status.expires_at, now)}</span>
      {status.attempts_left < 5 && <span className="text-lw-needs">{status.attempts_left} tries left</span>}
    </p>
    <p className="flex items-center gap-2 text-xs text-lw-text-2">
      <span className="h-2 w-2 animate-pulse rounded-full bg-lw-accent" />
      {purposeText[purpose].waiting}
    </p>
  </>
}
