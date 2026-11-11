import { useEffect, useRef, type ReactNode } from "react"
import { VoiceRing, type RingMode } from "./VoiceRing"
import type { VoiceView } from "./session"
import { idleLine } from "./copy"
import "./voice.css"

// What a widget does with the person's presses. Each is optional: a widget
// leaves out the buttons it has no handler for.
export interface VoiceActions {
  onListen?: () => void
  onSend?: () => void
  onCancel?: () => void
  onStop?: () => void
  onReply?: () => void
  onOpenConversation?: () => void
  onOpenSettings?: () => void
  // onOpenMain opens the main's page, from a worker's (the Android app).
  onOpenMain?: () => void
  onClose?: () => void
}

// Readiness, as GET /api/reeve/voice says it.
export interface VoiceReadinessView {
  ready: boolean
  reason: string
  setting: string
  main: string
}

const KICKER: Record<string, string> = { idle: "READY", listening: "LISTENING", thinking: "THINKING", speaking: "SPEAKING" }

function Keys({ keys }: { keys: string[] }) {
  if (!keys.length) return null
  return (
    <span className="flex gap-1" aria-label={`Key: ${keys.join(", ")}`}>
      {keys.map((k) => (
        <kbd key={k} className="voice-mono rounded-md border border-[#31313A] bg-[#0D0D12] px-1.5 py-0.5 text-[10px] text-[#9A99A5]">{k}</kbd>
      ))}
    </span>
  )
}

// VoiceCapsule is the widget at rest (the desktop's): the ring breathing,
// the Reeve's name, "<main> · ready" and the key; or, not ready, an amber
// ring, one line why, the setting and Open settings.
export function VoiceCapsule({ name, readiness, keys = [], onListen, onOpenSettings }: {
  name: string
  readiness: VoiceReadinessView
  keys?: string[]
} & Pick<VoiceActions, "onListen" | "onOpenSettings">) {
  if (!readiness.ready) {
    return (
      <div className="voice-ui flex h-16 w-[430px] max-w-full items-center gap-3.5 rounded-[32px] border border-[#E8B53A]/35 px-2.5 shadow-[0_12px_32px_rgba(0,0,0,0.45)]"
        style={{ background: "var(--voice-surface)" }} data-testid="voice-capsule" data-ready="false">
        <VoiceRing mode="blocked" size={44} />
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="truncate text-sm font-semibold">{readiness.reason || "Voice isn't ready"}</span>
          <span className="voice-mono truncate text-[11px] text-[#9A99A5]">not recording{readiness.setting ? ` · ${readiness.setting}` : ""}</span>
        </div>
        {onOpenSettings && (
          <button type="button" onClick={onOpenSettings} className="voice-secondary h-11 shrink-0 rounded-full px-4 text-[13px] font-semibold">Open settings</button>
        )}
      </div>
    )
  }
  return (
    <button type="button" onClick={onListen} data-testid="voice-capsule" data-ready="true"
      className="voice-ui flex h-16 w-[318px] max-w-full items-center gap-3.5 rounded-[32px] border border-[#6D5EF6]/30 pl-2.5 pr-4 text-left shadow-[0_12px_32px_rgba(0,0,0,0.45)]"
      style={{ background: "var(--voice-surface)" }} aria-label={`Talk to ${name}`}>
      <VoiceRing mode="idle" size={44} />
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="truncate text-sm font-semibold">{name}</span>
        <span className="voice-mono flex items-center gap-1.5 text-[11px] text-[#9A99A5]">
          <span className="h-[7px] w-[7px] rounded-full bg-[#6D5EF6]" />{readiness.main} · ready
        </span>
      </span>
      <Keys keys={keys} />
    </button>
  )
}

function Header({ view, main }: { view: VoiceView; main: string }) {
  return (
    <div className="flex items-center gap-2">
      <span className="text-[#6D5EF6]" aria-hidden="true">◈</span>
      <span className="voice-kicker" aria-live="polite">{KICKER[view.state] ?? "READY"}</span>
      <span className="flex-1" />
      <span className="h-2 w-2 rounded-full bg-[#6D5EF6]" />
      <span className="voice-mono text-xs text-[#9A99A5]">{main}</span>
    </div>
  )
}

// Caption is the line being spoken lit, those to come dimmed.
function Caption({ view, className }: { view: VoiceView; className?: string }) {
  const lines = view.lines
  if (!lines.length) return null
  return (
    <p className={className} data-testid="voice-caption">
      {lines.map((l, i) => (
        <span key={l.id} className={l.id === view.speaking || (!view.speaking && i === 0) ? "text-[#E6E5EB]" : "text-[#6E6D79]"}>
          {i > 0 ? " " : ""}{l.text}
        </span>
      ))}
    </p>
  )
}

function PauseBar({ view, hint }: { view: VoiceView; hint: ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <div className="h-[3px] overflow-hidden rounded-sm bg-[#26262D]">
        <div data-testid="voice-pause" className="voice-primary h-full" style={{ width: `${Math.round(view.pause * 100)}%` }} />
      </div>
      {hint}
    </div>
  )
}

