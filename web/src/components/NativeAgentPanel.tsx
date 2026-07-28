import { useState, useEffect, useCallback, useRef } from "react"
import { ArrowDown, ArrowLeftRight, Zap, MoreHorizontal, Unplug, Skull, Bell, BellOff } from "lucide-react"
import { useWebSocket, type WSMessage } from "../hooks/useWebSocket"
import { useServer } from "../hooks/useServer"
import { apiFetch } from "../lib/api"
import { VoiceRecorder, type VoiceRecorderHandle } from "./VoiceRecorder"

function formatTokens(value: number): string {
  if (value < 1_000) return value.toLocaleString()
  if (value < 1_000_000) return `${Number((value / 1_000).toFixed(1))}k`
  return `${Number((value / 1_000_000).toFixed(2))}M`
}

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
  content: string | Array<{ type: string; text?: string; toolCallId?: string; toolName?: string; input?: unknown; output?: unknown }>
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
  contextWindow?: number
  usedContextTokens?: number
  tasks?: { content: string; status: string }[]
}

interface ModeInfo {
  name: string
  description: string
  cycle: boolean
}

interface CommandInfo {
  name: string
  description?: string
  body: string
  path: string
  source: string
}

interface SkillInfo {
  name: string
  description: string
  path: string
  source: string
}

type SlashSuggestion = {
  type: "command" | "skill"
  name: string
  description: string
  body?: string
}

