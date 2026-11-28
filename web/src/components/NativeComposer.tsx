import type { RefObject } from "react"
import { ArrowLeftRight, Check, Terminal, Zap } from "lucide-react"
import { ContextGauge, ContextNotice, DictateButton, DictationBar, QueuedList, SendButton, WorkingStrip, type QueuedEntry } from "./Composer"
import { ComposerModelMenu } from "./ComposerModelMenu"
import { NativeModelSettingsForm } from "./NativeModelSettings"
import { NativeSlashMenu } from "./NativeSlashMenu"
import { PastedImageStrip } from "./PastedImageStrip"
import { AttachImageButton } from "./AttachImageButton"
import { ResourcesButton } from "./ResourcesButton"
import { MacroPickerItems } from "./macros/MacroPicker"
import type { PastedImages } from "../hooks/usePastedImages"
import type { PickerMacro } from "../hooks/useMacroPicker"
import { CONTEXT_NEARLY_FULL, composerToolButton, dictating, formatElapsed } from "../lib/composer"
import { compactionNotice, type Compaction } from "../lib/compaction"
import type { ModelSettingsState } from "../lib/modelSettings"
import type { ModeInfo, ModelInfo } from "../lib/nativeCatalog"
import type { VoiceRecorder } from "../lib/voiceRecorder"
import type { useSlashMenu } from "./useSlashMenu"

function formatTokens(value: number): string {
  if (value < 1_000) return value.toLocaleString()
  if (value < 1_000_000) return `${Number((value / 1_000).toFixed(1))}k`
  return `${Number((value / 1_000_000).toFixed(2))}M`
}

// How full the agent's context is, as its session reports it.
interface ContextUsage {
  used: number
  window: number
}

function contextShare(usage: ContextUsage | null): number | null {
  return usage ? Math.round((usage.used / usage.window) * 100) : null
}

type SlashMenu = ReturnType<typeof useSlashMenu>

export interface NativeComposerProps {
  agentId: string
  baseUrl: string
  sandboxed?: boolean
  enabled: boolean
  voiceEnabled?: boolean
  openItems: number
  busy: boolean
  turnSeconds: number | null
  step: string | null
  contextUsage: ContextUsage | null
  compaction?: Compaction
  sendError: string | null
  queuedEntries: QueuedEntry[]
  draft: string
  setDraft: (draft: string) => void
  textareaRef: RefObject<HTMLTextAreaElement | null>
  isBang: boolean
  bangRunning: boolean
  pasted: PastedImages
  voice: VoiceRecorder
  slash: SlashMenu
  catalog: { loading: boolean; errors: string[]; empty: boolean; onRetry: () => void }
  onKeyDown: (e: React.KeyboardEvent) => void
  onSend: () => void
  onInterrupt: () => void
  model: ModelControlProps
  modes: ModeMenuProps
  modesRef: RefObject<HTMLDivElement | null>
  macros: MacroMenuProps
  macrosRef: RefObject<HTMLDivElement | null>
}

function composerBorder(isDictating: boolean, dragging: boolean): string {
  if (isDictating) return "border-lw-danger-line"
  return dragging ? "border-lw-accent" : "border-lw-line-strong"
}

// NativeComposer is the message box, and under it everything that sets how
// the agent works (model, effort, mode, resources, macros) beside how full
// its context is and the buttons that send and interrupt.
export function NativeComposer(props: NativeComposerProps) {
  const { enabled, pasted, voice, slash } = props
  return (
      <div className="px-3 pb-3 md:px-6 md:pb-[18px]">
        {/* An image dropped anywhere on the composer joins the message. */}
        <div {...(enabled ? pasted.dropTarget : {})} className={`native-agent-composer relative mx-auto flex w-full max-w-[820px] flex-col rounded-[10px] border bg-lw-surface max-sm:rounded-xl ${composerBorder(dictating(voice), pasted.dragging)}`}>
          {slash.open && (
            <NativeSlashMenu
              anchorRef={props.textareaRef}
              onClose={slash.dismiss}
              loading={props.catalog.loading}
              errors={props.catalog.errors}
              empty={props.catalog.empty}
              onRetry={props.catalog.onRetry}
              browser={slash.browser.open ? {
                query: slash.browser.query, onQuery: slash.browser.setQuery,
                filter: slash.browser.filter, onFilter: slash.browser.setFilter,
                onKeyDown: props.onKeyDown,
                onClose: slash.escape,
              } : undefined}
              listId={slash.listId}
              optionId={slash.optionId}
              matches={slash.matches}
              active={slash.index}
              onHover={slash.setIndex}
              onPick={slash.select}
            />
          )}
          {enabled && <button type="button" className="self-start min-h-11 px-3.5 text-sm text-lw-text-2 hover:text-lw-text focus-visible:outline-2 focus-visible:outline-lw-accent" aria-expanded={slash.open} aria-controls={slash.open ? slash.listId : undefined}
            onClick={slash.toggleBrowser}>
            Commands and skills
          </button>}
          <ComposerNotices {...props} />
          <QueuedList entries={props.queuedEntries} />
          <PastedImageStrip pasted={pasted} />
          {enabled && <MessageBox {...props} />}
          <OpenItemsNote enabled={enabled} openItems={props.openItems} />
          {dictating(voice) ? <DictationBar recorder={voice} /> : <ComposerActions {...props} />}
        </div>
      </div>
  )
}

