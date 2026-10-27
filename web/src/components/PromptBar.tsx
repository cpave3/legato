import { useState, useRef, useEffect, useCallback, useImperativeHandle, forwardRef, type KeyboardEvent } from "react"
import type { PromptState } from "../hooks/useWebSocket"
import { useServer } from "../hooks/useServer"
import { usePastedImages } from "../hooks/usePastedImages"
import { insertAtCursor } from "../lib/agentImage"
import { PastedImageStrip } from "./PastedImageStrip"
import { useMacroHotkeys } from "../hooks/useMacroHotkeys"
import { apiFetch } from "../lib/api"
import { cn } from "../lib/utils"
import { ArrowLeftRight, X, Terminal, Zap } from "lucide-react"
import { useVoiceRecorder } from "../hooks/useVoiceRecorder"
import { DictateButton, DictationBar, SendButton, WorkingStrip } from "./Composer"
import { composerToolButton, dictating } from "../lib/composer"
import { AgentKindBadge } from "./AgentKindBadge"
import { appendProposal, onComposerProposal } from "../lib/agentUi"
import { AgentMoreMenu, SidePanelSwitch, type SidePanelsProps } from "./AgentChatHeader"
import { useTerminalMenu } from "../hooks/useTerminalMenu"

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
  onSubmitMessage: (content: string) => void
  onDismissPrompt: () => void
  onDetectPrompt: () => void
  onDisconnect: () => void
  onKill: () => void
  // Stops the agent and starts it again, resuming its conversation.
  onReload?: () => void
  onRefresh: () => void
  onTogglePromptDetection: () => void
  promptDetectionEnabled: boolean
  agentId: string
  agentTitle?: string
  agentActivity?: string
  agentCommand?: string
  agentKind?: string
  connected?: boolean
  ntfyConfigured?: boolean
  notifyEnabled?: boolean
  onToggleNotify?: () => void
  voiceEnabled?: boolean
  onUpgradeChimera?: () => Promise<void>
  // What the column beside the session can show, when the bar is its only
  // toolbar.
  panels?: SidePanelsProps
  // The page's header above the terminal already names the agent and holds
  // its side panels and More actions menu (useTerminalMenu); the bar keeps
  // only the message box and the controls under it.
  menuInHeader?: boolean
}

function draftKey(id: string) { return `legato:draft:${id}` }

export interface PromptBarHandle {
  focus: () => void
  voiceStart: () => void
  voiceSend: () => void
  voiceCancel: () => void
  voiceIsRecording: () => boolean
}

