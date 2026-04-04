import { useState, useRef, useEffect, useCallback, useImperativeHandle, forwardRef, type KeyboardEvent } from "react"
import type { PromptState } from "../hooks/useWebSocket"
import { cn } from "../lib/utils"
import { Send, Square, ArrowLeftRight, X, ScanSearch, Unplug, Skull, MoreHorizontal, RefreshCw, Eye, EyeOff, Terminal } from "lucide-react"

interface ActionListProps {
  actions: { label: string; keys: string }[]
  type: string
  onSelect: (keys: string) => void
  onDismiss: () => void
}

function ActionList({ actions, type, onSelect, onDismiss }: ActionListProps) {
  const [selected, setSelected] = useState(0)
  const listRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    listRef.current?.focus()
    setSelected(0)
  }, [type])

  const handleKey = useCallback((e: KeyboardEvent) => {
    switch (e.key) {
      case "ArrowDown":
      case "j":
        e.preventDefault()
        setSelected((s) => Math.min(s + 1, actions.length - 1))
        break
      case "ArrowUp":
      case "k":
        e.preventDefault()
        setSelected((s) => Math.max(s - 1, 0))
        break
      case "Enter":
        e.preventDefault()
        onSelect(actions[selected].keys)
        break
      case "Escape":
        e.preventDefault()
        onDismiss()
        break
    }
  }, [actions, selected, onSelect, onDismiss])

  const actionColor = (label: string) => {
    if (label === "Yes" || label === "Accept" || label === "Always") return "text-emerald-400"
    if (label === "No" || label === "Reject") return "text-red-400"
    return "text-indigo-400"
  }

  return (
    <div
      ref={listRef}
      tabIndex={0}
      onKeyDown={handleKey}
      className="flex flex-col gap-0.5 outline-none"
    >
      {actions.map((action, i) => (
        <button
          key={action.label}
          onClick={() => onSelect(action.keys)}
          className={cn(
            "flex items-center gap-2 rounded px-3 py-1.5 text-sm text-left transition-colors",
            i === selected
              ? "bg-zinc-800 text-zinc-100"
              : "text-zinc-400 hover:bg-zinc-800/50 hover:text-zinc-200"
          )}
        >
          <span className={cn("font-medium", actionColor(action.label))}>
            {action.label}
          </span>
          {i === selected && (
            <span className="ml-auto text-[10px] text-zinc-600">↵</span>
          )}
        </button>
      ))}
      <button
        onClick={onDismiss}
        className={cn(
          "flex items-center gap-2 rounded px-3 py-1.5 text-sm text-left transition-colors text-zinc-600 hover:bg-zinc-800/50 hover:text-zinc-400"
        )}
      >
        <X size={12} />
        Dismiss
      </button>
    </div>
  )
}

interface PromptBarProps {
  promptState: PromptState | null
  onSendKeys: (keys: string) => void
  onSubmitText: (keys: string) => void
  onDismissPrompt: () => void
  onDetectPrompt: () => void
  onDisconnect: () => void
  onKill: () => void
  onRefresh: () => void
  onTogglePromptDetection: () => void
  promptDetectionEnabled: boolean
  agentId: string
  agentTitle?: string
  agentActivity?: string
  agentCommand?: string
  connected?: boolean
}

function draftKey(id: string) { return `legato:draft:${id}` }

export interface PromptBarHandle {
  focus: () => void
}

