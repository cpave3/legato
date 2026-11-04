import { useEffect, useState } from "react"
import { getCurrentWindow } from "@tauri-apps/api/window"
import { api, errorText, tintColor, type Server, type Status, type UpdateStatus } from "../api"
import { Close, Maximize, Minimize, Reload, Restore, Seal } from "./Icons"

type Props = {
  server?: Server
  status?: Status
  waiting: number
  onApprovals: () => void
  onReload: () => void
}

// The app's own title bar: drag it to move the window, double-click it to
// maximize. It names the server in front and counts what waits to be signed.
export function TitleBar({ server, status, waiting, onApprovals, onReload }: Props) {
  const win = getCurrentWindow()
  const [maximized, setMaximized] = useState(false)

  useEffect(() => {
    win.isMaximized().then(setMaximized)
    const off = win.onResized(() => win.isMaximized().then(setMaximized))
    return () => { off.then((f) => f()) }
  }, [win])

  return (
    <header className="titlebar" data-tauri-drag-region>
      <div className="titlebar-brand" data-tauri-drag-region>
        <img src="/mark.svg" alt="" className="titlebar-mark" />
        <span className="titlebar-word" data-tauri-drag-region>legato</span>
      </div>
      {server && (
        <div className="titlebar-server" data-tauri-drag-region>
          <span className="tint-dot" style={{ background: tintColor(server.tint) }} />
          <span className="titlebar-name" data-tauri-drag-region>{server.name}</span>
          <span className="titlebar-url" data-tauri-drag-region>{server.url.replace(/^https?:\/\//, "")}</span>
          {status && !status.online && <span className="chip warn">offline</span>}
          <button className="icon-btn" title="Reload this server's page" onClick={onReload}><Reload size={14} /></button>
        </div>
      )}
      <div className="spacer" data-tauri-drag-region />
      <UpdatePill />
      {waiting > 0 && (
        <button className="approvals-pill" onClick={onApprovals} title="Approvals waiting for your key">
          <Seal size={14} /> {waiting} to sign
        </button>
      )}
      <div className="window-controls">
        <button className="win-btn" title="Minimize" onClick={() => win.minimize()}><Minimize size={14} /></button>
        <button className="win-btn" title={maximized ? "Restore" : "Maximize"} onClick={() => win.toggleMaximize()}>
          {maximized ? <Restore size={13} /> : <Maximize size={13} />}
        </button>
        <button className="win-btn close" title="Close" onClick={() => win.close()}><Close size={14} /></button>
      </div>
    </header>
  )
}

type Update = { kind: "idle" } | { kind: "installing" } | { kind: "installed"; version: string } | { kind: "failed"; message: string }

// A newer desktop release: install it (the AppImage replaces itself, the old
// one kept as .prev), then restart into it. Checked at start and every 6 hours.
function UpdatePill() {
  const [status, setStatus] = useState<UpdateStatus | null>(null)
  const [update, setUpdate] = useState<Update>({ kind: "idle" })

  useEffect(() => {
    const check = () => api.checkUpdate().then(setStatus).catch(() => {})
    check()
    const id = setInterval(check, 6 * 60 * 60 * 1000)
    return () => clearInterval(id)
  }, [])

  if (!status?.available || !status.latest) return null
  const latest = status.latest.version
  if (!status.can_install) {
    return <span className="chip" title={`${latest} is out: ${status.latest.page}`}>{latest} is out</span>
  }
  switch (update.kind) {
    case "installing":
      return <span className="update-pill busy">Installing {latest}…</span>
    case "installed":
      return <button className="update-pill" onClick={() => api.restartApp()}>Restart to update</button>
    default:
      return (
        <button
          className={"update-pill" + (update.kind === "failed" ? " failed" : "")}
          title={update.kind === "failed" ? update.message : `You have ${status.version}`}
          onClick={async () => {
            setUpdate({ kind: "installing" })
            try { setUpdate({ kind: "installed", version: await api.applyUpdate() }) }
            catch (e) { setUpdate({ kind: "failed", message: errorText(e) }) }
          }}
        >
          {update.kind === "failed" ? "Update failed · retry" : `Update to ${latest}`}
        </button>
      )
  }
}
