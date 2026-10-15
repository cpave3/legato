import { Loader2, Mic, Square } from "lucide-react"
import type { VoiceRecorder } from "../hooks/useVoiceRecorder"
import { CONTEXT_NEARLY_FULL } from "../lib/composer"
import { cn } from "../lib/utils"

// The parts of a message box that the native composer and the terminal
// session's PromptBar share: the line that says the agent is working, the
// buttons along the foot, the context gauge, and the send button.

// WorkingStrip is the line above the message box while the agent works: how
// long the turn has run, what it runs now, and the way to stop it.
export function WorkingStrip({ elapsed, step, onInterrupt }: {
  elapsed?: string
  step?: string | null
  onInterrupt: () => void
}) {
  return (
    <div role="status" aria-label="Agent working" className="flex min-w-0 items-center gap-2 border-b border-lw-line py-1.5 pl-3.5 pr-2 text-xs">
      <span aria-hidden="true" className="text-lw-live">●</span>
      <span className="shrink-0 text-[#b4b3bd]">Working{elapsed && ` ${elapsed}`}</span>
      <span className="min-w-0 flex-1 truncate font-lw-mono text-lw-faint">{step}</span>
      <button
        type="button"
        onClick={onInterrupt}
        aria-label="Interrupt"
        title="Interrupt (Esc)"
        className="flex h-[26px] shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md border border-lw-danger-line px-2.5 text-lw-danger hover:bg-lw-error-soft max-sm:h-8"
      >
        <Square size={9} fill="currentColor" aria-hidden="true" />
        Interrupt
        <span aria-hidden="true" className="hidden text-[#8a5a5e] sm:inline">esc</span>
      </button>
    </div>
  )
}

export interface QueuedEntry {
  key: string
  text: string
  // A message already went to the agent and cannot be taken back; a shell
  // command waits in the page and can be.
  onEdit?: () => void
  onRemove?: () => void
}

// QueuedList is what was sent while the agent worked and has not reached it
// yet, numbered in the order it will be read.
export function QueuedList({ entries }: { entries: QueuedEntry[] }) {
  if (entries.length === 0) return null
  return (
    <ol aria-label="Queued messages" className="m-0 flex list-none flex-col gap-1 px-2.5 pt-2">
      {entries.map((entry, index) => (
        <li key={entry.key} className="flex min-w-0 items-center gap-2 rounded-md bg-lw-raised px-2 py-1 text-xs">
          <span className="shrink-0 font-lw-mono text-lw-faint">{index + 1}</span>
          <span className="min-w-0 flex-1 truncate" title={entry.text}>{entry.text}</span>
          {entry.onEdit
            ? <button type="button" onClick={entry.onEdit} className="shrink-0 text-lw-text-2 hover:text-lw-text">Edit</button>
            : <span className="shrink-0 text-lw-faint" title="Sent. The agent reads it at its next step.">sent</span>}
          {entry.onRemove && (
            <button type="button" onClick={entry.onRemove} aria-label="Remove queued message" className="shrink-0 px-0.5 text-sm leading-none text-lw-text-2 hover:text-lw-danger">×</button>
          )}
        </li>
      ))}
    </ol>
  )
}

// ContextNotice is the line above the message box once the agent's context
// is nearly full, so the person knows before the agent stops or forgets.
export function ContextNotice({ used, window }: { used: string; window: string }) {
  return (
    <div role="status" aria-label="Context nearly full" className="flex min-w-0 items-center gap-2 rounded-t-[10px] border-b border-[#3d2a20] bg-[#1f1814] py-1.5 pl-3.5 pr-2 text-xs">
      <span aria-hidden="true" className="text-lw-danger">●</span>
      <span className="min-w-0 flex-1 text-[#e6c8b8]">Context nearly full: {used} of {window} used.</span>
    </div>
  )
}

// DictateButton starts a recording.
export function DictateButton({ recorder }: { recorder: VoiceRecorder }) {
  return (
    <button
      type="button"
      aria-label="Voice dictation"
      title="Voice dictation"
      onClick={() => { void recorder.start() }}
      className="flex size-8 shrink-0 items-center justify-center rounded-lg text-lw-text-2 hover:bg-lw-raised hover:text-lw-text max-sm:size-11 max-sm:rounded-full"
    >
      <Mic size={16} aria-hidden="true" />
    </button>
  )
}

