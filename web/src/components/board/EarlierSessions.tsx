import { useEffect, useState } from "react"
import { Link } from "react-router-dom"
import { useServer } from "../../hooks/useServer"
import { agentKindLabel } from "../../lib/agentKind"
import { fetchSessions, sessionStateLabel, type SessionEntry } from "../../lib/sessionHistory"

const SHOWN = 5

// EarlierSessions lists a task's conversations in the session history,
// each leading to it there. It shows nothing for a task with none, or
// when the history cannot be read. It fetches for itself rather than
// through react-query: the board's detail panel is drawn where there may
// be no query client.
export function EarlierSessions({ taskId, headingClass }: { taskId: string; headingClass?: string }) {
  const { baseUrl } = useServer()
  const [sessions, setSessions] = useState<SessionEntry[]>([])
  useEffect(() => {
    let current = true
    setSessions([])
    fetchSessions(baseUrl, { task: taskId, limit: SHOWN + 1 })
      .then((page) => { if (current) setSessions(page.sessions) })
      .catch(() => { /* no history to show */ })
    return () => { current = false }
  }, [baseUrl, taskId])
  if (sessions.length === 0) return null
  const all = `/agents/history?task=${encodeURIComponent(taskId)}`
  return (
    <section aria-labelledby="task-sessions-heading" className="space-y-1">
      <h3 id="task-sessions-heading" className={headingClass}>Earlier sessions</h3>
      <ul className="m-0 flex list-none flex-col gap-0.5 p-0">
        {sessions.slice(0, SHOWN).map((session) => (
          <li key={session.id}>
            <Link to={`${all}&session=${session.id}`} className="flex flex-col rounded px-1.5 py-1 text-xs hover:bg-zinc-800/60">
              <span className="text-zinc-300">{agentKindLabel(session.agent_kind)} · {sessionStateLabel(session)}</span>
              <span className="text-[11px] text-zinc-500">{new Date(session.last_seen_at).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" })}</span>
            </Link>
          </li>
        ))}
      </ul>
      <Link to={all} className="block px-1.5 text-xs text-lw-link">{sessions.length > SHOWN ? "All of them" : "In Session history"}</Link>
    </section>
  )
}
