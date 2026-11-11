import { useState } from "react"
import { initials, tintColor, type Server, type Status } from "../api"
import { Reload } from "./Icons"

type Props = { server: Server; status?: Status; onRetry: () => Promise<void> }

// The app's own page for a server that does not answer, in place of the
// server's page (which the web view could only fill with its own error
// page). The app keeps asking, and the server's page comes back by itself.
export function OfflineScreen({ server, status, onRetry }: Props) {
  const [busy, setBusy] = useState(false)
  const reason = status && !status.online ? status.error : undefined

  const retry = async () => {
    setBusy(true)
    try { await onRetry() } finally { setBusy(false) }
  }

  return (
    <section className="offline" style={{ "--tint": tintColor(server.tint) } as React.CSSProperties}>
      <div className="offline-body">
        <span className="offline-tile">
          <span className="mini-tile big">{initials(server.name)}</span>
          <span className="dock-dot off" />
        </span>
        <h1>{server.name} is offline</h1>
        <p className="dim">
          The app cannot reach this server. Check that Legato is running on it and that this computer can reach its address.
        </p>
        <div className="scope">
          <div className="scope-line"><span className="k">address</span><span className="v text">{server.url}</span></div>
          <div className="scope-line"><span className="k">reason</span><span className="v">{reason || "Its page did not load."}</span></div>
        </div>
        <div className="offline-actions">
          <button className="btn primary" disabled={busy} onClick={retry}>
            <Reload size={14} /> {busy ? "Trying…" : "Try again"}
          </button>
          <span className="checking"><i /> checking every few seconds</span>
        </div>
        <p className="hint">Its page comes back by itself when the server answers.</p>
      </div>
    </section>
  )
}
