import { useState, useEffect, useCallback, useRef } from "react"
import { useWebSocket, type WSMessage } from "../hooks/useWebSocket"
import { useServer } from "../hooks/useServer"
import { apiFetch } from "../lib/api"

interface ChimeraEvent {
  eventId: string
  sessionId: string
  ts: number
  type: string
  content?: string
  text?: string
  delta?: string
  id?: string
  name?: string
  display?: { summary?: string }
  requestId?: string
  toolName?: string
  reason?: string
  error?: string
  [key: string]: unknown
}

interface SnapshotMessage {
  role: string
  content: string | { type: string; text: string }[]
}

interface SessionSnapshot {
  id: string
  cwd: string
  mode: string
  status: string
  messages?: SnapshotMessage[]
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

// A rendered conversation item — either from snapshot or from live events
interface ConversationItem {
  key: string
  role: "user" | "assistant" | "tool" | "system"
  text: string
  toolName?: string
  toolSummary?: string
  isStreaming?: boolean
}

export function NativeAgentPanel({ agentId }: NativeAgentPanelProps) {
  const { send, subscribe, connected } = useWebSocket()
  const { baseUrl } = useServer()
  const [items, setItems] = useState<ConversationItem[]>([])
  const [draft, setDraft] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [usage, setUsage] = useState<{ totalTokens: number; stepCount: number } | null>(null)
  const [mode, setMode] = useState<string>("")
  const scrollRef = useRef<HTMLDivElement>(null)
  const draftKey = `legato:chimera-draft:${agentId}`
  const seenEventIds = useRef<Set<string>>(new Set())
  const streamingTextRef = useRef<string>("")
  const streamingIdRef = useRef<string>("")

  // Load/save draft
  useEffect(() => {
    const saved = localStorage.getItem(draftKey)
    if (saved) setDraft(saved)
  }, [draftKey])
  useEffect(() => {
    localStorage.setItem(draftKey, draft)
  }, [draftKey, draft])

  // Fetch initial snapshot and hydrate conversation
  useEffect(() => {
    setLoading(true)
    seenEventIds.current.clear()
    streamingTextRef.current = ""
    streamingIdRef.current = ""
    apiFetch(baseUrl, `/api/agents/${agentId}/chimera/snapshot`)
      .then((r) => {
        if (!r.ok) throw new Error(`snapshot: ${r.status}`)
        return r.json()
      })
      .then((data: SessionSnapshot) => {
        setMode(data.mode || "")
        setBusy(data.status === "running")
        if (data.usage) {
          setUsage({ totalTokens: data.usage.totalTokens, stepCount: data.usage.stepCount })
        }
        // Hydrate conversation from snapshot messages
        const snapshotItems: ConversationItem[] = []
        for (let i = 0; i < (data.messages?.length || 0); i++) {
          const msg = data.messages![i]
          const text = extractMessageText(msg)
          if (!text) continue
          // Skip the system prompt (first user message is the role prompt)
          if (i === 0 && msg.role === "user") continue
          snapshotItems.push({
            key: `snap-${i}`,
            role: msg.role as "user" | "assistant" | "tool",
            text,
          })
        }
        setItems(snapshotItems)
      })
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false))
  }, [agentId, baseUrl])

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
    const unsubscribe = subscribe((msg: WSMessage) => {
      if (msg.type !== "chimera_event" || msg.agent_id !== agentId) return
      try {
        const env: ChimeraEvent = JSON.parse(msg.content || "{}")
        // Deduplicate by event ID
        if (env.eventId && seenEventIds.current.has(env.eventId)) return
        if (env.eventId) seenEventIds.current.add(env.eventId)

        switch (env.type) {
          case "session_started":
            setBusy(true)
            break
          case "user_message": {
            setBusy(true)
            if (env.content) {
              setItems((prev) => [...prev, {
                key: env.eventId,
                role: "user",
                text: env.content!,
              }])
            }
            break
          }
          case "assistant_text_delta": {
            // Accumulate streaming text
            if (env.id) streamingIdRef.current = env.id
            if (env.delta) {
              streamingTextRef.current += env.delta
              // Update or append the streaming item
              setItems((prev) => {
                const streamKey = `stream-${streamingIdRef.current}`
                const existing = prev.find((it) => it.key === streamKey)
                if (existing) {
                  return prev.map((it) =>
                    it.key === streamKey ? { ...it, text: streamingTextRef.current } : it
                  )
                }
                return [...prev, {
                  key: streamKey,
                  role: "assistant",
                  text: streamingTextRef.current,
                  isStreaming: true,
                }]
              })
            }
            break
          }
          case "assistant_text_done": {
            // Finalize the streamed text
            const finalText = env.text || streamingTextRef.current
            streamingTextRef.current = ""
            streamingIdRef.current = ""
            setItems((prev) => {
              const streamKey = `stream-${env.id || ""}`
              const existing = prev.find((it) => it.key === streamKey)
              if (existing) {
                return prev.map((it) =>
                  it.key === streamKey
                    ? { ...it, text: finalText, isStreaming: false }
                    : it
                )
              }
              // No streaming item was created (delta events were missed)
              if (finalText) {
                return [...prev, {
                  key: env.eventId,
                  role: "assistant",
                  text: finalText,
                }]
              }
              return prev
            })
            break
          }
          case "tool_call_start": {
            if (env.name) {
              setItems((prev) => [...prev, {
                key: env.eventId,
                role: "tool",
                text: "",
                toolName: env.name,
                toolSummary: env.display?.summary,
              }])
            }
            break
          }
          case "tool_call_result":
            break
          case "permission_request": {
            setBusy(false)
            if (env.requestId) {
              setItems((prev) => [...prev, {
                key: env.eventId,
                role: "system",
                text: "",
                toolName: env.toolName,
              }])
            }
            break
          }
          case "run_finished":
            setBusy(false)
            break
          case "usage_updated": {
            const u = env as unknown as { usage?: { totalTokens: number; stepCount: number } }
            if (u.usage) {
              setUsage({ totalTokens: u.usage.totalTokens, stepCount: u.usage.stepCount })
            }
            break
          }
          case "error":
            if (env.error) setError(env.error)
            break
        }

        // Auto-scroll
        requestAnimationFrame(() => {
          if (scrollRef.current) {
            scrollRef.current.scrollTop = scrollRef.current.scrollHeight
          }
        })
      } catch {
        // ignore parse errors
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

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center">
        <p className="text-zinc-500">Loading Chimera session...</p>
      </div>
    )
  }

  if (error && items.length === 0) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-center">
          <p className="text-zinc-500 mb-2">No Chimera endpoint discovered</p>
          <p className="text-xs text-zinc-600">{error}</p>
        </div>
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col bg-zinc-950">
      {/* Conversation scroll area */}
      <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto">
        {items.length === 0 && (
          <div className="flex h-full items-center justify-center">
            <p className="text-sm text-zinc-500">
              Session ready — send a message to start
            </p>
          </div>
        )}
        {items.map((item) => {
          if (item.role === "user") {
            return (
              <div key={item.key} className="flex justify-end px-4 py-2">
                <div className="max-w-[80%] rounded-lg bg-blue-600/20 px-3 py-2 text-sm text-zinc-200 whitespace-pre-wrap">
                  {item.text}
                </div>
              </div>
            )
          }
          if (item.role === "assistant") {
            return (
              <div key={item.key} className="flex justify-start px-4 py-2">
                <div className={`max-w-[80%] whitespace-pre-wrap rounded-lg bg-zinc-800 px-3 py-2 text-sm text-zinc-200 ${item.isStreaming ? "opacity-80" : ""}`}>
                  {item.text}
                  {item.isStreaming && <span className="ml-0.5 animate-pulse">▋</span>}
                </div>
              </div>
            )
          }
          if (item.role === "tool") {
            return (
              <div key={item.key} className="px-4 py-1">
                <div className="flex items-center gap-2 text-xs text-zinc-400">
                  <span className="rounded bg-zinc-700 px-1.5 py-0.5 font-mono">
                    {item.toolName || "tool"}
                  </span>
                  {item.toolSummary && <span>{item.toolSummary}</span>}
                </div>
              </div>
            )
          }
          // system (permission request)
          return (
            <div key={item.key} className="mx-4 my-2 rounded-lg border border-yellow-600/40 bg-yellow-900/10 p-3">
              <div className="flex items-center justify-between">
                <div className="text-sm text-yellow-200">
                  Permission: <span className="font-mono">{item.toolName || "unknown"}</span>
                </div>
                <div className="flex gap-2">
                  <button
                    className="rounded bg-green-700/60 px-2 py-1 text-xs text-white hover:bg-green-600/60"
                    onClick={() => handlePermission(item.key, "allow")}
                  >
                    Allow
                  </button>
                  <button
                    className="rounded bg-red-700/60 px-2 py-1 text-xs text-white hover:bg-red-600/60"
                    onClick={() => handlePermission(item.key, "deny")}
                  >
                    Deny
                  </button>
                </div>
              </div>
            </div>
          )
        })}
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
        {mode && (
          <span className="rounded bg-zinc-800 px-1.5 py-0.5">{mode}</span>
        )}
        {usage && (
          <span>
            {usage.totalTokens.toLocaleString()} tokens · {usage.stepCount} steps
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

function extractMessageText(msg: SnapshotMessage): string {
  const content = msg.content
  if (typeof content === "string") return content
  if (Array.isArray(content)) {
    return content
      .filter((p) => p.type === "text")
      .map((p) => p.text)
      .join("")
  }
  return ""
}
