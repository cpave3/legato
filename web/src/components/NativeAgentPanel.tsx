import { useState, useEffect, useCallback, useRef } from "react"
import { ArrowDown } from "lucide-react"
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
  args?: Record<string, unknown>
  result?: Record<string, unknown>
  callId?: string
  display?: { summary?: string; detail?: string }
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

interface SnapshotToolCall {
  callId: string
  name: string
  args: Record<string, unknown>
  result?: Record<string, unknown>
  startedAt?: number
  endedAt?: number
  display?: { summary?: string; detail?: string }
}

interface SessionSnapshot {
  id: string
  cwd: string
  mode: string
  status: string
  messages?: SnapshotMessage[]
  toolCalls?: SnapshotToolCall[]
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

type ItemRole = "user" | "assistant" | "tool" | "system"

interface ToolCallItem {
  key: string
  toolName: string
  args: Record<string, unknown>
  result?: Record<string, unknown>
  summary?: string
  durationMs?: number
}

interface ConversationItem {
  key: string
  role: ItemRole
  text: string
  isStreaming?: boolean
  tool?: ToolCallItem
  toolName?: string // for permission requests
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
  // Track which snapshot tool calls we've already rendered (by position in messages)
  const snapshotToolCallCount = useRef<number>(0)
  const seenCallIds = useRef<Set<string>>(new Set())
  const [isScrolledUp, setIsScrolledUp] = useState(false)

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
    seenCallIds.current.clear()
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
        // Hydrate conversation from snapshot messages AND tool calls.
        // Tool calls are interleaved with messages in the conversation order.
        // We walk both arrays, inserting tool calls after the assistant message
        // that produced them (by matching step boundaries).
        const snapshotItems: ConversationItem[] = []
        const msgs = data.messages || []
        const tcs = data.toolCalls || []
        snapshotToolCallCount.current = tcs.length
        for (const tc of tcs) {
          if (tc.callId) seenCallIds.current.add(tc.callId)
        }

        for (let i = 0; i < msgs.length; i++) {
          const msg = msgs[i]
          const text = extractMessageText(msg)
          if (!text) continue
          // Skip the system/role prompt (first user message)
          if (i === 0 && msg.role === "user") continue

          snapshotItems.push({
            key: `snap-msg-${i}`,
            role: msg.role as ItemRole,
            text,
          })

          // If this is an assistant message, check if tool calls follow
          // (tool calls in the snapshot are in order, we insert them as we
          // encounter assistant messages that should have produced them)
          if (msg.role === "assistant") {
            // Insert all tool calls — since we can't precisely map which
            // assistant message produced which tool call, we insert tool
            // calls after each assistant message in order, distributing
            // roughly evenly. A simpler approach: insert all tool calls
            // after the last assistant message.
            // Actually, the snapshot's toolCalls array is chronological.
            // We'll insert tool calls after the assistant message that
            // precedes them chronologically. For simplicity, insert all
            // remaining tool calls after the last assistant message.
            if (i === msgs.length - 1 || msgs[i + 1]?.role !== "assistant") {
              // Insert all tool calls that haven't been placed yet
              for (let j = 0; j < tcs.length; j++) {
                snapshotItems.push({
                  key: `snap-tc-${j}`,
                  role: "tool" as ItemRole,
                  text: "",
                  tool: {
                    key: `snap-tc-${j}`,
                    toolName: tcs[j].name,
                    args: tcs[j].args,
                    result: tcs[j]?.result,
                    summary: tcs[j]?.display?.summary,
                    durationMs: tcs[j]?.endedAt && tcs[j]?.startedAt
                      ? tcs[j]!.endedAt! - tcs[j]!.startedAt!
                      : undefined,
                  },
                })
              }
            }
          }
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
            if (env.id) streamingIdRef.current = env.id
            if (env.delta) {
              streamingTextRef.current += env.delta
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
              if (finalText) {
                return [...prev, { key: env.eventId, role: "assistant", text: finalText }]
              }
              return prev
            })
            break
          }
          case "tool_call_start": {
            // Skip tool calls already rendered from the snapshot
            if (env.callId && seenCallIds.current.has(env.callId)) break
            if (env.callId) seenCallIds.current.add(env.callId)
            if (env.name) {
              setItems((prev) => [...prev, {
                key: env.eventId,
                role: "tool",
                text: "",
                tool: {
                  key: env.eventId,
                  toolName: env.name!,
                  args: env.args || {},
                  summary: env.display?.summary,
                },
              }])
            }
            break
          }
          case "tool_call_result": {
            // Update the tool call item with results
            if (env.callId) {
              setItems((prev) => prev.map((it) => {
                if (it.tool && it.tool.key === env.callId) {
                  return {
                    ...it,
                    tool: {
                      ...it.tool,
                      result: env.result,
                      summary: env.display?.summary || it.tool.summary,
                    },
                  }
                }
                // Also check by eventId match for tool_call_start
                if (it.key === env.callId && it.tool) {
                  return {
                    ...it,
                    tool: {
                      ...it.tool,
                      result: env.result,
                      summary: env.display?.summary || it.tool.summary,
                    },
                  }
                }
                return it
              }))
            }
            break
          }
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

