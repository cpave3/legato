import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { listen } from "@tauri-apps/api/event"
import { getCurrentWindow } from "@tauri-apps/api/window"
import { api, errorText, tintColor, type ImportRequest, type PageMessage, type Server, type SheetOutcome, type Status } from "./api"
import { ApprovalSheet } from "./components/ApprovalSheet"
import { ApprovalsPanel } from "./components/ApprovalsPanel"
import { Dock } from "./components/Dock"
import { ImportSheet } from "./components/ImportSheet"
import { PairForm } from "./components/PairForm"
import { ServersPanel } from "./components/ServersPanel"
import { TitleBar } from "./components/TitleBar"
import { TrustScreen } from "./components/TrustScreen"
import { useUpdates } from "./components/Updates"

type Panel = null | { kind: "manage"; pairing: boolean } | { kind: "approvals" }
type Sheet = null | { server: Server; id: string }
type Importing = null | { request: ImportRequest; failure?: string }

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
  const [importing, setImporting] = useState<Importing>(null)
  const updates = useUpdates()
  // Whether each server's page will load here (see api.pageTrusted).
  const [pages, setPages] = useState<Record<string, "trusted" | "untrusted">>({})

  const loadServers = useCallback(async () => setServers(await api.servers()), [])
  useEffect(() => { loadServers() }, [loadServers])

  // What a server's page asks of the app (src-tauri/src/bridge.rs): its
  // tray hands a signed approval to the sheet, and Settings › Devices asks
  // to enrol this computer, which is the app's keys.
  const known = useRef<Server[]>([])
  known.current = servers ?? []
  useEffect(() => {
    const off = listen<PageMessage>("page-message", ({ payload }) => {
      const server = known.current.find((s) => s.id === payload.server)
      if (!server) return
      if (payload.kind === "approval") {
        setPanel(null)
        setSheet({ server, id: payload.id })
      } else {
        setPanel({ kind: "manage", pairing: false })
      }
    })
    return () => { off.then((f) => f()) }
  }, [])

  // The sheet closed: its server's page is told how it came out, so its
  // tray stops waiting on it.
  const closeSheet = (outcome: SheetOutcome) => {
    if (sheet) api.pageApprovalDone(sheet.server.id, sheet.id, outcome).catch(() => {})
    setSheet(null)
  }

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

  // Tickets the browser extension hands the app (src-tauri/src/applink.rs):
  // into the server in front when the person chose that, else a sheet asks
  // which. One the server in front refuses opens the sheet with the reason.
  const currentRef = useRef<Server | undefined>(undefined)
  currentRef.current = current
  const imported = (server: Server, task: string) => {
    setImporting(null)
    setPanel(null)
    select(server.id)
    api.openTaskInPage(server.id, task).catch(() => {})
  }
  useEffect(() => {
    if (servers === null) return
    const take = async () => {
      const requests = await api.takeImportRequests().catch(() => [] as ImportRequest[])
      const request = requests[requests.length - 1]
      if (!request) return
      const front = currentRef.current
      const target = await api.importPreference().catch(() => "ask" as const)
      if (target === "current" && front) {
        try {
          const task = await api.importTicket(front.id, request.provider, request.key)
          imported(front, task.id)
        } catch (e) {
          setImporting({ request, failure: `${front.name}: ${errorText(e)}` })
        }
        return
      }
      setImporting({ request })
    }
    take()
    const off = listen("import-request", () => { take() })
    return () => { off.then((f) => f()) }
    // Once the servers are known; take reads the server in front from a ref.
  }, [servers === null])

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
  const overlay = !!panel || !!sheet || !!importing || !current || page !== "trusted"
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

  // The signed approvals, which the sheet decides: the server in front first.
  const waiting = useMemo(() => {
    const out: { server: Server; id: string }[] = []
    for (const s of servers ?? []) for (const a of statuses[s.id]?.approvals ?? []) if (a.signed) out.push({ server: s, id: a.id })
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
        updates={updates}
        onApprovals={() => waiting[0] && setSheet(waiting[0])}
        onReload={() => current && api.reloadServer(current.id)}
      />
      <Dock
        servers={servers}
        statuses={statuses}
        active={current?.id}
        panel={panel?.kind ?? null}
        onSelect={select}
        onAdd={() => setPanel({ kind: "manage", pairing: true })}
        onApprovals={() => setPanel(panel?.kind === "approvals" ? null : { kind: "approvals" })}
        onManage={() => setPanel(panel?.kind === "manage" ? null : { kind: "manage", pairing: false })}
      />
      <main className="content">
        {panel?.kind === "approvals" ? (
          <ApprovalsPanel
            servers={servers}
            statuses={statuses}
            onSign={(server, id) => setSheet({ server, id })}
            onOpen={(server, id) => {
              select(server.id)
              api.openApprovalInPage(server.id, id).catch(() => {})
            }}
            onClose={() => setPanel(null)}
          />
        ) : panel ? (
          <ServersPanel
            servers={servers}
            statuses={statuses}
            startPairing={panel.pairing}
            updates={updates}
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
      {sheet && <ApprovalSheet key={`${sheet.server.id}/${sheet.id}`} server={sheet.server} approvalId={sheet.id} onClose={closeSheet} />}
      {!sheet && importing && (
        <ImportSheet
          key={importing.request.provider + importing.request.key}
          request={importing.request}
          servers={servers}
          statuses={statuses}
          current={current}
          failure={importing.failure}
          onImported={imported}
          onClose={() => setImporting(null)}
        />
      )}
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
