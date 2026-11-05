import { useState } from "react"
import { api, errorText, tintColor, type ImportRequest, type Server, type Status } from "../api"
import { providerLabel } from "./importTarget"

type Props = {
  request: ImportRequest
  servers: Server[]
  statuses: Record<string, Status>
  current?: Server
  // A message the import ended with before the sheet opened (the server in
  // front refused it), shown at the top.
  failure?: string
  onImported: (server: Server, task: string) => void
  onClose: () => void
}

// Which server a ticket from the browser extension goes to. "Don't ask
// again" makes the server in front the answer from then on, which Servers &
// keys can turn back.
export function ImportSheet({ request, servers, statuses, current, failure, onImported, onClose }: Props) {
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState(failure ?? "")
  const [remember, setRemember] = useState(false)

  const pick = async (server: Server) => {
    setBusy(server.id)
    setError("")
    try {
      if (remember) await api.setImportPreference("current")
      const task = await api.importTicket(server.id, request.provider, request.key)
      onImported(server, task.id)
    } catch (e) {
      setError(`${server.name}: ${errorText(e)}`)
      setBusy(null)
    }
  }

  // The server in front first, then the rest as the dock lists them.
  const ordered = [...servers].sort((a, b) => Number(b.id === current?.id) - Number(a.id === current?.id))

  return (
    <div className="scrim" onClick={() => !busy && onClose()}>
      <div className="sheet" onClick={(e) => e.stopPropagation()}>
        <header className="sheet-head">
          <span className="glyph">↓</span><span className="kicker">IMPORT {providerLabel(request.provider).toUpperCase()} TICKET</span>
        </header>
        <h2 className="sheet-title">Import <span className="mono">{request.key}</span> into which server?</h2>
        {error && <p className="error">{error}</p>}
        <div className="import-servers">
          {ordered.map((s) => {
            const online = statuses[s.id]?.online
            return (
              <button key={s.id} className="btn import-server" disabled={!!busy} onClick={() => pick(s)}>
                <span className="tint-dot" style={{ background: tintColor(s.tint) }} />
                <span>{s.name}</span>
                {s.id === current?.id && <span className="dim small">in front</span>}
                <span className="spacer" />
                {online === false && <span className="dim small">not answering</span>}
                {busy === s.id && <span className="dim small">Importing…</span>}
              </button>
            )
          })}
        </div>
        <label className="row gap small">
          <input type="checkbox" checked={remember} disabled={!!busy} onChange={(e) => setRemember(e.target.checked)} />
          Don't ask again: import into the server in front
        </label>
        <div className="actions">
          <button className="btn" disabled={!!busy} onClick={onClose}>Cancel</button>
        </div>
      </div>
    </div>
  )
}
