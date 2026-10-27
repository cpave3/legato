import { useCallback, useEffect, useState } from "react"
import { Check, Eye, X } from "lucide-react"
import { encode } from "uqr"
import { apiFetch } from "../lib/api"
import { useWebSocket } from "../hooks/useWebSocket"

// A pairing request of the server (GET/POST/DELETE /api/pairing): a phone in
// the Legato app pairs by typing its four words. See mobile/README.md § Pairing.
interface PairingStatus {
  state: "none" | "open" | "paired" | "expired" | "exhausted" | "cancelled"
  phrase?: string[]
  expires_at?: string
  attempts_left: number
  device?: string
  // The QR code the app scans: the address and this phrase, never the token.
  pair_uri?: string
}

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

export function PairDeviceDialog({ baseUrl, onClose }: { baseUrl: string; onClose: () => void }) {
  const { subscribe } = useWebSocket()
  const [status, setStatus] = useState<PairingStatus | null>(null)
  const [error, setError] = useState("")
  const [now, setNow] = useState(() => Date.now())

  const request = useCallback(
    async (method: "GET" | "POST" | "DELETE") => {
      try {
        const res = await apiFetch(baseUrl, "/api/pairing", { method })
        const body = await res.json().catch(() => ({}))
        if (res.status === 404) {
          setError("This server cannot pair by phrase. Update Legato on it.")
          return
        }
        if (!res.ok) {
          setError(body.error ?? `The server answered ${res.status}.`)
          return
        }
        setError("")
        setStatus(body as PairingStatus)
      } catch {
        setError("The server did not answer.")
      }
    },
    [baseUrl],
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

  // The countdown reaching zero is the server's to decide; read it again then.
  const expired = status?.state === "open" && status.expires_at !== undefined && Date.parse(status.expires_at) <= now
  useEffect(() => {
    if (expired) void request("GET")
  }, [expired, request])

  const close = useCallback(() => {
    if (status?.state === "open") void request("DELETE")
    onClose()
  }, [onClose, request, status?.state])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") close()
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [close])

  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center bg-black/60 sm:items-center sm:p-4" onClick={close}>
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="pair-device-title"
        onClick={(e) => e.stopPropagation()}
        className="flex w-full flex-col rounded-t-2xl border-t border-lw-line-strong bg-lw-surface font-lw text-[15px] leading-5 text-lw-text shadow-[0_24px_60px_rgba(0,0,0,.5)] sm:w-[480px] sm:rounded-xl sm:border sm:text-[13px]"
      >
        <div className="flex items-center justify-between gap-3 border-b border-lw-line px-5 py-3.5">
          <div>
            <h2 id="pair-device-title" className="text-base font-semibold sm:text-sm">Pair a device</h2>
            <p className="text-xs text-lw-text-2">In the Legato app, add a server, choose this one and type the words.</p>
          </div>
          <button
            onClick={close}
            aria-label="Close"
            className="flex h-11 w-11 items-center justify-center rounded-md text-lw-text-2 transition-colors hover:bg-lw-raised hover:text-lw-text sm:h-8 sm:w-8"
          >
            <X size={16} />
          </button>
        </div>

        <div className="flex flex-col gap-4 px-5 py-5">
          {error && (
            <div role="alert" className="rounded-md border border-lw-danger-line bg-lw-error-soft px-3 py-2 text-xs text-lw-danger">
              {error}
            </div>
          )}

          {!error && status?.state === "open" && status.phrase && (
            <>
              {status.pair_uri && (
                <div className="flex flex-col gap-2">
                  <HiddenQrCode key={status.pair_uri} text={status.pair_uri} />
                  <p className="text-center text-xs text-lw-text-2">Scan it in the Legato app, or type the words.</p>
                </div>
              )}
              <ol aria-label="Pairing phrase" className="grid grid-cols-2 gap-2">
                {status.phrase.map((word, i) => (
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
                Waiting for a device…
              </p>
            </>
          )}

          {!error && status?.state === "paired" && (
            <p className="flex items-center gap-2 text-sm text-lw-live">
              <Check size={16} />
              Paired with {status.device || "a device"}.
            </p>
          )}

          {!error && status && ended[status.state] && <p className="text-sm text-lw-text-2">{ended[status.state]}</p>}

          {!error && !status && <p className="text-sm text-lw-text-2">Asking the server for a phrase…</p>}
        </div>

        <div className="flex justify-end gap-2 border-t border-lw-line px-5 py-3">
          {status && ended[status.state] && (
            <button
              onClick={() => void request("POST")}
              className="h-11 rounded-md border border-lw-line-strong px-4 text-sm text-lw-text hover:bg-lw-raised sm:h-8 sm:px-3"
            >
              New phrase
            </button>
          )}
          <button onClick={close} className="h-11 rounded-md bg-lw-accent px-4 text-sm font-medium text-white hover:opacity-90 sm:h-8 sm:px-3">
            Done
          </button>
        </div>
      </div>
    </div>
  )
}