interface NativeAgentPanelProps {
  agentId: string
  onClickTerminal?: () => void
  onKill?: () => void
  onDisconnect?: () => void
  ntfyConfigured?: boolean
  notifyEnabled?: boolean
  onToggleNotify?: () => void
  voiceEnabled?: boolean
  agentKind?: string
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

export function NativeAgentPanel({ agentId, onKill, onDisconnect, ntfyConfigured, notifyEnabled, onToggleNotify, voiceEnabled, agentKind }: NativeAgentPanelProps) {
  const { send, subscribe, connected } = useWebSocket()
  const { baseUrl } = useServer()
  const [items, setItems] = useState<ConversationItem[]>([])
  const [draft, setDraft] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [contextUsage, setContextUsage] = useState<{ used: number; window: number } | null>(null)
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
  const [modes, setModes] = useState<ModeInfo[]>([])
  const [modesOpen, setModesOpen] = useState(false)
  const [macros, setMacros] = useState<{ name: string; keys: string }[]>([])
  const [macrosOpen, setMacrosOpen] = useState(false)
  const [menuOpen, setMenuOpen] = useState(false)
  const modesRef = useRef<HTMLDivElement>(null)
  const macrosRef = useRef<HTMLDivElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  const voiceRef = useRef<VoiceRecorderHandle>(null)
  const [commands, setCommands] = useState<CommandInfo[]>([])
  const [skills, setSkills] = useState<SkillInfo[]>([])
  const [slashOpen, setSlashOpen] = useState(false)
  const [slashIndex, setSlashIndex] = useState(0)
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  // Load/save draft
  useEffect(() => {
    const saved = localStorage.getItem(draftKey)
    if (saved) setDraft(saved)
  }, [draftKey])
  useEffect(() => {
    localStorage.setItem(draftKey, draft)
  }, [draftKey, draft])

  // Fetch available modes
  useEffect(() => {
    apiFetch(baseUrl, `/api/agents/${agentId}/chimera/modes`)
      .then((r) => r.ok ? r.json() : [])
      .then((data: ModeInfo[]) => setModes(Array.isArray(data) ? data : []))
      .catch(() => {})
  }, [agentId, baseUrl])

  // Fetch commands and skills for slash autocomplete
  useEffect(() => {
    Promise.all([
      apiFetch(baseUrl, `/api/agents/${agentId}/chimera/commands`).then((r) => r.ok ? r.json() : []).catch(() => []),
      apiFetch(baseUrl, `/api/agents/${agentId}/chimera/skills`).then((r) => r.ok ? r.json() : []).catch(() => []),
    ]).then(([cmds, skls]) => {
      setCommands(Array.isArray(cmds) ? cmds : [])
      setSkills(Array.isArray(skls) ? skls : [])
    })
  }, [agentId, baseUrl])

  // Compute slash suggestions from current draft
  const slashSuggestions: SlashSuggestion[] = (() => {
    if (!draft.startsWith("/")) return []
    const query = draft.slice(1).toLowerCase()
    const cmdSuggestions: SlashSuggestion[] = commands
      .filter((c) => c.name.toLowerCase().includes(query))
      .map((c) => ({ type: "command" as const, name: c.name, description: c.description || "", body: c.body }))
    const skillSuggestions: SlashSuggestion[] = skills
      .filter((s) => s.name.toLowerCase().includes(query))
      .map((s) => ({ type: "skill" as const, name: s.name, description: s.description }))
    return [...cmdSuggestions, ...skillSuggestions]
  })()

  // Open/close slash menu based on draft
  useEffect(() => {
    if (draft.startsWith("/") && slashSuggestions.length > 0) {
      setSlashOpen(true)
      setSlashIndex(0)
    } else {
      setSlashOpen(false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [draft])

  // Fetch macros
  useEffect(() => {
    if (!macrosOpen) return
    apiFetch(baseUrl, "/api/macros")
      .then((r) => r.ok ? r.json() : { macros: [] })
      .then((data: { macros?: { name: string; keys: string }[] }) => {
        if (Array.isArray(data.macros)) setMacros(data.macros)
      })
      .catch(() => setMacros([]))
  }, [baseUrl, macrosOpen])

  // Close dropdowns on outside click
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (modesRef.current && !modesRef.current.contains(e.target as Node)) setModesOpen(false)
      if (macrosRef.current && !macrosRef.current.contains(e.target as Node)) setMacrosOpen(false)
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) setMenuOpen(false)
    }
    document.addEventListener("mousedown", handler)
    return () => document.removeEventListener("mousedown", handler)
  }, [])

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
        if (data.contextWindow && data.usedContextTokens != null) {
          setContextUsage({ used: data.usedContextTokens, window: data.contextWindow })
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

        // Walk messages and interleave text and tool calls in conversation
        // order. Assistant messages contain tool-call parts inline; tool
        // result messages contain tool-result parts. We match tool-call
        // parts to the flat toolCalls[] array by chronological order.
        let tcIdx = 0 // index into the flat toolCalls array
        for (let i = 0; i < msgs.length; i++) {
          const msg = msgs[i]
          // Skip the system/role prompt (first user message)
          if (i === 0 && msg.role === "user") continue

          if (typeof msg.content === "string") {
            if (msg.content) {
              snapshotItems.push({
                key: `snap-msg-${i}`,
                role: msg.role as ItemRole,
                text: msg.content,
              })
            }
            continue
          }

          if (!Array.isArray(msg.content)) continue

          // Extract text parts as a single assistant/user message
          const textParts = msg.content.filter((p) => p.type === "text" && p.text)
          if (textParts.length > 0) {
            snapshotItems.push({
              key: `snap-msg-${i}`,
              role: msg.role as ItemRole,
              text: textParts.map((p) => p.text!).join(""),
            })
          }

          // Extract tool-call parts and insert as tool cards inline
          const toolCallCount = msg.content.filter((p) => p.type === "tool-call").length
          for (let _ = 0; _ < toolCallCount; _++) {
            const tc = tcIdx < tcs.length ? tcs[tcIdx] : undefined
            if (tc) {
              if (tc.callId) seenCallIds.current.add(tc.callId)
              snapshotItems.push({
                key: `snap-tc-${tcIdx}`,
                role: "tool" as ItemRole,
                text: "",
                tool: {
                  key: tc.callId || `snap-tc-${tcIdx}`,
                  toolName: tc.name,
                  args: tc.args as Record<string, unknown>,
                  result: tc.result as Record<string, unknown> | undefined,
                  summary: tc.display?.summary,
                  durationMs: tc.endedAt && tc.startedAt
                    ? tc.endedAt - tc.startedAt
                    : undefined,
                },
              })
              tcIdx++
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
              const callKey = env.callId || env.eventId
              setItems((prev) => [...prev, {
                key: callKey,
                role: "tool",
                text: "",
                tool: {
                  key: callKey,
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
            const update = env as unknown as { contextWindow?: number; usedContextTokens?: number }
            if (update.contextWindow && update.usedContextTokens != null) {
              setContextUsage({ used: update.usedContextTokens, window: update.contextWindow })
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

  const handleSlashSelect = useCallback((suggestion: SlashSuggestion) => {
    if (suggestion.type === "command" && suggestion.body) {
      // Expand the command body into the draft and send immediately
      setDraft(suggestion.body)
      setSlashOpen(false)
      // Auto-send the expanded command
      send({ type: "agent_message", agent_id: agentId, content: suggestion.body })
      setDraft("")
    } else {
      // For skills, insert the skill name and let the user continue typing
      setDraft(`/${suggestion.name} `)
      setSlashOpen(false)
      textareaRef.current?.focus()
    }
  }, [agentId, send])

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (slashOpen) {
        if (e.key === "ArrowDown") {
          e.preventDefault()
          setSlashIndex((i) => Math.min(i + 1, slashSuggestions.length - 1))
          return
        }
        if (e.key === "ArrowUp") {
          e.preventDefault()
          setSlashIndex((i) => Math.max(i - 1, 0))
          return
        }
        if (e.key === "Enter" && !e.shiftKey) {
          e.preventDefault()
          handleSlashSelect(slashSuggestions[slashIndex])
          return
        }
        if (e.key === "Escape") {
          e.preventDefault()
          setSlashOpen(false)
          return
        }
      }
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault()
        handleSend()
      }
    },
    [handleSend, slashOpen, slashIndex, slashSuggestions, handleSlashSelect],
  )

  const scrollToBottom = useCallback(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
      setIsScrolledUp(false)
    }
  }, [])

  const handleModeChange = useCallback((mode: string) => {
    apiFetch(baseUrl, `/api/agents/${agentId}/chimera/modes`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ mode }),
    }).then(() => setMode(mode)).catch(() => {})
    setModesOpen(false)
  }, [agentId, baseUrl])

  const handleMacro = useCallback((keys: string) => {
    // For native sessions, macros send text via the message API
    send({ type: "agent_message", agent_id: agentId, content: keys })
    setMacrosOpen(false)
  }, [agentId, send])

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
        {contextUsage && (
          <span>
            {formatTokens(contextUsage.used)} / {formatTokens(contextUsage.window)} ({Math.round((contextUsage.used / contextUsage.window) * 100)}%)
          </span>
        )}
        {error && <span className="text-red-400">{error}</span>}
        {/* Action buttons */}
        <div className="ml-auto flex items-center gap-1.5 shrink-0">
          {/* Mode selector */}
          {modes.length > 0 && (
            <div className="relative" ref={modesRef}>
              <button
                onClick={() => setModesOpen((v) => !v)}
                className="flex items-center gap-1 rounded px-2 py-1 text-xs text-zinc-400 border border-zinc-700 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
                title="Change mode"
              >
                <ArrowLeftRight size={12} />
                <span>Mode</span>
              </button>
              {modesOpen && (
                <div className="absolute bottom-full right-0 mb-1 rounded border border-zinc-700 bg-zinc-900 shadow-xl py-1 min-w-[160px] z-10">
                  {modes.map((m) => (
                    <button
                      key={m.name}
                      onClick={() => handleModeChange(m.name)}
                      className={"flex w-full items-center gap-2 px-3 py-1.5 text-xs transition-colors " + (m.name === mode ? "text-zinc-200 bg-zinc-800" : "text-zinc-300 hover:bg-zinc-800")}
                    >
                      <span className="font-mono">{m.name}</span>
                      {m.description && <span className="text-zinc-500 truncate">{m.description}</span>}
                    </button>
                  ))}
                </div>
              )}
            </div>
          )}
          {/* Macros dropdown */}
          <div className="relative" ref={macrosRef}>
            <button
              onClick={() => setMacrosOpen((v) => !v)}
              className={"flex items-center gap-1 rounded px-2 py-1 text-xs border transition-colors " + (macrosOpen ? "text-zinc-200 bg-zinc-800 border-zinc-600" : "text-zinc-400 border-zinc-700 hover:bg-zinc-800 hover:text-zinc-200")}
              title="Macros"
            >
              <Zap size={12} />
              <span>Macros</span>
            </button>
            {macrosOpen && (
              <div className="absolute bottom-full right-0 mb-1 rounded border border-zinc-700 bg-zinc-900 shadow-xl py-1 min-w-[180px] z-10">
                {macros.length === 0 ? (
                  <div className="px-3 py-1.5 text-xs text-zinc-500">(no macros configured)</div>
                ) : (
                  macros.map((macro) => (
                    <button
                      key={macro.name}
                      onClick={() => handleMacro(macro.keys)}
                      className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-zinc-300 hover:bg-zinc-800 transition-colors"
                    >
                      <Zap size={12} />
                      {macro.name}
                    </button>
                  ))
                )}
              </div>
            )}
          </div>
          {/* Overflow menu */}
          <div className="relative" ref={menuRef}>
            <button
              onClick={() => setMenuOpen((v) => !v)}
              className={"flex items-center gap-1 rounded px-2 py-1 text-xs border transition-colors " + (menuOpen ? "text-zinc-200 bg-zinc-800 border-zinc-600" : "text-zinc-400 border-zinc-700 hover:bg-zinc-800 hover:text-zinc-200")}
              title="More actions"
            >
              <MoreHorizontal size={12} />
            </button>
            {menuOpen && (
              <div className="absolute bottom-full right-0 mb-1 rounded border border-zinc-700 bg-zinc-900 shadow-xl py-1 min-w-[180px] z-10">
                {ntfyConfigured && onToggleNotify && (
                  <button
                    onClick={() => { onToggleNotify(); setMenuOpen(false) }}
                    className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-zinc-300 hover:bg-zinc-800 transition-colors"
                  >
                    {notifyEnabled ? <Bell size={12} /> : <BellOff size={12} />}
                    {notifyEnabled ? "Disable notify" : "Enable notify"}
                  </button>
                )}
                {onDisconnect && (
                  <button
                    onClick={() => { onDisconnect(); setMenuOpen(false) }}
                    className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-zinc-300 hover:bg-zinc-800 transition-colors"
                  >
                    <Unplug size={12} />
                    Disconnect
                  </button>
                )}
                {onKill && (
                  <>
                    <div className="my-1 border-t border-zinc-800" />
                    <button
                      onClick={() => { onKill(); setMenuOpen(false) }}
                      className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-red-400 hover:bg-red-950 transition-colors"
                    >
                      <Skull size={12} />
                      Kill agent
                    </button>
                  </>
                )}
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Composer */}
      <div className="relative border-t border-zinc-800 p-3">
        {slashOpen && (
          <div className="absolute bottom-full left-3 right-3 mb-1 max-h-48 overflow-y-auto rounded-lg border border-zinc-700 bg-zinc-900 shadow-xl z-20">
            {slashSuggestions.map((s, i) => (
              <button
                key={s.type + ":" + s.name}
                onMouseDown={(e) => { e.preventDefault(); handleSlashSelect(s) }}
                onMouseEnter={() => setSlashIndex(i)}
                className={"flex w-full items-center gap-2 px-3 py-2 text-left text-sm transition-colors " + (i === slashIndex ? "bg-zinc-800 text-zinc-100" : "text-zinc-300 hover:bg-zinc-800")}
              >
                <span className={"shrink-0 rounded px-1 text-[10px] font-mono uppercase " + (s.type === "command" ? "bg-indigo-900/60 text-indigo-300" : "bg-amber-900/60 text-amber-300")}>
                  {s.type === "command" ? "cmd" : "skill"}
                </span>
                <span className="font-mono text-zinc-200">{s.name}</span>
                {s.description && <span className="text-zinc-500 truncate">{s.description}</span>}
              </button>
            ))}
          </div>
        )}
        <div className="flex items-end gap-2">
          <textarea
            ref={textareaRef}
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
            {busy ? (
              <button
                onClick={handleInterrupt}
                className="rounded-lg bg-red-700/60 px-4 py-2 text-xs text-white hover:bg-red-600/60"
              >
                Interrupt
              </button>
            ) : voiceEnabled ? (
              <VoiceRecorder
                ref={voiceRef}
                agentId={agentId}
                agentKind={agentKind}
                baseUrl={baseUrl}
                transcriptionOnly
                onTranscription={(text) => {
                  setDraft((prev) => prev ? prev + " " + text : text)
                }}
              />
            ) : null}
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


