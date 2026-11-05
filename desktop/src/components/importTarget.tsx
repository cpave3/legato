import { useEffect, useState } from "react"
import { api, errorText, type ImportTarget } from "../api"

const LABELS: Record<string, string> = { jira: "Jira", burnlite: "Burnlite", github: "GitHub" }

// A ticket provider's name for people; another one by its own name.
export function providerLabel(provider: string): string {
  return LABELS[provider] ?? provider
}

// Servers & keys: where tickets from the browser extension are imported.
export function ImportTargetSection() {
  const [target, setTarget] = useState<ImportTarget | null>(null)
  const [error, setError] = useState("")
  useEffect(() => { api.importPreference().then(setTarget).catch((e) => setError(errorText(e))) }, [])

  const change = (ask: boolean) => {
    const next: ImportTarget = ask ? "ask" : "current"
    setTarget(next)
    api.setImportPreference(next).catch((e) => setError(errorText(e)))
  }

  return (
    <div className="panel-section">
      <div className="section-head"><h2>Tickets from the browser</h2></div>
      <label className="row gap small">
        <input type="checkbox" checked={target === "ask"} disabled={target === null} onChange={(e) => change(e.target.checked)} />
        Ask which server to import a ticket into
      </label>
      <p className="hint">
        When the Legato browser extension is set to open imports in this app, a ticket you import from Jira or Burnlite
        comes here. {target === "current" ? "It goes to the server in front." : "You choose the server each time."}
      </p>
      {error && <p className="error">{error}</p>}
    </div>
  )
}