// DictationBar takes the place of the composer's foot while the person
// speaks: how long, how loud, and the way to stop or throw it away. What was
// said goes into the message, to be read over before it is sent.
export function DictationBar({ recorder }: { recorder: VoiceRecorder }) {
  const { state, seconds, levels, error } = recorder
  const clock = `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`
  const bars = Array.from({ length: 24 }, (_, index) => levels[levels.length - 24 + index])
  return (
    <div role="group" aria-label="Dictating" className="flex min-w-0 items-center gap-2.5 py-1.5 pl-3.5 pr-2 text-xs">
      {state === "recording" && <>
        <span aria-hidden="true" className="size-2 shrink-0 animate-pulse rounded-full bg-lw-record" />
        <span className="shrink-0 font-lw-mono text-lw-danger">{clock}</span>
        <span aria-hidden="true" className="flex h-5 min-w-0 flex-1 items-center gap-0.5 overflow-hidden">
          {bars.map((level, index) => (
            <span
              key={index}
              className={cn("w-[3px] shrink-0 rounded-[1px]", level === undefined ? "bg-lw-danger-line" : "bg-lw-danger")}
              style={{ height: `${level === undefined ? 4 : Math.max(4, Math.round(level * 20))}px` }}
            />
          ))}
        </span>
        <button type="button" onClick={recorder.cancel} className="h-8 shrink-0 rounded-lg px-3 text-[13px] text-lw-text-2 hover:text-lw-text">Cancel</button>
        <button type="button" onClick={() => { void recorder.stop() }} aria-label="Stop" className="flex h-8 shrink-0 items-center gap-1.5 rounded-lg border border-lw-danger-line bg-lw-error-soft px-3.5 text-[13px] font-medium text-lw-danger">
          <Square size={9} fill="currentColor" aria-hidden="true" />
          Stop
        </button>
      </>}
      {state === "transcribing" && <span className="flex h-8 items-center gap-2 text-lw-text-2"><Loader2 size={14} className="animate-spin" aria-hidden="true" />Transcribing…</span>}
      {state === "error" && <>
        <span className="min-w-0 flex-1 truncate text-lw-danger" title={error}>Dictation failed: {error}</span>
        <button type="button" onClick={recorder.cancel} className="h-8 shrink-0 rounded-lg px-3 text-[13px] text-lw-text-2 hover:text-lw-text">Dismiss</button>
      </>}
    </div>
  )
}


// ContextGauge is how full the agent's context window is: the tokens used
// and a bar, amber from 70% and red from 85%.
export function ContextGauge({ share, used, window }: { share: number; used: string; window: string }) {
  const tone = share >= CONTEXT_NEARLY_FULL ? "bg-lw-error" : share >= 70 ? "bg-lw-needs" : "bg-lw-live"
  return (
    <span
      role="img"
      aria-label={`Context ${share}% full`}
      title={`${used} of ${window} context`}
      className={cn("flex shrink-0 items-center gap-1.5 px-1.5 font-lw-mono text-[11px] tabular-nums", share >= CONTEXT_NEARLY_FULL ? "text-lw-danger" : share >= 70 ? "text-lw-needs sm:text-lw-text-2" : "text-lw-text-2")}
    >
      <span className="hidden sm:inline">{used}</span>
      <span className="hidden h-1 w-10 overflow-hidden rounded-sm bg-lw-line-strong sm:flex">
        <span className={tone} style={{ width: `${Math.min(share, 100)}%` }} />
      </span>
      <span className="sm:hidden">{share}%</span>
    </span>
  )
}

// SendButton sends the message, or queues it while the agent works: the
// quieter look says it will not be read at once.
export function SendButton({ label, queue, disabled, onClick }: { label: string; queue: boolean; disabled: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-label={label}
      className={cn(
        "flex h-8 shrink-0 items-center gap-1.5 rounded-lg px-3.5 text-[13px] font-medium disabled:opacity-50 max-sm:size-11 max-sm:justify-center max-sm:rounded-full max-sm:px-0",
        queue ? "border border-lw-accent-ring bg-lw-accent-soft text-lw-accent-text" : "bg-lw-accent text-white hover:brightness-110",
      )}
    >
      <span className="max-sm:hidden">{label}</span>
      <span aria-hidden="true" className={cn("max-sm:hidden", queue ? "text-[#8a82c8]" : "text-[#cfc8ff]")}>↵</span>
      <span aria-hidden="true" className="text-lg leading-none sm:hidden">↑</span>
    </button>
  )
}