export const PromptBar = forwardRef<PromptBarHandle, PromptBarProps>(function PromptBar({ promptState, onSendKeys, onSubmitText, onDismissPrompt, onDetectPrompt, onDisconnect, onKill, onRefresh, onTogglePromptDetection, promptDetectionEnabled, agentId, agentTitle, agentActivity, agentCommand, connected }, ref) {
  const [input, setInput] = useState(() => localStorage.getItem(draftKey(agentId)) ?? "")
  const [menuOpen, setMenuOpen] = useState(false)
  const menuRef = useRef<HTMLDivElement>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const inputRef = useRef(input)
  const prevAgentIdRef = useRef(agentId)

  useImperativeHandle(ref, () => ({
    focus: () => textareaRef.current?.focus(),
  }))

  // Sync textarea height whenever input changes (submit clear, agent switch, typing).
  useEffect(() => {
    const el = textareaRef.current
    if (el) {
      el.style.height = "auto"
      el.style.height = Math.min(el.scrollHeight, 5 * 24 + 12) + "px"
    }
  }, [input])

  // Keep inputRef in sync for use in the agentId change effect.
  inputRef.current = input

  // On agent switch: save old draft, load new draft.
  useEffect(() => {
    if (prevAgentIdRef.current !== agentId) {
      const old = inputRef.current
      if (old) {
        localStorage.setItem(draftKey(prevAgentIdRef.current), old)
      } else {
        localStorage.removeItem(draftKey(prevAgentIdRef.current))
      }
      prevAgentIdRef.current = agentId
      setInput(localStorage.getItem(draftKey(agentId)) ?? "")
    }
  }, [agentId])

  const handleInputChange = (value: string) => {
    setInput(value)
    if (value) {
      localStorage.setItem(draftKey(agentId), value)
    } else {
      localStorage.removeItem(draftKey(agentId))
    }
  }

  // Close menu on outside click.
  useEffect(() => {
    if (!menuOpen) return
    const onClick = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setMenuOpen(false)
      }
    }
    document.addEventListener("click", onClick, true)
    return () => document.removeEventListener("click", onClick, true)
  }, [menuOpen])

  const handleSubmit = () => {
    const trimmed = input.trim()
    if (trimmed) {
      onSubmitText(trimmed + "\n")
      setInput("")
      localStorage.removeItem(draftKey(agentId))
    }
  }

  const handleKeyDown = (e: KeyboardEvent) => {
    // When input is empty, pass navigation keys through to the terminal
    // so the user can arrow through choices and press Enter to select.
    if (!input) {
      const passthrough: Record<string, string> = {
        ArrowUp: "Up",
        ArrowDown: "Down",
        Enter: "Enter",
        Escape: "Escape",
        Tab: "Tab",
      }
      const tmuxKey = passthrough[e.key]
      if (tmuxKey) {
        e.preventDefault()
        onSendKeys(tmuxKey)
        return
      }
    }

    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault()
      handleSubmit()
    }
  }

  const isWorking = agentActivity === "working"
  const isWaiting = agentActivity === "waiting"
  // Only show detected prompt buttons when the agent is waiting for input.
  // This avoids false positives from pattern-matching mid-output.
  const type = isWaiting ? (promptState?.type ?? null) : null

  // Claude-specific: "! " prefix runs a bash command.
  const isBashMode = agentCommand === "claude" && input.startsWith("! ")

  return (
    <div className="border-t border-zinc-800 bg-zinc-950 px-4 py-3">
      <div className="flex items-center justify-between gap-2">
        {/* Left: title */}
        {agentTitle && (
          <div className="text-xs text-zinc-500 truncate">{agentTitle}</div>
        )}

        {/* Right: action buttons */}
        <div className="ml-auto flex items-center gap-1.5 shrink-0">
          <button
            onClick={() => onSendKeys("BTab")}
            className="flex items-center gap-1 rounded px-2 py-1 text-xs text-zinc-400 border border-zinc-700 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
            title="Cycle Claude mode (Shift+Tab)"
          >
            <ArrowLeftRight size={12} />
            <span>Mode</span>
          </button>
          {isWorking && (
            <button
              onClick={() => onSendKeys("Escape")}
              className="flex items-center gap-1 rounded px-2 py-1 text-xs text-red-400 border border-red-900 transition-colors hover:bg-red-950 hover:text-red-300"
              title="Stop (Escape)"
            >
              <Square size={12} />
              <span>Stop</span>
            </button>
          )}
          {/* Overflow menu */}
          <div className="relative" ref={menuRef}>
            <button
              onClick={() => setMenuOpen((v) => !v)}
              className={cn(
                "flex items-center gap-1 rounded px-2 py-1 text-xs border transition-colors",
                menuOpen
                  ? "text-zinc-200 bg-zinc-800 border-zinc-600"
                  : "text-zinc-400 border-zinc-700 hover:bg-zinc-800 hover:text-zinc-200"
              )}
              title="More actions"
            >
              <MoreHorizontal size={12} />
            </button>
            {menuOpen && (
              <div className="absolute bottom-full right-0 mb-1 rounded border border-zinc-700 bg-zinc-900 shadow-xl py-1 min-w-[180px] z-10">
                <button
                  onClick={() => { onRefresh(); setMenuOpen(false) }}
                  className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-zinc-300 hover:bg-zinc-800 transition-colors"
                >
                  <RefreshCw size={12} />
                  Refresh terminal
                </button>
                <button
                  onClick={() => { onDetectPrompt(); setMenuOpen(false) }}
                  className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-zinc-300 hover:bg-zinc-800 transition-colors"
                >
                  <ScanSearch size={12} />
                  Re-detect prompt
                </button>
                <button
                  onClick={() => { onTogglePromptDetection(); setMenuOpen(false) }}
                  className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-zinc-300 hover:bg-zinc-800 transition-colors"
                >
                  {promptDetectionEnabled ? <EyeOff size={12} /> : <Eye size={12} />}
                  {promptDetectionEnabled ? "Disable prompt detection" : "Enable prompt detection"}
                </button>
                <button
                  onClick={() => { onDisconnect(); setMenuOpen(false) }}
                  className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-zinc-300 hover:bg-zinc-800 transition-colors"
                >
                  <Unplug size={12} />
                  Disconnect
                </button>
                <div className="my-1 border-t border-zinc-800" />
                <button
                  onClick={() => { onKill(); setMenuOpen(false) }}
                  className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-red-400 hover:bg-red-950 transition-colors"
                >
                  <Skull size={12} />
                  Kill agent
                </button>
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Prompt-specific controls */}
      <div className="mt-2">
        {(type === "tool_approval" || type === "plan_approval") && promptState?.actions && (
          <ActionList
            actions={promptState.actions}
            type={type}
            onSelect={(keys) => { onSendKeys(keys); onDismissPrompt() }}
            onDismiss={onDismissPrompt}
          />
        )}

        {(type === "free_text" || type === null) && (
          <div className="flex items-end gap-2">
            <div className="relative flex-1">
              {isBashMode && (
                <div className="absolute left-2.5 top-1.5 flex items-center gap-1 text-amber-400 pointer-events-none">
                  <Terminal size={13} />
                  <span className="text-[10px] font-semibold uppercase tracking-wide">bash</span>
                </div>
              )}
              <textarea
                ref={textareaRef}
                value={input}
                onChange={(e) => handleInputChange(e.target.value)}
                onKeyDown={handleKeyDown}
                placeholder={connected === false ? "Disconnected..." : "Type a message..."}
                disabled={connected === false}
                rows={1}
                className={cn(
                  "w-full rounded bg-zinc-900 border px-3 py-1.5 text-sm text-zinc-200 placeholder:text-zinc-600 outline-none disabled:opacity-50 resize-none leading-6 transition-colors",
                  isBashMode
                    ? "border-amber-500/60 focus:border-amber-400 pt-7"
                    : "border-zinc-700 focus:border-indigo-500"
                )}
                autoFocus
              />
            </div>
            <button
              onClick={handleSubmit}
              disabled={!input.trim() || connected === false}
              className="rounded bg-indigo-600 p-1.5 text-white transition-colors hover:bg-indigo-500 disabled:opacity-40 disabled:hover:bg-indigo-600 mb-0.5"
            >
              <Send size={16} />
            </button>
          </div>
        )}
      </div>
    </div>
  )
})
