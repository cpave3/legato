import { initials, tintColor, type Server, type Status } from "../api"
import { Inbox, KeyIcon, Plus } from "./Icons"

type Props = {
  servers: Server[]
  statuses: Record<string, Status>
  active?: string
  panel: "manage" | "approvals" | null
  onSelect: (id: string) => void
  onAdd: () => void
  onApprovals: () => void
  onManage: () => void
}

// The dock: each server as a tile in its tint, with a dot for whether it
// answers and a count of the approvals waiting there. At its foot, every
// server's approvals, and the servers and keys.
export function Dock({ servers, statuses, active, panel, onSelect, onAdd, onApprovals, onManage }: Props) {
  const panelOpen = panel !== null
  const waiting = servers.reduce((n, s) => n + (statuses[s.id]?.approvals.length ?? 0), 0)
  return (
    <nav className="dock" aria-label="Servers">
      <div className="dock-servers">
        {servers.map((s) => {
          const st = statuses[s.id]
          const waiting = st?.approvals.length ?? 0
          const color = tintColor(s.tint)
          return (
            <button
              key={s.id}
              className={"dock-tile" + (s.id === active && !panelOpen ? " active" : "")}
              title={`${s.name}\n${s.url}${st && !st.online ? "\nNot answering" : ""}`}
              onClick={() => onSelect(s.id)}
              style={{ "--tint": color } as React.CSSProperties}
            >
              <span className="dock-face">{initials(s.name)}</span>
              <span className={"dock-dot " + (st ? (st.online ? "on" : "off") : "unknown")} />
              {waiting > 0 && <span className="dock-badge">{waiting}</span>}
            </button>
          )
        })}
        <button className="dock-add" title="Pair a server" onClick={onAdd}><Plus size={18} /></button>
      </div>
      <div className="dock-foot">
        <button className={"dock-manage" + (panel === "approvals" ? " active" : "")} title="Approvals on every server" onClick={onApprovals}>
          <Inbox size={18} />
          {waiting > 0 && <span className="dock-badge">{waiting}</span>}
        </button>
        <button className={"dock-manage" + (panel === "manage" ? " active" : "")} title="Servers and keys" onClick={onManage}>
          <KeyIcon size={18} />
        </button>
      </div>
    </nav>
  )
}
