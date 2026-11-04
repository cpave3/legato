import { useEffect, useState } from "react"
import { getCurrentWindow } from "@tauri-apps/api/window"
import { tintColor, type Server, type Status } from "../api"
import { Close, Maximize, Minimize, Reload, Restore, Seal } from "./Icons"
import { UpdatePill, type Updates } from "./Updates"

type Props = {
  server?: Server
  status?: Status
  waiting: number
  updates: Updates
  onApprovals: () => void
  onReload: () => void
}

// The app's own title bar: drag it to move the window, double-click it to
// maximize. It names the server in front and counts what waits to be signed.
export function TitleBar({ server, status, waiting, updates, onApprovals, onReload }: Props) {
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
      <UpdatePill updates={updates} />
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
