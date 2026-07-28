import { useState, useEffect, useCallback, useRef } from "react"
import { useWebSocket } from "../hooks/useWebSocket"
import { useServer } from "../hooks/useServer"
import { apiFetch } from "../lib/api"

interface ChimeraEvent {
  eventId: string
  sessionId: string
  ts: number
  type: string
  content?: string
  text?: string
  name?: string
  display?: { summary?: string }
  requestId?: string
  toolName?: string
  reason?: string
  error?: string
  [key: string]: unknown
}

interface SessionSnapshot {
  id: string
  cwd: string
  mode: string
  status: string
  messages?: unknown[]
  toolCalls?: unknown[]
  usage?: {
    inputTokens: number
    outputTokens: number
    totalTokens: number
    stepCount: number
  }
  tasks?: { content: string; status: string }[]
}

interface NativeAgentPanelProps {
  agentId: string
  onClickTerminal?: () => void
}

export function NativeAgentPanel({ agentId }: NativeAgentPanelProps) {
  const { send, subscribe, connected } = useWebSocket()
  const { baseUrl } = useServer()
  const [snapshot, setSnapshot] = useState<SessionSnapshot | null>(null)
  const [events, setEvents] = useState<ChimeraEvent[]>([])
  const [draft, setDraft] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const scrollRef = useRef<HTMLDivElement>(null)
  const draftKey = `legato:chimera-draft:${agentId}`

  // Load draft from localStorage
  useEffect(() => {
    const saved = localStorage.getItem(draftKey)
    if (saved) setDraft(saved)
  }, [draftKey])

  // Save draft to localStorage
  useEffect(() => {
    localStorage.setItem(draftKey, draft)
  }, [draftKey, draft])

  // Fetch initial snapshot
  useEffect(() => {
    setLoading(true)
    apiFetch(baseUrl, `/api/agents/${agentId}/chimera/snapshot`)
      .then((r) => {
        if (!r.ok) throw new Error(`snapshot: ${r.status}`)
        return r.json()
      })
      .then((data: SessionSnapshot) => {
        setSnapshot(data)
        setBusy(data.status === "running")
      })
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false))
  }, [agentId])

  // Subscribe to Chimera events via WebSocket
  useEffect(() => {
    if (connected) {
      send({ type: "subscribe_agent", agent_id: agentId })
    }
    return () => {
      if (connected) {
        send({ type: "unsubscribe_agent", agent_id: agentId })
      }
    }
  }, [agentId, connected, send])

  // Handle incoming WebSocket messages
  useEffect(() => {
    const unsubscribe = subscribe((msg) => {
      if (msg.type === "chimera_event" && msg.agent_id === agentId) {
        try {
          const env: ChimeraEvent = JSON.parse(msg.content || "{}")
          setEvents((prev) => [...prev, env])
          if (env.type === "run_finished") {
            setBusy(false)
          } else if (env.type === "user_message" || env.type === "session_started") {
            setBusy(true)
          } else if (env.type === "permission_request") {
            setBusy(false)
          }
          requestAnimationFrame(() => {
            if (scrollRef.current) {
              scrollRef.current.scrollTop = scrollRef.current.scrollHeight
            }
          })
        } catch {
          // ignore parse errors
        }
      }
    })
    return unsubscribe
  }, [subscribe, agentId])

  const handleSend = useCallback(() => {
    if (!draft.trim() || busy) return
    send({ type: "agent_message", agent_id: agentId, content: draft.trim() })
    setDraft("")
    setBusy(true)
    setError(null)
  }, [draft, busy, agentId, send])

  const handleInterrupt = useCallback(() => {
    send({ type: "agent_interrupt", agent_id: agentId })
  }, [agentId, send])

  const handlePermission = useCallback(
    (requestId: string, decision: string) => {
      send({
        type: "agent_permission",
        agent_id: agentId,
        content: JSON.stringify({ request_id: requestId, decision }),
      } as never)
    },
    [agentId, send],
  )

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault()
        handleSend()
      }
    },
    [handleSend],
  )

  const renderedEvents = events.map((env, i) => {
    switch (env.type) {
      case "user_message":
        return (
          <div key={i} className="flex justify-end px-4 py-2">
            <div className="max-w-[80%] rounded-lg bg-blue-600/20 px-3 py-2 text-sm text-zinc-200">
              {String(env.content || "")}
            </div>
          </div>
        )
      case "assistant_text_done":
        return (
          <div key={i} className="flex justify-start px-4 py-2">
            <div className="max-w-[80%] whitespace-pre-wrap rounded-lg bg-zinc-800 px-3 py-2 text-sm text-zinc-200">
              {String(env.text || "")}
            </div>
          </div>
        )
      case "tool_call_start":
        return (
          <div key={i} className="px-4 py-1">
            <div className="flex items-center gap-2 text-xs text-zinc-400">
              <span className="rounded bg-zinc-700 px-1.5 py-0.5 font-mono">{String(env.name || "tool")}</span>
              {env.display?.summary && <span>{env.display.summary}</span>}
            </div>
          </div>
        )
      case "tool_call_result":
        return null
      case "permission_request":
        return (
          <div key={i} className="mx-4 my-2 rounded-lg border border-yellow-600/40 bg-yellow-900/10 p-3">
            <div className="flex items-center justify-between">
              <div className="text-sm text-yellow-200">
                Permission: <span className="font-mono">{String(env.toolName || "unknown")}</span>
              </div>
              <div className="flex gap-2">
                <button
                  className="rounded bg-green-700/60 px-2 py-1 text-xs text-white hover:bg-green-600/60"
                  onClick={() => handlePermission(String(env.requestId || ""), "allow")}
                >
                  Allow
                </button>
                <button
                  className="rounded bg-red-700/60 px-2 py-1 text-xs text-white hover:bg-red-600/60"
                  onClick={() => handlePermission(String(env.requestId || ""), "deny")}
                >
                  Deny
                </button>
              </div>
            </div>
          </div>
        )
      case "run_finished":
        return (
          <div key={i} className="px-4 py-1 text-xs text-zinc-500 italic">
            Turn ended ({String(env.reason || "stop")})
          </div>
        )
      case "error":
        return (
          <div key={i} className="px-4 py-1 text-xs text-red-400">
            {String(env.error || "unknown error")}
          </div>
        )
      default:
        return null
    }
  })

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center">
        <p className="text-zinc-500">Loading Chimera session...</p>
      </div>
    )
  }

  if (error && !snapshot) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-center">
          <p className="text-zinc-500 mb-2">No Chimera endpoint discovered</p>
          <p className="text-xs text-zinc-600">{error}</p>
          <p className="text-xs text-zinc-600 mt-2">Falling back to terminal view...</p>
        </div>
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col bg-zinc-950">
      {/* Conversation scroll area */}
      <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto">
        {renderedEvents.length === 0 && (
          <div className="flex h-full items-center justify-center">
            <p className="text-sm text-zinc-500">
              Session ready — send a message to start
            </p>
          </div>
        )}
        {renderedEvents}
      </div>

      {/* Status bar */}
      <div className="flex items-center gap-3 border-t border-zinc-800 px-4 py-1.5 text-xs text-zinc-500">
        {busy ? (
          <span className="flex items-center gap-1.5 text-green-400">
            <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-green-400" />
            Working
          </span>
        ) : (
          <span className="text-zinc-500">Idle</span>
        )}
        {snapshot?.mode && (
          <span className="rounded bg-zinc-800 px-1.5 py-0.5">{snapshot.mode}</span>
        )}
        {snapshot?.usage && (
          <span>
            {snapshot.usage.totalTokens.toLocaleString()} tokens · {snapshot.usage.stepCount} steps
          </span>
        )}
        {error && <span className="text-red-400">{error}</span>}
      </div>

      {/* Composer */}
      <div className="border-t border-zinc-800 p-3">
        <div className="flex gap-2">
          <textarea
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={busy ? "Agent is working..." : "Send a message..."}
            disabled={busy}
            rows={2}
            className="flex-1 resize-none rounded-lg border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm text-zinc-200 placeholder:text-zinc-600 focus:border-zinc-500 focus:outline-none disabled:opacity-50"
          />
          <div className="flex flex-col gap-1">
            <button
              onClick={handleSend}
              disabled={!draft.trim() || busy}
              className="rounded-lg bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-500 disabled:opacity-40"
            >
              Send
            </button>
            {busy && (
              <button
                onClick={handleInterrupt}
                className="rounded-lg bg-red-700/60 px-4 py-2 text-xs text-white hover:bg-red-600/60"
              >
                Interrupt
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
