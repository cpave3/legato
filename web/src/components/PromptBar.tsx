import { useState, useRef, useEffect, type KeyboardEvent } from "react"
import type { PromptState } from "../hooks/useWebSocket"
import { cn } from "../lib/utils"
import { Send, Square, ArrowLeftRight, X, ScanSearch, Unplug, Skull, MoreHorizontal } from "lucide-react"

interface PromptBarProps {
  promptState: PromptState | null
  onSendKeys: (keys: string) => void
  onDismissPrompt: () => void
  onDetectPrompt: () => void
  onDisconnect: () => void
  onKill: () => void
  agentTitle?: string
  agentActivity?: string
}

export function PromptBar({ promptState, onSendKeys, onDismissPrompt, onDetectPrompt, onDisconnect, onKill, agentTitle, agentActivity }: PromptBarProps) {
  const [input, setInput] = useState("")
  const [menuOpen, setMenuOpen] = useState(false)
  const menuRef = useRef<HTMLDivElement>(null)

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
      onSendKeys(trimmed + "\n")
      setInput("")
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
  const type = promptState?.type ?? null

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
              <div className="absolute bottom-full right-0 mb-1 rounded border border-zinc-700 bg-zinc-900 shadow-xl py-1 min-w-[160px] z-10">
                <button
                  onClick={() => { onDetectPrompt(); setMenuOpen(false) }}
                  className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-zinc-300 hover:bg-zinc-800 transition-colors"
                >
                  <ScanSearch size={12} />
                  Re-detect prompt
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
        {type === "tool_approval" && promptState?.actions && (
          <div className="flex items-center gap-2">
            {promptState.actions.map((action) => (
              <button
                key={action.label}
                onClick={() => { onSendKeys(action.keys); onDismissPrompt() }}
                className={cn(
                  "rounded px-3 py-1.5 text-sm font-medium transition-colors",
                  action.label === "Yes"
                    ? "bg-emerald-600 text-white hover:bg-emerald-500"
                    : action.label === "No"
                      ? "bg-zinc-700 text-zinc-200 hover:bg-zinc-600"
                      : "bg-indigo-600 text-white hover:bg-indigo-500"
                )}
              >
                {action.label}
              </button>
            ))}
            <button
              onClick={onDismissPrompt}
              className="rounded p-1.5 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-300"
              title="Dismiss — show text input instead"
            >
              <X size={14} />
            </button>
          </div>
        )}

        {type === "plan_approval" && promptState?.actions && (
          <div className="flex items-center gap-2">
            {promptState.actions.map((action) => (
              <button
                key={action.label}
                onClick={() => { onSendKeys(action.keys); onDismissPrompt() }}
                className={cn(
                  "rounded px-3 py-1.5 text-sm font-medium transition-colors",
                  action.label === "Accept"
                    ? "bg-emerald-600 text-white hover:bg-emerald-500"
                    : "bg-zinc-700 text-zinc-200 hover:bg-zinc-600"
                )}
              >
                {action.label}
              </button>
            ))}
            <button
              onClick={onDismissPrompt}
              className="rounded p-1.5 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-300"
              title="Dismiss — show text input instead"
            >
              <X size={14} />
            </button>
          </div>
        )}

        {(type === "free_text" || type === null) && (
          <div className="flex items-center gap-2">
            <input
              type="text"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder="Type a message..."
              className="flex-1 rounded bg-zinc-900 border border-zinc-700 px-3 py-1.5 text-sm text-zinc-200 placeholder:text-zinc-600 outline-none focus:border-indigo-500"
              autoFocus
            />
            <button
              onClick={handleSubmit}
              disabled={!input.trim()}
              className="rounded bg-indigo-600 p-1.5 text-white transition-colors hover:bg-indigo-500 disabled:opacity-40 disabled:hover:bg-indigo-600"
            >
              <Send size={16} />
            </button>
          </div>
        )}
      </div>
    </div>
  )
}