export const PromptBar = forwardRef<PromptBarHandle, PromptBarProps>(function PromptBar({ promptState, onSendKeys, onSubmitMessage, onDismissPrompt, onDetectPrompt, onDisconnect, onKill, onReload, onRefresh, onTogglePromptDetection, promptDetectionEnabled, agentId, agentTitle, agentActivity, agentCommand, agentKind, connected, ntfyConfigured, notifyEnabled, onToggleNotify, voiceEnabled, onUpgradeChimera, panels, menuInHeader = false }, ref) {
  const { baseUrl } = useServer()
  const [input, setInput] = useState(() => localStorage.getItem(draftKey(agentId)) ?? "")
  const terminalMenu = useTerminalMenu({ agentKind, onUpgradeChimera, onRefresh, onDetectPrompt, onTogglePromptDetection, promptDetectionEnabled, ntfyConfigured, notifyEnabled, onToggleNotify, onReload, onDisconnect, onKill })
  const [macrosOpen, setMacrosOpen] = useState(false)
  const [macros, setMacros] = useState<{ name: string; keys: string }[]>([])
  const macrosRef = useRef<HTMLDivElement>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const inputRef = useRef(input)
  const prevAgentIdRef = useRef(agentId)
  // Dictation: the server sends what was said to the agent, or types it into
  // the pane, as the voice settings say.
  const voice = useVoiceRecorder({ agentId, agentKind, baseUrl })

  // Fetch macros eagerly so numbered shortcuts are ready when the menu opens.
  useEffect(() => {
    apiFetch(baseUrl, "/api/macros")
      .then((r) => r.ok ? r.json() : { macros: [] })
      .then((data: { macros?: { name: string; keys: string }[] }) => {
        setMacros(Array.isArray(data.macros) ? data.macros : [])
      })
      .catch(() => setMacros([]))
  }, [baseUrl])

  useImperativeHandle(ref, () => ({
    focus: () => textareaRef.current?.focus(),
    voiceStart: () => { void voice.start() },
    voiceSend: () => { void voice.stop() },
    voiceCancel: voice.cancel,
    voiceIsRecording: () => voice.state === "recording",
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
  useEffect(() => {
    inputRef.current = input
  }, [input])

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

  // An image pasted into the message box is stored by Legato, and its path
  // goes into the message where the cursor is.
  const pasted = usePastedImages(agentId, input, (path) => {
    handleInputChange(insertAtCursor(inputRef.current, path, textareaRef.current?.selectionStart))
  })

  // Something the agent showed in the side panel may put text in the input.
  // It is added to what is there, kept as a draft, and never sent from here.
  useEffect(() => onComposerProposal(agentId, (text) => {
    const next = appendProposal(inputRef.current, text)
    setInput(next)
    localStorage.setItem(draftKey(agentId), next)
    textareaRef.current?.focus()
  }), [agentId])

  // Close macros dropdown on outside click.
  useEffect(() => {
    if (!macrosOpen) return
    const onClick = (e: MouseEvent) => {
      if (macrosRef.current && !macrosRef.current.contains(e.target as Node)) {
        setMacrosOpen(false)
      }
    }
    document.addEventListener("click", onClick, true)
    return () => document.removeEventListener("click", onClick, true)
  }, [macrosOpen])

  const handleSubmit = () => {
    const trimmed = input.trim()
    if (trimmed) {
      onSubmitMessage(trimmed)
      setInput("")
      localStorage.removeItem(draftKey(agentId))
    }
  }

  const handleMacro = useCallback((keys: string) => {
    onSubmitMessage(keys)
    setMacrosOpen(false)
  }, [onSubmitMessage])
  const openMacros = useCallback(() => setMacrosOpen(true), [])
  const closeMacros = useCallback(() => setMacrosOpen(false), [])
  useMacroHotkeys({ macros, open: macrosOpen, onOpen: openMacros, onClose: closeMacros, onSelect: handleMacro })

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
  const commandKind = agentKind || agentCommand
  const isBashMode = ["claude-code", "claude", "codex"].includes(commandKind || "") && input.startsWith("! ")

  const asking = (type === "tool_approval" || type === "plan_approval" || type === "question" || type === "menu") && promptState?.actions

  return (
    <div className="bg-lw-ground px-3 pb-3 font-lw text-[13px] text-lw-text md:px-6 md:pb-[18px]">
      {!menuInHeader && (
        // Without the page's header (in a swarm), the bar names the agent
        // and holds its side panels and menu itself.
        <div className="flex items-center justify-between gap-2 py-2">
          <div className="flex min-w-0 items-center gap-2">
            {/* Below md the agent picker above the terminal already names the tool. */}
            <AgentKindBadge kind={agentKind} isNative={false} className="hidden md:inline-flex" />
            {agentTitle && <div className="truncate text-xs text-lw-text-2">{agentTitle}</div>}
          </div>
          <div className="flex shrink-0 items-center gap-1.5">
            {panels && <SidePanelSwitch {...panels} />}
            <AgentMoreMenu entries={terminalMenu.entries} />
          </div>
        </div>
      )}
      {!menuInHeader && terminalMenu.upgradeError && <div role="alert" className="mb-2 text-xs text-lw-error">{terminalMenu.upgradeError}</div>}

      <div className={cn("mx-auto flex w-full max-w-[820px] flex-col rounded-[10px] border bg-lw-surface max-sm:rounded-xl", dictating(voice) ? "border-lw-danger-line" : "border-lw-line-strong", menuInHeader && "mt-3")}>
        {isWorking && !asking && <WorkingStrip onInterrupt={() => onSendKeys("Escape")} />}
        {asking && promptState?.actions && type && (
          <div className="p-2.5">
            <ActionList
              actions={promptState.actions}
              type={type}
              onSelect={(keys) => { onSendKeys(keys); onDismissPrompt() }}
              onDismiss={onDismissPrompt}
            />
          </div>
        )}

        {!asking && (
          <>
            <PastedImageStrip pasted={pasted} />
            <div className="relative">
              {isBashMode && (
                <div className="pointer-events-none absolute left-3.5 top-2 flex items-center gap-1 text-lw-needs">
                  <Terminal size={13} />
                  <span className="text-[10px] font-semibold uppercase tracking-wide">bash</span>
                </div>
              )}
              <textarea
                ref={textareaRef}
                aria-label="Message"
                value={input}
                onChange={(e) => handleInputChange(e.target.value)}
                onKeyDown={handleKeyDown}
                onPaste={pasted.onPaste}
                placeholder={connected === false ? "Disconnected..." : "Type a message..."}
                disabled={connected === false}
                rows={1}
                className={cn(
                  "block w-full resize-none border-0 bg-transparent px-3.5 pb-1 pt-3 text-sm leading-6 text-lw-text outline-none placeholder:text-lw-faint disabled:opacity-50 max-sm:text-base",
                  isBashMode && "pt-8 shadow-[inset_0_0_0_1px] shadow-lw-needs/60",
                )}
                autoFocus
              />
            </div>
          </>
        )}

        {dictating(voice) ? <DictationBar recorder={voice} /> : (
        <div className={cn("flex min-w-0 items-center gap-1 px-2 pb-2 pt-1.5 text-xs text-lw-text-2 max-sm:gap-0.5 max-sm:px-1.5", asking && "border-t border-lw-line")}>
          <div className="flex min-w-0 flex-1 items-center gap-1 max-sm:gap-0.5">
            <button type="button" onClick={() => onSendKeys("BTab")} className={composerToolButton()} title="Cycle Claude mode (Shift+Tab)" aria-label="Mode">
              <ArrowLeftRight size={16} aria-hidden="true" className="sm:hidden" />
              <span className="hidden sm:inline">Mode</span>
              <span aria-hidden="true" className="hidden font-lw-mono text-lw-faint sm:inline">⇧Tab</span>
            </button>
        {/* Macros dropdown */}
        <div className="relative" ref={macrosRef}>
          <button
            type="button"
            onClick={() => setMacrosOpen((v) => !v)}
            className={composerToolButton(macrosOpen)}
            title="Macros (M)"
            aria-label="Macros"
            aria-keyshortcuts="m"
          >
            <Zap size={16} aria-hidden="true" className="sm:hidden" />
            <span className="hidden sm:inline">Macros</span>
          </button>
          {macrosOpen && (
            <div className="absolute bottom-full left-0 z-10 mb-1 min-w-[180px] max-w-[calc(100vw-2rem)] rounded-md border border-lw-line-strong bg-lw-surface py-1 shadow-xl">
              {macros.length === 0 ? (
                <div className="px-3 py-1.5 text-xs text-lw-text-2">(no macros configured)</div>
              ) : (
                macros.map((macro, index) => {
                  const shortcut = index < 9 ? String(index + 1) : undefined
                  return (
                    <button
                      key={macro.name}
                      onClick={() => handleMacro(macro.keys)}
                      aria-keyshortcuts={shortcut}
                      className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-lw-text transition-colors hover:bg-lw-raised"
                    >
                      {shortcut && <kbd className="w-4 rounded border border-lw-line-strong text-center font-mono text-[10px] text-lw-text-2">{shortcut}</kbd>}
                      <Zap size={12} />
                      {macro.name}
                    </button>
                  )
                })
              )}
            </div>
          )}
        </div>
          </div>
          {voiceEnabled && connected !== false && <DictateButton recorder={voice} />}
          {!asking && (
            <SendButton label="Send" queue={false} disabled={!input.trim() || connected === false} onClick={handleSubmit} />
          )}
        </div>
        )}
      </div>
    </div>
  )
})