// What the composer says above the box: the context nearly full, the turn
// running, a compaction, and why the last message did not leave.
function ComposerNotices({ contextUsage, busy, turnSeconds, step, onInterrupt, compaction, sendError }: NativeComposerProps) {
  return (
    <>
          <NearlyFullNotice usage={contextUsage} />
          {busy && <WorkingStrip elapsed={turnSeconds === null ? undefined : formatElapsed(turnSeconds)} step={step} onInterrupt={onInterrupt} />}
          {compaction && <p role="status" className={`px-3.5 pt-2 text-xs ${compaction.status === "failed" ? "text-lw-danger" : "text-lw-text-2"}`}>{compactionNotice(compaction)}</p>}
          {sendError && <p role="alert" className="px-3.5 pt-2 text-xs text-lw-danger">{sendError}</p>}
    </>
  )
}

function NearlyFullNotice({ usage }: { usage: ContextUsage | null }) {
  const share = contextShare(usage)
  if (!usage || share === null || !(share >= CONTEXT_NEARLY_FULL)) return null
  return <ContextNotice used={formatTokens(usage.used)} window={formatTokens(usage.window)} />
}

function messagePlaceholder(busy: boolean, queued: number, agentId: string): string {
  if (!busy) return `Message ${agentId}`
  return queued > 0 ? "Queue another" : "Queue a message. Sent after the current step."
}

function MessageBox({ agentId, busy, queuedEntries, draft, setDraft, textareaRef, isBang, pasted, slash, onKeyDown }: NativeComposerProps) {
  return (
            <div data-testid="agent-composer" className="relative">
              {isBang && (
                <div className="pointer-events-none absolute left-3.5 top-2 flex items-center gap-1 text-lw-needs">
                  <Terminal size={13} />
                  <span className="text-[10px] font-semibold uppercase tracking-wide">bash</span>
                </div>
              )}
              <textarea
                ref={textareaRef}
                aria-label="Message"
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                onKeyDown={onKeyDown}
                onPaste={pasted.onPaste}
                aria-autocomplete="list"
                aria-controls={slash.open ? slash.listId : undefined}
                aria-activedescendant={slash.open && slash.matches.length > 0 ? slash.optionId(slash.index) : undefined}
                placeholder={messagePlaceholder(busy, queuedEntries.length, agentId)}
                rows={2}
                className={`block min-h-14 w-full resize-none border-0 bg-transparent px-3.5 pb-1 pt-3 text-sm text-lw-text placeholder:text-lw-faint focus:outline-none disabled:opacity-50 max-sm:text-base ${isBang ? "pt-8 shadow-[inset_0_0_0_1px] shadow-lw-needs/60" : ""}`}
              />
            </div>
  )
}

function OpenItemsNote({ enabled, openItems }: { enabled: boolean; openItems: number }) {
  if (!enabled || openItems <= 0) return null
  return (
            <p className="px-3.5 pt-1 text-[11.5px] text-lw-text-2">Your next message to this agent closes its {openItems} {openItems === 1 ? "item" : "items"} in the rail.</p>
  )
}

// The foot of the composer: what sets how the agent works on the left, how
// full its context is, dictation and the send button on the right.
function ComposerActions(props: NativeComposerProps) {
  const { enabled, agentId, baseUrl, sandboxed, model, modes, macros, macrosRef } = props
  return (
          <div data-testid="composer-actions" className="flex min-w-0 items-center gap-1 px-2 pb-2 pt-1.5 text-xs text-lw-text-2 max-sm:gap-0.5 max-sm:px-1.5">
            <div className="flex min-w-0 flex-1 items-center gap-1 max-sm:gap-0.5">
            {enabled && <AttachImageButton onFiles={props.pasted.attach} />}
            {model.models.length > 0 && <ModelControl {...model} />}
            {modes.modes.length > 0 && <ModeMenu {...modes} menuRef={props.modesRef} />}
            <ResourcesButton agentId={agentId} baseUrl={baseUrl} sandboxed={sandboxed} />
            <div className="relative" ref={macrosRef}>
              <button type="button" onClick={macros.onToggle} className={composerToolButton(macros.open)} title="Macros (M)" aria-keyshortcuts="m">
                <Zap size={16} aria-hidden="true" className="sm:hidden" />
                <span className="hidden sm:inline">Macros</span>
              </button>
              {macros.open && (
                <div className="absolute bottom-full left-0 z-10 mb-1 min-w-[180px] max-w-[calc(100vw-2rem)] rounded-md border border-lw-line-strong bg-lw-surface py-1 shadow-xl">
                  <MacroPickerItems shown={macros.shown} hidden={macros.hidden} onShowAll={macros.onShowAll} onPick={(macro) => macros.onPick(macro.keys)} />
                </div>
              )}
            </div>
            </div>
            <ComposerGauge usage={props.contextUsage} />
            {enabled && props.voiceEnabled && <DictateButton recorder={props.voice} />}
            {enabled && <SendControl {...props} />}
          </div>
  )
}

