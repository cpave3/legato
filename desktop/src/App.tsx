import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { listen } from "@tauri-apps/api/event"
import { getCurrentWindow } from "@tauri-apps/api/window"
import { api, errorText, tintColor, type ImportRequest, type PageLoadFailed, type PageMessage, type Server, type SheetOutcome, type Status } from "./api"
import { ApprovalSheet } from "./components/ApprovalSheet"
import { ApprovalsPanel } from "./components/ApprovalsPanel"
import { Dock } from "./components/Dock"
import { ImportSheet } from "./components/ImportSheet"
import { OfflineScreen } from "./components/OfflineScreen"
import { PairForm } from "./components/PairForm"
import { ServersPanel } from "./components/ServersPanel"
import { TitleBar } from "./components/TitleBar"
import { TrustScreen } from "./components/TrustScreen"
import { useUpdates } from "./components/Updates"
import { failedAtStart, recovered, screenFor, type Failed } from "./pageState"

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

  // The voice widget opens a server's settings, or Servers & keys.
  useEffect(() => {
    const offServer = listen<{ server: string }>("shell-show-server", ({ payload }) => {
      setActive(payload.server)
      setPanel(null)
    })
    const offPanel = listen("shell-open-panel", () => setPanel({ kind: "manage", pairing: false }))
    return () => {
      offServer.then((f) => f())
      offPanel.then((f) => f())
    }
  }, [])

  // The sheet closed: its server's page is told how it came out, so its
  // tray stops waiting on it.
  const closeSheet = (outcome: SheetOutcome) => {
    if (sheet) api.pageApprovalDone(sheet.server.id, sheet.id, outcome).catch(() => {})
    setSheet(null)
  }

  // Pages that did not load (see pageState.ts): the web view says so on
  // Linux, and a page made before its server was known to be down is taken
  // as one. The shell draws the offline screen in their place.
  const [failed, setFailed] = useState<Failed>({})
  const failedRef = useRef<Failed>({})
  failedRef.current = failed
  const made = useRef(new Set<string>())
  const statusesRef = useRef<Record<string, Status>>({})
  useEffect(() => {
    const off = listen<PageLoadFailed>("page-load-failed", ({ payload }) => {
      setFailed((f) => ({ ...f, [payload.server]: payload.uri }))
    })
    return () => { off.then((f) => f()) }
  }, [])

  // Each server's state and open approvals, every few seconds. A failed
  // page is loaded again once its server answers: at most once a poll, so a
  // page that keeps failing is not loaded in a loop.
  const refresh = useCallback(async () => {
    try {
      const list = await api.poll()
      const next = Object.fromEntries(list.map((s) => [s.id, s]))
      const now: Failed = { ...failedRef.current }
      for (const id of failedAtStart(made.current, statusesRef.current, next)) now[id] ??= ""
      for (const id of recovered(now, next)) {
        api.loadServerPage(id, now[id]).catch(() => {})
        delete now[id]
      }
      statusesRef.current = next
      failedRef.current = now
      setFailed(now)
      setStatuses(next)
    } catch { /* the next tick tries again */ }
  }, [])
  useEffect(() => {
    refresh()
    const id = setInterval(refresh, 3000)
    return () => clearInterval(id)
  }, [refresh])

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

  // A server that is down gets the app's offline screen, not its page.
  const screen = current ? screenFor(statuses[current.id], current.id in failed) : "page"

  // The content area shows the server's page unless the shell draws over it.
  // A server known to be down gets no page until it answers.
  const overlay = !!panel || !!sheet || !!importing || !current || page !== "trusted" || screen === "offline"
  useEffect(() => {
    if (!servers) return
    if (current && !panel && page === "trusted" && screen === "page") {
      made.current.add(current.id)
      api.showServer(current.id).catch(() => {})
    }
    api.setContentVisible(!overlay).catch(() => {})
  }, [servers, current, panel, overlay, page, screen])

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
        onReload={() => { if (screen === "offline") refresh(); else if (current) api.reloadServer(current.id) }}
      />
      <Dock
        servers={servers}
        statuses={statuses}
        active={current?.id}
        panel={panel?.kind ?? null}
        onSelect={select}
        onReorder={(ids) => {
          setServers((list) => list && ids.map((id) => list.find((s) => s.id === id)).filter((s): s is Server => !!s))
          api.reorderServers(ids).catch(() => loadServers())
        }}
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
        ) : screen === "offline" ? (
          <OfflineScreen server={current} status={statuses[current.id]} onRetry={refresh} />
        ) : (
          <div className="content-under" />
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