  const scrollToBottom = useCallback(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
      setIsScrolledUp(false)
    }
  }, [])

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
    <div className="relative flex h-full flex-col bg-zinc-950">
      {/* Conversation scroll area */}
      <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto" onScroll={(e) => { const el = e.currentTarget; const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40; setIsScrolledUp(!atBottom) }}>
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
          if (item.role === "tool" && item.tool) {
            return <ToolCallCard key={item.key} tool={item.tool} />
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

      {/* Scroll to bottom button */}
      {isScrolledUp && (
        <button
          onClick={scrollToBottom}
          className="absolute bottom-16 right-3 z-10 rounded-full bg-zinc-800 border border-zinc-700 p-2 text-zinc-400 shadow-lg transition-colors hover:bg-zinc-700 hover:text-zinc-200 active:bg-zinc-600"
          title="Scroll to bottom"
        >
          <ArrowDown size={18} />
        </button>
      )}

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

function ToolCallCard({ tool }: { tool: ToolCallItem }) {
  const [expanded, setExpanded] = useState(false)

  const name = tool.toolName
  const args = tool.args || {}
  const summary = tool.summary || ""
  const result = tool.result || {}
  const durationMs = tool.durationMs

  // For edit tool calls, render a diff
  if (name === "edit") {
    const path = args.path as string || ""
    const oldStr = args.old_string as string || ""
    const newStr = args.new_string as string || ""
    const replacements = result.replacements as number | undefined
    const filename = path.split("/").pop() || path

    return (
      <div className="mx-4 my-1.5 rounded-lg border border-zinc-700 bg-zinc-900/50">
        <button
          className="flex w-full items-center gap-2 px-3 py-1.5 text-left"
          onClick={() => setExpanded(!expanded)}
        >
          <span className="rounded bg-purple-700/60 px-1.5 py-0.5 font-mono text-xs text-white">edit</span>
          <span className="text-xs text-zinc-300 font-mono truncate">{filename}</span>
          {replacements !== undefined && (
            <span className="text-xs text-zinc-500">({replacements} replacement{replacements !== 1 ? "s" : ""})</span>
          )}
          {durationMs !== undefined && <span className="text-xs text-zinc-600">{durationMs}ms</span>}
          <span className="ml-auto text-xs text-zinc-500">{expanded ? "▼" : "▶"}</span>
        </button>
        {expanded && (
          <div className="border-t border-zinc-800 px-3 py-2">
            {path !== filename && <div className="text-xs text-zinc-500 mb-1">{path}</div>}
            <div className="font-mono text-xs">
              {oldStr.split("\n").map((line, i) => (
                <div key={`old-${i}`} className="bg-red-900/30 text-red-300 px-2">
                  - {line}
                </div>
              ))}
              {newStr.split("\n").map((line, i) => (
                <div key={`new-${i}`} className="bg-green-900/30 text-green-300 px-2">
                  + {line}
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    )
  }

  // For write tool calls, show the path and content preview
  if (name === "write") {
    const path = args.path as string || ""
    const content = args.content as string || ""
    const bytesWritten = result.bytes_written as number | undefined
    const created = result.created as boolean | undefined
    const filename = path.split("/").pop() || path

    return (
      <div className="mx-4 my-1.5 rounded-lg border border-zinc-700 bg-zinc-900/50">
        <button
          className="flex w-full items-center gap-2 px-3 py-1.5 text-left"
          onClick={() => setExpanded(!expanded)}
        >
          <span className="rounded bg-green-700/60 px-1.5 py-0.5 font-mono text-xs text-white">write</span>
          <span className="text-xs text-zinc-300 font-mono truncate">{filename}</span>
          {created && <span className="text-xs text-green-400">created</span>}
          {bytesWritten !== undefined && <span className="text-xs text-zinc-500">{bytesWritten}B</span>}
          {durationMs !== undefined && <span className="text-xs text-zinc-600">{durationMs}ms</span>}
          <span className="ml-auto text-xs text-zinc-500">{expanded ? "▼" : "▶"}</span>
        </button>
        {expanded && (
          <div className="border-t border-zinc-800 px-3 py-2">
            {path !== filename && <div className="text-xs text-zinc-500 mb-1">{path}</div>}
            <pre className="font-mono text-xs text-zinc-300 whitespace-pre-wrap max-h-60 overflow-y-auto">{content}</pre>
          </div>
        )}
      </div>
    )
  }

  // For bash tool calls, show the command and output
  if (name === "bash") {
    const command = args.command as string || ""
    const stdout = result.stdout as string || ""
    const stderr = result.stderr as string || ""
    const exitCode = result.exit_code as number | undefined

    return (
      <div className="mx-4 my-1.5 rounded-lg border border-zinc-700 bg-zinc-900/50">
        <button
          className="flex w-full items-center gap-2 px-3 py-1.5 text-left"
          onClick={() => setExpanded(!expanded)}
        >
          <span className="rounded bg-zinc-600 px-1.5 py-0.5 font-mono text-xs text-white">bash</span>
          <span className="text-xs text-zinc-300 font-mono truncate">{command.slice(0, 80)}{command.length > 80 ? "…" : ""}</span>
          {exitCode !== undefined && (
            <span className={`text-xs ${exitCode === 0 ? "text-green-500" : "text-red-400"}`}>exit {exitCode}</span>
          )}
          {durationMs !== undefined && <span className="text-xs text-zinc-600">{durationMs}ms</span>}
          <span className="ml-auto text-xs text-zinc-500">{expanded ? "▼" : "▶"}</span>
        </button>
        {expanded && (
          <div className="border-t border-zinc-800 px-3 py-2">
            <div className="font-mono text-xs text-zinc-500 mb-1">$ {command}</div>
            {stdout && <pre className="font-mono text-xs text-zinc-300 whitespace-pre-wrap max-h-60 overflow-y-auto">{stdout}</pre>}
            {stderr && <pre className="font-mono text-xs text-red-400 whitespace-pre-wrap max-h-40 overflow-y-auto">{stderr}</pre>}
          </div>
        )}
      </div>
    )
  }

  // For read tool calls, show the path
  if (name === "read") {
    const path = args.path as string || ""
    const filename = path.split("/").pop() || path
    return (
      <div className="mx-4 my-1 flex items-center gap-2 px-1 py-0.5 text-xs text-zinc-400">
        <span className="rounded bg-blue-700/40 px-1.5 py-0.5 font-mono text-white">read</span>
        <span className="font-mono truncate">{filename}</span>
      </div>
    )
  }

  // Default: compact tool card
  return (
    <div className="mx-4 my-1.5 rounded-lg border border-zinc-700 bg-zinc-900/50">
      <button
        className="flex w-full items-center gap-2 px-3 py-1.5 text-left"
        onClick={() => setExpanded(!expanded)}
      >
        <span className="rounded bg-zinc-600 px-1.5 py-0.5 font-mono text-xs text-white">{name}</span>
        <span className="text-xs text-zinc-300 truncate">{summary}</span>
        {durationMs !== undefined && <span className="text-xs text-zinc-600">{durationMs}ms</span>}
        <span className="ml-auto text-xs text-zinc-500">{expanded ? "▼" : "▶"}</span>
      </button>
      {expanded && (
        <div className="border-t border-zinc-800 px-3 py-2">
          {Object.keys(args).length > 0 && (
            <>
              <div className="text-xs text-zinc-500 mb-1">Args</div>
              <pre className="font-mono text-xs text-zinc-300 whitespace-pre-wrap max-h-40 overflow-y-auto">{JSON.stringify(args, null, 2)}</pre>
            </>
          )}
          {Object.keys(result).length > 0 && (
            <>
              <div className="text-xs text-zinc-500 mt-2 mb-1">Result</div>
              <pre className="font-mono text-xs text-zinc-300 whitespace-pre-wrap max-h-40 overflow-y-auto">{JSON.stringify(result, null, 2)}</pre>
            </>
          )}
        </div>
      )}
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
