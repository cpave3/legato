import { initials, tintColor, type Server, type Status } from "../api"
import { KeyIcon, Plus } from "./Icons"

type Props = {
  servers: Server[]
  statuses: Record<string, Status>
  active?: string
  panelOpen: boolean
  onSelect: (id: string) => void
  onAdd: () => void
  onManage: () => void
}

// The dock: each server as a tile in its tint, with a dot for whether it
// answers and a count of what waits to be signed. At its foot, adding a
// server, and the servers and keys.
export function Dock({ servers, statuses, active, panelOpen, onSelect, onAdd, onManage }: Props) {
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
        <button className={"dock-manage" + (panelOpen ? " active" : "")} title="Servers and keys" onClick={onManage}>
          <KeyIcon size={18} />
        </button>
      </div>
    </nav>
  )
}
