import { useCallback, useEffect, useState } from "react"
import { api, errorText, type UpdateStatus } from "../api"

type Install = { kind: "idle" } | { kind: "installing" } | { kind: "installed"; version: string } | { kind: "failed"; message: string }

export type Updates = ReturnType<typeof useUpdates>

// Whether a newer release has this computer's build, checked at start, every
// 6 hours and when asked; and installing it (the AppImage, or Legato.app on
// a Mac, replaces itself, the old copy kept beside it), then restarting.
export function useUpdates() {
  const [status, setStatus] = useState<UpdateStatus | null>(null)
  const [checking, setChecking] = useState(false)
  const [checked, setChecked] = useState<Date | null>(null)
  const [error, setError] = useState("")
  const [install, setInstall] = useState<Install>({ kind: "idle" })

  const check = useCallback(async () => {
    setChecking(true)
    setError("")
    try {
      setStatus(await api.checkUpdate())
      setChecked(new Date())
    } catch (e) {
      setError(errorText(e))
    } finally {
      setChecking(false)
    }
  }, [])

  useEffect(() => {
    check()
    const id = setInterval(check, 6 * 60 * 60 * 1000)
    return () => clearInterval(id)
  }, [check])

  const apply = useCallback(async () => {
    setInstall({ kind: "installing" })
    try { setInstall({ kind: "installed", version: await api.applyUpdate() }) }
    catch (e) { setInstall({ kind: "failed", message: errorText(e) }) }
  }, [])

  return { status, checking, checked, error, install, check, apply, restart: () => api.restartApp() }
}

// The title bar's word on it: only when there is something newer.
export function UpdatePill({ updates }: { updates: Updates }) {
  const { status, install, apply, restart } = updates
  if (!status?.available || !status.latest) return null
  const latest = status.latest.version
  if (!status.can_install) {
    return <span className="chip" title={`${status.reason ?? ""} ${status.latest.page}`.trim()}>{latest} is out</span>
  }
  switch (install.kind) {
    case "installing":
      return <span className="update-pill busy">Installing {latest}…</span>
    case "installed":
      return <button className="update-pill" onClick={restart}>Restart to update</button>
    default:
      return (
        <button
          className={"update-pill" + (install.kind === "failed" ? " failed" : "")}
          title={install.kind === "failed" ? install.message : `You have ${status.version}`}
          onClick={apply}
        >
          {install.kind === "failed" ? "Update failed · retry" : `Update to ${latest}`}
        </button>
      )
  }
}

// The Servers panel's section on the app itself: its version, and Check for
// updates.
export function UpdatesSection({ updates }: { updates: Updates }) {
  const { status, checking, checked, error, install, check, apply, restart } = updates
  const dev = status && !status.version.startsWith("v")
  const latest = status?.latest

  let line: React.ReactNode = checking ? "Checking…" : null
  if (!checking && dev) line = "Built from source, so it does not update itself."
  else if (!checking && status && !status.available) line = `Up to date${checked ? `, checked at ${checked.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}` : ""}.`
  else if (!checking && status?.available && latest) line = `${latest.version} is out.`

  return (
    <div className="panel-section">
      <div className="section-head">
        <h2>This app</h2><span className="spacer" />
        <span className="mono dim">{status?.version ?? "…"}</span>
      </div>
      <div className="row gap wrap">
        <span className="dim small">{line}</span>
        <span className="spacer" />
        {status?.available && status.can_install && install.kind !== "installed" && (
          <button className="btn small primary" disabled={install.kind === "installing"} onClick={apply}>
            {install.kind === "installing" ? `Installing ${latest?.version}…` : `Update to ${latest?.version}`}
          </button>
        )}
        {install.kind === "installed" && <button className="btn small primary" onClick={restart}>Restart to update</button>}
        <button className="btn small" disabled={checking || !!dev} onClick={check}>Check for updates</button>
      </div>
      {status?.available && !status.can_install && latest && (
        <p className="hint">
          {status.reason} Or download it: <code>{latest.page}</code>
        </p>
      )}
      {install.kind === "failed" && <p className="error">{install.message}</p>}
      {error && <p className="error">Could not check: {error}</p>}
    </div>
  )
}