function ComposerGauge({ usage }: { usage: ContextUsage | null }) {
  const share = contextShare(usage)
  if (!usage || share === null) return null
  return <ContextGauge share={share} used={formatTokens(usage.used)} window={formatTokens(usage.window)} />
}

export interface ModelControlProps {
  agentId: string
  baseUrl: string
  busy: boolean
  models: ModelInfo[]
  model?: string
  contextUsage: ContextUsage | null
  effort?: unknown
  efforts: string[]
  onModel: (ref: string) => void
  onEffort: (value: string) => void
  onSaved: (settingsState: ModelSettingsState) => void
}

function ModelControl({ agentId, baseUrl, busy, models, model, contextUsage, effort, efforts, onModel, onEffort, onSaved }: ModelControlProps) {
  const modelWindow = models.find((m) => m.ref === model)?.contextWindow ?? contextUsage?.window
  return (
              <ComposerModelMenu
                models={models}
                model={model ?? ""}
                window={modelWindow != null ? formatTokens(modelWindow) : undefined}
                effort={effort !== undefined ? String(effort) : undefined}
                efforts={efforts}
                onModel={onModel}
                onEffort={onEffort}
                effortLocked={busy ? "Wait for the current run to finish before changing the effort." : undefined}
                settings={model ? (close) => (
                  <NativeModelSettingsForm agentId={agentId} baseUrl={baseUrl} model={model} busy={busy} onClose={close}
                    onSaved={onSaved} />
                ) : undefined}
              />
  )
}

export interface ModeMenuProps {
  modes: ModeInfo[]
  mode: string
  open: boolean
  onToggle: () => void
  onChange: (mode: string) => void
}

function ModeMenu({ modes, mode, open, onToggle, onChange, menuRef }: ModeMenuProps & { menuRef: RefObject<HTMLDivElement | null> }) {
  return (
              <div className="relative" ref={menuRef}>
                <button type="button" onClick={onToggle} className={composerToolButton(open)} title="Change mode">
                  <ArrowLeftRight size={16} aria-hidden="true" className="sm:hidden" />
                  <span className="hidden sm:inline">Mode</span>
                  {mode && <span className="hidden font-lw-mono text-lw-faint sm:inline">{mode}</span>}
                </button>
                {open && (
                  <div className="absolute bottom-full left-0 z-10 mb-1 min-w-[160px] rounded-md border border-lw-line-strong bg-lw-surface py-1 shadow-xl">
                    {modes.map((m) => (
                      <button
                        key={m.name}
                        onClick={() => onChange(m.name)}
                        className={"flex w-full min-w-0 items-start gap-2 px-3 py-1.5 text-xs transition-colors " + (m.name === mode ? "bg-lw-raised text-lw-text" : "text-lw-text hover:bg-lw-raised")}
                      >
                        <Check size={12} className={m.name === mode ? "mt-0.5 shrink-0 text-lw-text-2" : "mt-0.5 shrink-0 opacity-0"} />
                        <div className="min-w-0 flex-1 text-left">
                          <div className="font-mono">{m.name}</div>
                          {m.description && <div className="truncate text-lw-text-2">{m.description}</div>}
                        </div>
                      </button>
                    ))}
                  </div>
                )}
              </div>
  )
}

export interface MacroMenuProps {
  open: boolean
  onToggle: () => void
  shown: PickerMacro[]
  hidden: number
  onShowAll: () => void
  onPick: (keys: string) => void
}

function sendLabel(bangRunning: boolean, isBang: boolean, busy: boolean): string {
  if (bangRunning) return "Running…"
  if (isBang) return "Run"
  return busy ? "Queue" : "Send"
}

function SendControl({ bangRunning, isBang, busy, draft, onSend }: NativeComposerProps) {
  return (
              <SendButton
                label={sendLabel(bangRunning, isBang, busy)}
                queue={busy && !isBang}
                disabled={!draft.trim() || bangRunning}
                onClick={onSend}
              />
  )
}
