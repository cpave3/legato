import { useCallback, useEffect, useMemo, useState } from "react"
import { getCurrentWindow } from "@tauri-apps/api/window"
import { api, tintColor, type Approval, type Server, type Status } from "./api"
import { ApprovalSheet } from "./components/ApprovalSheet"
import { Dock } from "./components/Dock"
import { PairForm } from "./components/PairForm"
import { ServersPanel } from "./components/ServersPanel"
import { TitleBar } from "./components/TitleBar"
import { TrustScreen } from "./components/TrustScreen"

type Panel = null | { kind: "manage"; pairing: boolean }
type Sheet = null | { server: Server; approval: Approval }

const ACTIVE = "legato-desktop:active"

function remembered(): string | undefined {
  try { return localStorage.getItem(ACTIVE) ?? undefined } catch { return undefined }
}

export function App() {
  const [servers, setServers] = useState<Server[] | null>(null)
  const [statuses, setStatuses] = useState<Record<string, Status>>({})
  const [active, setActive] = useState<string | undefined>(remembered)
  const [panel, setPanel] = useState<Panel>(null)
  const [sheet, setSheet] = useState<Sheet>(null)
  // Whether each server's page will load here (see api.pageTrusted).
  const [pages, setPages] = useState<Record<string, "trusted" | "untrusted">>({})

  const loadServers = useCallback(async () => setServers(await api.servers()), [])
  useEffect(() => { loadServers() }, [loadServers])

  // Each server's state and open approvals, every few seconds.
  useEffect(() => {
    let stop = false
    const tick = async () => {
      try {
        const list = await api.poll()
        if (!stop) setStatuses(Object.fromEntries(list.map((s) => [s.id, s])))
      } catch { /* the next tick tries again */ }
    }
    tick()
    const id = setInterval(tick, 3000)
    return () => { stop = true; clearInterval(id) }
  }, [])

  // The server in front: the remembered one, else the first.
  const current = useMemo(() => servers?.find((s) => s.id === active) ?? servers?.[0], [servers, active])

  // A page the web view would not load (a Mac that does not trust the
  // server's certificate yet) is not shown: the shell explains instead. A
  // check that fails (the server is down) shows the page as before.
  useEffect(() => {
    if (!current || pages[current.id]) return
    let stop = false
    const id = current.id
    api.pageTrusted(id)
      .then((ok) => { if (!stop) setPages((p) => ({ ...p, [id]: ok ? "trusted" : "untrusted" })) })
      .catch(() => { if (!stop) setPages((p) => ({ ...p, [id]: "trusted" })) })
    return () => { stop = true }
  }, [current, pages])
  const page = current ? pages[current.id] : undefined
  const recheck = (id: string) => setPages((p) => { const { [id]: _, ...rest } = p; return rest })

  // The content area shows the server's page unless the shell draws over it.
  const overlay = !!panel || !!sheet || !current || page !== "trusted"
  useEffect(() => {
    if (!servers) return
    if (current && !panel && page === "trusted") api.showServer(current.id).catch(() => {})
    api.setContentVisible(!overlay).catch(() => {})
  }, [servers, current, panel, overlay, page])

  const select = (id: string) => {
    setActive(id)
    setPanel(null)
    try { localStorage.setItem(ACTIVE, id) } catch { /* only a convenience */ }
  }

  const waiting = useMemo(() => {
    const out: { server: Server; approval: Approval }[] = []
    for (const s of servers ?? []) for (const a of statuses[s.id]?.approvals ?? []) out.push({ server: s, approval: a })
    // The server in front first.
    return out.sort((a, b) => Number(b.server.id === current?.id) - Number(a.server.id === current?.id))
  }, [servers, statuses, current])

  // The server in front tints the chrome, as its own web UI tints its page;
  // the panels are the app's, and stay neutral.
  const tint = panel ? undefined : (current && (statuses[current.id]?.tint ?? current.tint)) || undefined
  const tintStyle = tint ? ({ "--server-tint": tintColor(tint) } as React.CSSProperties) : undefined

  if (servers === null) return <div className="app" />

  return (
    <div className={"app" + (tint ? " tinted" : "")} style={tintStyle}>
      <ResizeEdges />
      <TitleBar
        server={panel ? undefined : current}
        status={current ? statuses[current.id] : undefined}
        waiting={waiting.length}
        onApprovals={() => waiting[0] && setSheet(waiting[0])}
        onReload={() => current && api.reloadServer(current.id)}
      />
      <Dock
        servers={servers}
        statuses={statuses}
        active={current?.id}
        panelOpen={!!panel}
        onSelect={select}
        onAdd={() => setPanel({ kind: "manage", pairing: true })}
        onManage={() => setPanel(panel ? null : { kind: "manage", pairing: false })}
      />
      <main className="content">
        {panel ? (
          <ServersPanel
            servers={servers}
            statuses={statuses}
            startPairing={panel.pairing}
            onChanged={loadServers}
            onClose={() => setPanel(null)}
          />
        ) : !current ? (
          <Welcome onPaired={(s) => { loadServers(); select(s.id) }} />
        ) : page === "untrusted" ? (
          <TrustScreen
            server={current}
            onTrusted={() => setPages((p) => ({ ...p, [current.id]: "trusted" }))}
            onCheckAgain={() => recheck(current.id)}
          />
        ) : (
          <div className="content-under">
            {statuses[current.id] && !statuses[current.id].online && (
              <p className="dim">{current.name} is not answering: {statuses[current.id].error}</p>
            )}
          </div>
        )}
      </main>
      {sheet && <ApprovalSheet server={sheet.server} approval={sheet.approval} onClose={() => setSheet(null)} />}
    </div>
  )
}

function Welcome({ onPaired }: { onPaired: (s: Server) => void }) {
  return (
    <section className="welcome">
      <img src="/mark.svg" alt="" className="welcome-mark" />
      <h1>Every Legato in one window</h1>
      <p className="dim">Pair a server with the four words it shows. Add a security key afterwards to sign approvals with a touch.</p>
      <div className="card"><PairForm onPaired={onPaired} /></div>
    </section>
  )
}

type Direction = "North" | "South" | "East" | "West" | "NorthEast" | "NorthWest" | "SouthEast" | "SouthWest"

// A frameless window resizes from thin strips along its edges.
function ResizeEdges() {
  const drag = (d: Direction) => (e: React.MouseEvent) => {
    if (e.button === 0) getCurrentWindow().startResizeDragging(d)
  }
  return (
    <>
      {(["North", "South", "East", "West", "NorthEast", "NorthWest", "SouthEast", "SouthWest"] as Direction[]).map((d) => (
        <div key={d} className={"edge edge-" + d} onMouseDown={drag(d)} />
      ))}
    </>
  )
}
