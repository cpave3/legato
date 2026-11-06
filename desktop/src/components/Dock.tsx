import { useRef, useState } from "react"
import { initials, tintColor, type Server, type Status } from "../api"
import { Inbox, KeyIcon, Plus } from "./Icons"

type Props = {
  servers: Server[]
  statuses: Record<string, Status>
  active?: string
  panel: "manage" | "approvals" | null
  onSelect: (id: string) => void
  onReorder: (ids: string[]) => void
  onAdd: () => void
  onApprovals: () => void
  onManage: () => void
}

// The dock: each server as a tile in its tint, with a dot for whether it
// answers and a count of the approvals waiting there. At its foot, every
// server's approvals, and the servers and keys.
export function Dock({ servers, statuses, active, panel, onSelect, onReorder, onAdd, onApprovals, onManage }: Props) {
  const panelOpen = panel !== null
  // Press and hold a tile, then drag it up or down to move it. `drag` holds
  // the order while it is in the air; letting go saves it.
  const [drag, setDrag] = useState<{ id: string; order: string[]; dy: number } | null>(null)
  const tiles = useRef(new Map<string, HTMLButtonElement>())
  const moved = useRef(false)
  const shown = drag ? drag.order.map((id) => servers.find((s) => s.id === id)).filter((s): s is Server => !!s) : servers

  const press = (e: React.PointerEvent<HTMLButtonElement>, id: string) => {
    if (e.button !== 0) return
    const el = e.currentTarget
    const startY = e.clientY
    let order = servers.map((s) => s.id)
    let active = false
    moved.current = false
    const move = (ev: PointerEvent) => {
      const dy = ev.clientY - startY
      if (!active) {
        if (Math.abs(dy) < 6) return
        active = true
        moved.current = true
        el.setPointerCapture(e.pointerId)
      }
      // The tile goes where the pointer is: past the middle of a neighbour swaps them.
      const mid = (i: string) => {
        const r = tiles.current.get(i)?.getBoundingClientRect()
        return r ? r.top + r.height / 2 : 0
      }
      const others = order.filter((i) => i !== id)
      let at = others.findIndex((i) => ev.clientY < mid(i))
      if (at < 0) at = others.length
      others.splice(at, 0, id)
      order = others
      setDrag({ id, order, dy })
    }
    const up = () => {
      el.removeEventListener("pointermove", move)
      el.removeEventListener("pointerup", up)
      el.removeEventListener("pointercancel", up)
      if (active) {
        if (order.join() !== servers.map((s) => s.id).join()) onReorder(order)
        setDrag(null)
        // The click that follows the release is not a selection.
        setTimeout(() => (moved.current = false), 0)
      }
    }
    el.addEventListener("pointermove", move)
    el.addEventListener("pointerup", up)
    el.addEventListener("pointercancel", up)
  }

  const waiting = servers.reduce((n, s) => n + (statuses[s.id]?.approvals.length ?? 0), 0)
  return (
    <nav className="dock" aria-label="Servers">
      <div className="dock-servers">
        {shown.map((s) => {
          const st = statuses[s.id]
          const waiting = st?.approvals.length ?? 0
          const color = tintColor(s.tint)
          return (
            <button
              key={s.id}
              ref={(el) => { if (el) tiles.current.set(s.id, el); else tiles.current.delete(s.id) }}
              className={"dock-tile" + (s.id === active && !panelOpen ? " active" : "") + (drag?.id === s.id ? " dragging" : "")}
              title={`${s.name}\n${s.url}${st && !st.online ? "\nNot answering" : ""}`}
              onPointerDown={(e) => press(e, s.id)}
              onClick={() => { if (!moved.current) onSelect(s.id) }}
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
