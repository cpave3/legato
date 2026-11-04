import { initials, tintColor, type Approval, type Server, type Status } from "../api"
import { Seal } from "./Icons"

type Props = {
  servers: Server[]
  statuses: Record<string, Status>
  onSign: (server: Server, id: string) => void
  onOpen: (server: Server, id: string) => void
  onClose: () => void
}

// The open approvals of every server, as the Android app lists them. A
// signed one opens the sheet, which signs with a key; any other opens its
// server's page on it, where its tray answers it (a question, a plan, a
// note for the agent).
export function ApprovalsPanel({ servers, statuses, onSign, onOpen, onClose }: Props) {
  const groups = servers.map((s) => ({ server: s, status: statuses[s.id] }))
  const total = groups.reduce((n, g) => n + (g.status?.approvals.length ?? 0), 0)
  return (
    <section className="panel">
      <header className="panel-head">
        <h1>Approvals</h1>
        <span className="spacer" />
        <button className="btn" onClick={onClose}>Done</button>
      </header>
      {total === 0 && <p className="dim">Nothing is waiting for you on any server.</p>}
      {groups.map(({ server, status }) => {
        const list = status?.approvals ?? []
        if (!list.length && status?.online !== false) return null
        return (
          <div className="panel-section" key={server.id}>
            <div className="section-head">
              <span className="mini-tile" style={{ "--tint": tintColor(server.tint) } as React.CSSProperties}>{initials(server.name)}</span>
              <h2>{server.name}</h2>
              <span className="spacer" />
              {status && !status.online && <span className="chip warn">not answering</span>}
            </div>
            {list.map((a) => <ApprovalRow key={a.id} approval={a} onClick={() => (a.signed ? onSign(server, a.id) : onOpen(server, a.id))} />)}
          </div>
        )
      })}
    </section>
  )
}

function ApprovalRow({ approval: a, onClick }: { approval: Approval; onClick: () => void }) {
  const asker = [a.details?.agent_kind, a.details?.task_title].filter(Boolean).join(" · ")
  return (
    <button className={"approval-row" + (a.signed ? " signed" : "")} onClick={onClick}>
      <span className="approval-mark" />
      <span className="grow">
        <span className="approval-title">{a.title}</span>
        <span className="approval-meta">
          {a.source.replace(/_/g, " ")}
          {asker && ` · ${asker}`}
        </span>
      </span>
      {a.signed ? <span className="chip sign"><Seal size={12} /> Sign</span> : <span className="chip">Open</span>}
    </button>
  )
}
