import { useCallback, useEffect, useRef, useState, type ClipboardEvent, type KeyboardEvent, type ReactNode, type RefObject } from "react"
import { ArrowLeftRight, Terminal, X, Zap } from "lucide-react"

import { choiceStep, choiceTone } from "../lib/promptBar"
import { composerToolButton } from "../lib/composer"
import { cn } from "../lib/utils"
import { AgentKindBadge } from "./AgentKindBadge"
import { AgentMoreMenu, SidePanelSwitch, type SidePanelsProps } from "./AgentChatHeader"
import { AttachImageButton } from "./AttachImageButton"
import { DictateButton, SendButton } from "./Composer"
import { MacroPickerItems } from "./macros/MacroPicker"
import type { useMacroMenu } from "./usePromptBar"
import type { useTerminalMenu } from "../hooks/useTerminalMenu"
import type { useVoiceRecorder } from "../hooks/useVoiceRecorder"

// The terminal prompt bar's sections. See PromptBar.tsx.

interface ActionListProps {
  actions: { label: string; keys: string }[]
  type: string
  onSelect: (keys: string) => void
  onDismiss: () => void
}

// The choices of a prompt the tool shows: Down or j and Up or k move,
// Enter on the list picks the highlighted one, Escape dismisses.
export function ActionList({ actions, type, onSelect, onDismiss }: ActionListProps) {
  const [selected, setSelected] = useState(0)
  const listRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    listRef.current?.focus()
    setSelected(0)
  }, [type])

  const handleKey = useCallback((e: KeyboardEvent) => {
    const next = choiceStep(e.key, selected, actions.length)
    if (next !== null) {
      e.preventDefault()
      setSelected(next)
      listRef.current?.querySelectorAll<HTMLButtonElement>("button")[next]?.focus()
    } else if (e.key === "Enter" && e.target === e.currentTarget) {
      e.preventDefault()
      onSelect(actions[selected].keys)
    } else if (e.key === "Escape") {
      e.preventDefault()
      onDismiss()
    }
  }, [actions, selected, onSelect, onDismiss])

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
          onFocus={() => setSelected(i)}
          onClick={() => onSelect(action.keys)}
          className={cn(
            "flex items-center gap-2 rounded px-3 py-1.5 text-sm text-left transition-colors",
            i === selected
              ? "bg-zinc-800 text-zinc-100"
              : "text-zinc-400 hover:bg-zinc-800/50 hover:text-zinc-200"
          )}
        >
          <span className={cn("font-medium", choiceTone(action.label))}>
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

// Without the page's header (in a swarm), the bar names the agent and holds
// its side panels and menu itself, and says why an upgrade failed.
export function BarHeader({ agentKind, engram, agentTitle, panels, terminalMenu }: {
  agentKind?: string
  engram?: string
  agentTitle?: string
  panels?: SidePanelsProps
  terminalMenu: ReturnType<typeof useTerminalMenu>
}) {
  return <>
    <div className="flex items-center justify-between gap-2 py-2">
      <div className="flex min-w-0 items-center gap-2">
        {/* Below md the agent picker above the terminal already names the tool. */}
        <AgentKindBadge kind={agentKind} engram={engram} isNative={false} className="hidden md:inline-flex" />
        {agentTitle && <div className="truncate text-xs text-lw-text-2">{agentTitle}</div>}
      </div>
      <div className="flex shrink-0 items-center gap-1.5">
        {panels && <SidePanelSwitch {...panels} />}
        <AgentMoreMenu entries={terminalMenu.entries} />
      </div>
    </div>
    {terminalMenu.upgradeError && <div role="alert" className="mb-2 text-xs text-lw-error">{terminalMenu.upgradeError}</div>}
  </>
}