function Buttons({ view, actions, big }: { view: VoiceView; actions: VoiceActions; big?: boolean }) {
  const cls = big ? "h-[52px] rounded-[14px] text-[15px]" : "h-11 rounded-xl text-[13px]"
  let pair: [string, (() => void) | undefined, string, (() => void) | undefined]
  switch (view.state) {
    case "listening": pair = ["Cancel", actions.onCancel, "Send now", actions.onSend]; break
    case "thinking": pair = ["Stop", actions.onStop, "Open conversation", actions.onOpenConversation]; break
    case "speaking": pair = ["Stop", actions.onStop, "Reply", actions.onReply]; break
    default: pair = ["Close", actions.onClose, view.written ? "Open conversation" : "Talk", view.written ? actions.onOpenConversation : actions.onListen]
  }
  const [second, onSecond, first, onFirst] = pair
  return (
    <div className="flex gap-2">
      {onSecond && <button type="button" onClick={onSecond} className={`voice-secondary flex-1 font-semibold ${cls}`}>{second}</button>}
      {onFirst && <button type="button" onClick={onFirst} className={`voice-primary flex-1 font-semibold ${cls}`}>{first}</button>}
    </div>
  )
}

// VoiceCard is the desktop's widget during a conversation: listening with
// the words so far and the pause bar, thinking with what was sent and what
// the Reeve is doing, speaking with the caption.
export function VoiceCard({ view, main, keyLabel, ...actions }: { view: VoiceView; main: string; keyLabel?: string } & VoiceActions) {
  const mode: RingMode = view.state
  return (
    <div className="voice-ui flex w-[412px] max-w-full flex-col gap-3.5 rounded-[20px] border border-[#6D5EF6]/45 px-[18px] py-4 shadow-[0_18px_48px_rgba(0,0,0,0.5)]"
      style={{ background: "var(--voice-surface)" }} data-testid="voice-card" data-state={view.state}>
      <Header view={view} main={main} />
      <div className="flex items-center gap-4">
        <VoiceRing mode={mode} size={88} level={view.level} />
        <div className="flex min-w-0 flex-1 flex-col gap-2">
          <Body view={view} />
        </div>
      </div>
      {view.state === "listening" && (
        <PauseBar view={view} hint={
          <div className="voice-mono flex justify-between text-[11px] text-[#9A99A5]">
            <span>pause to send</span>{keyLabel && <span>{keyLabel} to send</span>}
          </div>} />
      )}
      <Buttons view={view} actions={actions} />
    </div>
  )
}

function Body({ view, centered }: { view: VoiceView; centered?: boolean }) {
  const big = centered ? "text-lg font-medium" : "text-[15px]"
  switch (view.state) {
    case "listening":
      return <p className={`m-0 leading-[1.45] ${big}`}>{view.partial}<span className="voice-caret" aria-hidden="true" /></p>
    case "thinking":
      return (
        <>
          {view.heard && <p className={`m-0 leading-[1.45] text-[#9A99A5] ${centered ? "text-[13px]" : "text-sm"}`}>“{view.heard}”</p>}
          <span className="voice-mono text-[11px] text-[#A99CFF]">{view.activity ? `${view.activity}…` : "thinking…"}</span>
        </>
      )
    case "speaking":
      return (
        <>
          {centered && view.heard && <p className="m-0 text-[13px] leading-[1.45] text-[#9A99A5]">“{view.heard}”</p>}
          <Caption view={view} className={`m-0 leading-[1.45] ${big}`} />
        </>
      )
  }
  const line = idleLine(view)
  return <p className={`m-0 leading-[1.45] text-[#9A99A5] ${centered ? "text-[15px]" : "text-sm"}`}>{line || "Press to talk to the Reeve"}</p>
}

// VoiceSheet is the phone's: a sheet over the page with the ring at 176 px.
export function VoiceSheet({ view, main, readiness, ...actions }: { view: VoiceView; main: string; readiness?: VoiceReadinessView } & VoiceActions) {
  const blocked = readiness && !readiness.ready
  // The focus moves into the sheet when it opens.
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => { ref.current?.focus() }, [])
  return (
    <div ref={ref} tabIndex={-1} className="voice-ui voice-rise flex flex-col gap-[18px] rounded-t-[22px] border border-b-0 border-[#6D5EF6]/45 px-5 pt-[18px] pb-[30px] outline-none"
      style={{ background: "var(--voice-surface)" }} data-testid="voice-sheet" data-state={blocked ? "blocked" : view.state}
      role="dialog" aria-modal="true" aria-label="Talk to the Reeve">
      <Header view={view} main={main} />
      <div className="flex justify-center py-1.5">
        <VoiceRing mode={blocked ? "blocked" : view.state} size={176} level={view.level} />
      </div>
      {blocked ? (
        <>
          <p className="m-0 text-center text-lg font-medium leading-[1.45]" aria-live="polite">{readiness.reason || "Voice isn't ready"}</p>
          {readiness.setting && <p className="voice-mono m-0 text-center text-[11px] text-[#9A99A5]">{readiness.setting}</p>}
          <div className="flex gap-2.5">
            {actions.onClose && <button type="button" onClick={actions.onClose} className="voice-secondary h-[52px] flex-1 rounded-[14px] text-[15px] font-semibold">Close</button>}
            {actions.onOpenSettings && <button type="button" onClick={actions.onOpenSettings} className="voice-primary h-[52px] flex-1 rounded-[14px] text-[15px] font-semibold">Open settings</button>}
            {actions.onOpenMain && <button type="button" onClick={actions.onOpenMain} className="voice-primary h-[52px] flex-1 rounded-[14px] text-[15px] font-semibold">Open the main</button>}
          </div>
        </>
      ) : (
        <>
          <div className="flex flex-col items-center gap-2.5 text-center" aria-live="polite"><Body view={view} centered /></div>
          {view.state === "listening" && (
            <PauseBar view={view} hint={<div className="voice-mono text-center text-[11px] text-[#9A99A5]">pause to send</div>} />
          )}
          {view.state === "speaking" && <div className="voice-mono text-center text-[11px] text-[#9A99A5]">tap Reply, or talk when it has finished</div>}
          <Buttons view={view} actions={actions} big />
        </>
      )}
    </div>
  )
}