// The message box, marked when the message is a shell command.
export function MessageBox({ textareaRef, input, bash, connected, onChange, onKeyDown, onPaste }: {
  textareaRef: RefObject<HTMLTextAreaElement | null>
  input: string
  bash: boolean
  connected?: boolean
  onChange: (value: string) => void
  onKeyDown: (e: KeyboardEvent) => void
  onPaste: (e: ClipboardEvent<HTMLTextAreaElement>) => void
}) {
  return (
    <div className="relative">
      {bash && (
        <div className="pointer-events-none absolute left-3.5 top-2 flex items-center gap-1 text-lw-needs">
          <Terminal size={13} />
          <span className="text-[10px] font-semibold uppercase tracking-wide">bash</span>
        </div>
      )}
      <textarea
        ref={textareaRef}
        aria-label="Message"
        value={input}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={onKeyDown}
        onPaste={onPaste}
        placeholder={connected === false ? "Disconnected..." : "Type a message..."}
        disabled={connected === false}
        rows={1}
        className={cn(
          "block w-full resize-none border-0 bg-transparent px-3.5 pb-1 pt-3 text-sm leading-6 text-lw-text outline-none placeholder:text-lw-faint disabled:opacity-50 max-sm:text-base",
          bash && "pt-8 shadow-[inset_0_0_0_1px] shadow-lw-needs/60",
        )}
        autoFocus
      />
    </div>
  )
}

type MacroMenu = Omit<ReturnType<typeof useMacroMenu>, "ref">

// The row under the message box: the tools, dictation where it is on, and
// Send, which a prompt that asks takes the place of.
export function ComposerFooter({ asking, connected, canSend, voiceEnabled, voice, onSubmit, children }: {
  asking: boolean
  connected?: boolean
  canSend: boolean
  voiceEnabled?: boolean
  voice: ReturnType<typeof useVoiceRecorder>
  onSubmit: () => void
  children: ReactNode
}) {
  return (
    <div className={cn("flex min-w-0 items-center gap-1 px-2 pb-2 pt-1.5 text-xs text-lw-text-2 max-sm:gap-0.5 max-sm:px-1.5", asking && "border-t border-lw-line")}>
      {children}
      {voiceEnabled && connected !== false && <DictateButton recorder={voice} />}
      {!asking && (
        <SendButton label="Send" queue={false} disabled={!canSend || connected === false} onClick={onSubmit} />
      )}
    </div>
  )
}

// The tools left of Send: attach an image (not while a prompt asks), cycle
// the tool's mode, and the macros.
export function ComposerTools({ asking, connected, onAttach, onSendKeys, macroRef, macroMenu }: {
  asking: boolean
  connected?: boolean
  onAttach: (files: File[]) => void
  onSendKeys: (keys: string) => void
  macroRef: RefObject<HTMLDivElement | null>
  macroMenu: MacroMenu
}) {
  return (
    <div className="flex min-w-0 flex-1 items-center gap-1 max-sm:gap-0.5">
      {!asking && <AttachImageButton onFiles={onAttach} disabled={connected === false} />}
      <button type="button" onClick={() => onSendKeys("BTab")} className={composerToolButton()} title="Cycle Claude mode (Shift+Tab)" aria-label="Mode">
        <ArrowLeftRight size={16} aria-hidden="true" className="sm:hidden" />
        <span className="hidden sm:inline">Mode</span>
        <span aria-hidden="true" className="hidden font-lw-mono text-lw-faint sm:inline">⇧Tab</span>
      </button>
      <div className="relative" ref={macroRef}>
        <button
          type="button"
          onClick={macroMenu.toggle}
          className={composerToolButton(macroMenu.open)}
          title="Macros (M)"
          aria-label="Macros"
          aria-keyshortcuts="m"
        >
          <Zap size={16} aria-hidden="true" className="sm:hidden" />
          <span className="hidden sm:inline">Macros</span>
        </button>
        {macroMenu.open && (
          <div className="absolute bottom-full left-0 z-10 mb-1 min-w-[180px] max-w-[calc(100vw-2rem)] rounded-md border border-lw-line-strong bg-lw-surface py-1 shadow-xl">
            <MacroPickerItems shown={macroMenu.picker.shown} hidden={macroMenu.picker.hidden} onShowAll={macroMenu.picker.showAll} onPick={(macro) => macroMenu.pick(macro.keys)} />
          </div>
        )}
      </div>
    </div>
  )
}
