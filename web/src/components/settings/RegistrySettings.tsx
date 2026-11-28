import { useEffect, useRef, useState } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { Copy, Lock, MoreHorizontal, RotateCcw } from "lucide-react"
import { fetchMovable, moveSettings, resetConfigSetting, setConfigSetting, type ConfigSetting } from "../../lib/configSettings"
import { queryKeys } from "../../lib/query"
import { cn } from "../../lib/utils"
import { byteUnit, formatValue, isZero, modeLabel } from "../../lib/settingValues"
import { SettingsRows, Toggle, primaryButton, quietButton } from "./SettingsUI"
import { useConfirm } from "../ui/useConfirm"
import { useShowKeys } from "./showKeys"
import { PickerControl, TagsControl } from "./Pickers"
import { partnersOf, settingPickers } from "../../lib/settingPickers"

const chip = "inline-flex items-center gap-1 rounded px-1.5 py-px text-[11px] leading-[18px]"

// The words for where a value comes from, as `legato config show
// --sources` says them: nothing for the default.
export function SourceChip({ setting }: { setting: ConfigSetting }) {
  if (setting.source === "config.yaml") return <span className={cn(chip, "border border-lw-line bg-lw-raised text-lw-text-2")} title="Set in config.yaml: change it there" aria-label="Pinned by config.yaml"><Lock size={10} aria-hidden />config.yaml</span>
  if (setting.source === "database") return <span className={cn(chip, "bg-lw-accent-soft text-lw-accent-text")}>Set here</span>
  return null
}

// SettingSource is a value's source and whether it waits for a restart,
// for a card that draws a setting with a control of its own.
export function SettingSource({ setting, onReset, busy }: { setting: ConfigSetting; onReset?: () => void; busy?: boolean }) {
  return (
    <span className="flex flex-wrap items-center gap-1.5" data-testid={`source-${setting.key}`}>
      <SourceChip setting={setting} />
      {setting.source === "database" && onReset && <ResetButton setting={setting} onReset={onReset} busy={busy} />}
      {setting.pending_restart && <span className={cn(chip, "bg-lw-needs-soft text-lw-needs")}>Restart to apply</span>}
    </span>
  )
}

function ResetButton({ setting, onReset, busy }: { setting: ConfigSetting; onReset: () => void; busy?: boolean }) {
  const to = "default" in setting ? formatValue(setting, setting.default) : ""
  const words = to && to !== "none" ? `Reset to ${to}` : "Reset"
  return <button type="button" className="inline-flex items-center gap-1 rounded px-1 text-[12px] text-lw-text-2 hover:text-lw-text disabled:opacity-50"
    onClick={onReset} disabled={busy} aria-label={`${words}: ${setting.title}`}>
    <RotateCcw size={11} aria-hidden />{words}
  </button>
}

type Outcome = { kind: "saved" | "signed" | "refused"; text: string }

// useSettingActions sets and resets one setting, refetching the registry.
function useSettingActions(baseUrl: string) {
  const queryClient = useQueryClient()
  const [busy, setBusy] = useState<Record<string, boolean>>({})
  const pending = useRef(new Set<string>())
  const [outcomes, setOutcomes] = useState<Record<string, Outcome | undefined>>({})
  const timers = useRef<Record<string, ReturnType<typeof setTimeout>>>({})
  useEffect(() => () => Object.values(timers.current).forEach(clearTimeout), [])
  const say = (key: string, outcome: Outcome | undefined, fade = false) => {
    clearTimeout(timers.current[key])
    setOutcomes((o) => ({ ...o, [key]: outcome }))
    if (fade) timers.current[key] = setTimeout(() => setOutcomes((o) => ({ ...o, [key]: undefined })), 4000)
  }
  const run = async (key: string, action: () => Promise<unknown>) => {
    if (pending.current.has(key)) return
    pending.current.add(key)
    setBusy((b) => ({ ...b, [key]: true }))
    say(key, undefined)
    try {
      const result = await action() as (ConfigSetting & { status?: string }) | undefined
      if (result?.status === "needs_signature") {
        say(key, { kind: "signed", text: "Sent for your signature, on your phone or with a passkey. It changes once you approve." })
      } else if (result?.pending_restart && "started_value" in result) {
        say(key, { kind: "saved", text: `Saved. Legato uses ${formatValue(result, result.started_value)} until it restarts.` })
      } else if (result?.pending_restart) {
        say(key, { kind: "saved", text: "Saved. It applies when Legato restarts." })
      } else {
        say(key, { kind: "saved", text: "Saved." }, true)
      }
      await queryClient.invalidateQueries({ queryKey: queryKeys.configSettings(baseUrl) })
      await queryClient.invalidateQueries({ queryKey: queryKeys.settings(baseUrl) })
      if (result?.status !== "needs_signature") return result
    } catch (reason) {
      say(key, { kind: "refused", text: reason instanceof Error ? reason.message : String(reason) })
    } finally {
      pending.current.delete(key)
      setBusy((b) => ({ ...b, [key]: false }))
    }
  }
  return {
    busy, outcomes,
    set: (key: string, value: unknown) => run(key, () => setConfigSetting(baseUrl, key, value)),
    reset: (key: string) => run(key, () => resetConfigSetting(baseUrl, key)),
  }
}

// A typed value's text, and the value it saves as.
function useTextField(setting: ConfigSetting) {
  const bytes = setting.unit === "bytes" ? byteUnit(Number(setting.value) || Number(setting.default) || 0) : undefined
  const format = (value: unknown) => {
    if (setting.kind === "strings") return ((value as string[] | null) ?? []).join(", ")
    if (setting.kind === "int" && isZero(value) && setting.default_label && setting.source === "default") return ""
    if (bytes && typeof value === "number") return String(value / bytes.factor)
    return String(value ?? "")
  }
  const parse = (text: string): unknown => {
    if (setting.kind === "strings") return text.split(",").map((s) => s.trim()).filter(Boolean)
    if (setting.kind === "int") return Math.round(Number(text) * (bytes?.factor ?? 1))
    return text
  }
  const unit = bytes ? bytes.label : setting.unit
  return { format, parse, unit }
}

// The row's control by kind. A switch or a menu saves at once; a typed
// value saves with Save or Enter and goes back with Cancel or Esc.
function SettingControl({ setting, disabled, onSet }: {
  setting: ConfigSetting; disabled: boolean
  onSet: (value: unknown) => Promise<ConfigSetting | undefined>
}) {
  switch (setting.kind) {
    case "bool":
      return <Toggle label={setting.title} disabled={disabled} on={Boolean(setting.value)} onChange={() => onSet(!setting.value)} />
    case "enum":
      return <EnumControl setting={setting} disabled={disabled} onSet={onSet} />
    default:
      return <TextControl setting={setting} disabled={disabled} onSet={onSet} />
  }
}

function EnumControl({ setting, disabled, onSet }: { setting: ConfigSetting; disabled: boolean; onSet: (value: unknown) => void }) {
  return (
    <select aria-label={setting.title} value={String(setting.value ?? "")} disabled={disabled}
      onChange={(e) => onSet(e.target.value)}
      className={cn("min-h-9 rounded-lg border border-lw-line-strong bg-lw-ground px-2 text-sm disabled:border-lw-line disabled:opacity-100", setting.source === "default" ? "text-lw-text-2" : "text-lw-text")}>
      {(setting.enum ?? []).map((option) => <option key={option} value={option}>{option || setting.default_label || "None"}</option>)}
    </select>
  )
}

// useDraft keeps a typed value's draft against the text it started from
// (baseline). A refresh moves both along unless the draft was edited; one
// that lands on an edited draft is a conflict, until Use or Keep settles it.
function useDraft(text: string) {
  const [draft, setDraft] = useState(text)
  const [baseline, setBaseline] = useState(text)
  const [observed, setObserved] = useState(text)
  if (observed !== text) {
    setObserved(text)
    if (draft === baseline || draft === text) {
      setDraft(text)
      setBaseline(text)
    }
  }
  const dirty = draft !== baseline
  const conflict = dirty && text !== baseline && draft !== text
  const settle = (value: string) => { setDraft(value); setBaseline(value) }
  return { draft, setDraft, dirty, conflict, settle, keep: () => setBaseline(text) }
}

function TextControl({ setting, disabled, onSet }: {
  setting: ConfigSetting; disabled: boolean
  onSet: (value: unknown) => Promise<ConfigSetting | undefined>
}) {
  const { text, unit, draft, setDraft, dirty, conflict, reload, keep, commit } = useTextSave(setting, disabled, onSet)
  // On a conflict, the value saved elsewhere while the draft was edited.
  const now = text || formatValue(setting, setting.value)
  return (
    <div className="flex flex-wrap items-center justify-end gap-2">
      <TextInput setting={setting} value={draft} unit={unit} disabled={disabled} greyed={setting.source === "default" && !dirty}
        onChange={setDraft} onEnter={() => void commit()} onEscape={dirty ? reload : undefined} />
      {dirty && !setting.pinned && <span className="flex gap-2">
        <button type="button" className={quietButton + " min-h-9"} disabled={disabled} onClick={reload}>Cancel</button>
        <button type="button" className={primaryButton + " min-h-9"} disabled={disabled || conflict} onClick={commit}>Save</button>
      </span>}
      {conflict && <div className="flex w-full flex-wrap items-center justify-end gap-2 text-xs text-lw-needs">
        <p role="status">Changed to {now} while you edited.</p>
        <button type="button" className={quietButton + " min-h-8 px-2.5 text-xs"} disabled={disabled} onClick={reload}>Use {now}</button>
        <button type="button" className={quietButton + " min-h-8 px-2.5 text-xs"} disabled={disabled} onClick={keep}>Keep {draft}</button>
      </div>}
    </div>
  )
}

// useTextSave is a typed value's draft and its save, which takes the value
// the server saved as the new clean text.
function useTextSave(setting: ConfigSetting, disabled: boolean, onSet: (value: unknown) => Promise<ConfigSetting | undefined>) {
  const { format, parse, unit } = useTextField(setting)
  const text = format(setting.value)
  const { draft, setDraft, dirty, conflict, settle, keep } = useDraft(text)
  const reload = () => settle(text)
  const commit = async () => {
    if (!dirty || conflict || disabled) return
    const saved = await onSet(parse(draft))
    if (saved && "value" in saved) settle(format(saved.value))
  }
  return { text, unit, draft, setDraft, dirty, conflict, reload, keep, commit }
}

// TextInput is the typed value with its unit: Enter saves, and Esc goes
// back when there is something to go back from.
function TextInput({ setting, value, unit, disabled, greyed, onChange, onEnter, onEscape }: {
  setting: ConfigSetting; value: string; unit?: string; disabled: boolean; greyed: boolean
  onChange: (value: string) => void; onEnter: () => void; onEscape?: () => void
}) {
  return (
    <span className="flex items-center gap-2">
      <input aria-label={setting.title} value={value} disabled={disabled} inputMode={setting.kind === "int" ? "numeric" : undefined}
        placeholder={setting.default_label}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") { e.preventDefault(); onEnter() }
          if (e.key === "Escape" && onEscape) { e.preventDefault(); onEscape() }
        }}
        className={cn("min-h-9 rounded-lg border border-lw-line-strong bg-lw-ground px-2.5 text-sm placeholder:text-lw-faint disabled:border-lw-line",
          setting.kind === "int" ? "w-24 text-right" : "w-60 max-w-full",
          greyed ? "text-lw-text-2" : "text-lw-text")} />
      {unit && <span className="text-[13px] text-lw-text-2">{unit}</span>}
    </span>
  )
}

// The menu of a row config.yaml pins: move it out, or copy its key.
function PinnedMenu({ setting, baseUrl, onCopy }: { setting: ConfigSetting; baseUrl: string; onCopy: () => void }) {
  const [open, setOpen] = useState(false)
  const { confirm, dialog } = useConfirm()
  const movable = useQuery({ queryKey: [baseUrl, "config-move"], queryFn: () => fetchMovable(baseUrl), enabled: !setting.secret, retry: false })
  const [state, setState] = useState<"idle" | "moving" | "restarting">("idle")
  const [error, setError] = useState("")
  const canMove = movable.data?.settings.includes(setting.key) ?? false
  const move = async () => {
    setOpen(false)
    if (!await confirm({ title: "Move setting and restart?", message: `${setting.title} will move out of config.yaml into the database. Legato will restart and connected clients will disconnect. A backup of config.yaml is kept.`, confirmLabel: "Move and restart", destructive: true })) return
    setState("moving")
    setError("")
    try {
      await moveSettings(baseUrl, { settings: [setting.key], entries: [] })
      setState("restarting")
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
      setState("idle")
    }
  }
  return <div className="relative">
    <button type="button" aria-label={`More for ${setting.title}`} aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen(!open)}
      className="flex h-9 w-9 items-center justify-center rounded-lg text-lw-text-2 hover:bg-lw-raised hover:text-lw-text"><MoreHorizontal size={16} aria-hidden /></button>
    {open && <div role="menu" aria-label={`${setting.title} menu`} className="absolute right-0 top-10 z-20 w-64 overflow-hidden rounded-lg border border-lw-line bg-lw-surface py-1 shadow-lg"
      onKeyDown={(e) => { if (e.key === "Escape") setOpen(false) }}>
      {canMove && <button type="button" role="menuitem" disabled={state !== "idle"} onClick={() => void move()} className="flex w-full flex-col px-3 py-2 text-left text-sm hover:bg-lw-raised">
        Move out of config.yaml<span className="text-xs text-lw-text-2">Saves it here and restarts Legato</span>
      </button>}
      {movable.isError && <p role="alert" className="px-3 py-2 text-xs text-lw-error">Could not check whether it can move: {movable.error.message}</p>}
      <button type="button" role="menuitem" onClick={() => { setOpen(false); onCopy() }} className="flex w-full flex-col px-3 py-2 text-left text-sm hover:bg-lw-raised">
        Copy key<span className="font-lw-mono text-xs text-lw-text-2">{setting.key}</span>
      </button>
    </div>}
    {error && <p role="alert" className="mt-1 text-xs text-lw-error">{error}</p>}
    {state !== "idle" && <p role="status" className="mt-1 text-xs text-lw-text-2">{state === "moving" ? "Moving…" : "Restarting…"}</p>}
    {dialog}
  </div>
}

function copyText(text: string): Promise<void> {
  try {
    return navigator.clipboard?.writeText(text) ?? Promise.reject(new Error("no clipboard"))
  } catch (reason) {
    return Promise.reject(reason)
  }
}

// SecretState says whether Legato can read the secret a setting names, and
// how to store it: the command, since a browser never writes a secret.
function SecretState({ setting }: { setting: ConfigSetting }) {
  const [open, setOpen] = useState(false)
  const [copied, setCopied] = useState(false)
  const ref = String(setting.value ?? "")
  const name = ref.startsWith("legato://") ? ref.slice("legato://".length) : ""
  const command = name ? `legato secret add ${name}` : ""
  const missing = setting.secret_status === "missing"
  return <div className="mt-1.5 flex flex-col gap-1.5">
    <span className="flex flex-wrap items-center gap-2">
      {missing
        ? <span className={cn(chip, "bg-lw-error-soft text-lw-error")}>Not found</span>
        : <span className={cn(chip, "bg-lw-live-soft text-lw-live")}>Stored</span>}
      {!missing && command && !open && <button type="button" className="text-[12px] text-lw-text-2 hover:text-lw-text" onClick={() => setOpen(true)}>Replace</button>}
      {missing && !command && <span className="text-xs text-lw-text-2">Legato cannot read it from 1Password.</span>}
    </span>
    {command && (missing || open) && <span className="flex flex-wrap items-center gap-2 text-xs text-lw-text-2">
      {missing ? "Store it on this server with" : "Replace it on this server with"}
      <code className="rounded bg-lw-raised px-1.5 py-0.5 font-lw-mono text-lw-text">{command}</code>
      <button type="button" aria-label={`Copy ${command}`} className="inline-flex items-center gap-1 text-lw-text-2 hover:text-lw-text"
        onClick={() => void copyText(command).then(() => setCopied(true), () => setCopied(false))}><Copy size={11} aria-hidden />{copied ? "Copied" : "Copy"}</button>
    </span>}
  </div>
}

// The key line: shown on hover and focus, or always with Show config keys.
function KeyLine({ keyName, extra, onCopy }: { keyName: string; extra?: string; onCopy: () => void }) {
  return <button type="button" onClick={onCopy} aria-label={`Copy key ${keyName}`} title="Copy key"
    className="mt-1 inline-flex items-center gap-1.5 font-lw-mono text-[11.5px] text-lw-text-2 hover:text-lw-text">
    <span>{keyName}{extra ? ` · ${extra}` : ""}</span><Copy size={11} aria-hidden />
  </button>
}

function useRowKey(keyName: string) {
  const showKeys = useShowKeys()
  const [near, setNear] = useState(false)
  const [copied, setCopied] = useState(false)
  const copy = () => { void copyText(keyName).then(() => { setCopied(true); setTimeout(() => setCopied(false), 2000) }, () => setCopied(false)) }
  const handlers = {
    onMouseEnter: () => setNear(true),
    onMouseLeave: () => setNear(false),
    onFocus: () => setNear(true),
    onBlur: (e: React.FocusEvent) => { if (!e.currentTarget.contains(e.relatedTarget as Node)) setNear(false) },
  }
  return { shown: showKeys || near, copy, copied, handlers }
}

// RegistrySettingRow is one setting of the registry: its label and what it
// does, its source only when not the default, and its control.
export function RegistrySettingRow({ setting, baseUrl, actions, showMode }: { setting: ConfigSetting; baseUrl: string; actions?: ReturnType<typeof useSettingActions>; showMode?: boolean }) {
  const own = useSettingActions(baseUrl)
  const { busy, outcomes, set, reset } = actions ?? own
  const key = useRowKey(setting.key)
  // An agent's row speaks for its model or tier too: their refusals show here.
  const partners = [setting.key, ...partnersOf(setting.key)]
  const outcome = partners.map((k) => outcomes[k]).find((o) => o?.kind === "refused") ?? outcomes[setting.key]
  const defaultWords = "default" in setting && setting.source !== "default" ? `default ${formatValue(setting, setting.default)}` : undefined
  return (
    <div className="flex min-h-16 flex-wrap items-center gap-x-5 gap-y-2 px-4 py-3.5" id={`setting-${setting.key}`} data-testid={`setting-${setting.key}`} {...key.handlers}>
      <div className="min-w-0 flex-[1_1_16rem]">
        <div className="flex flex-wrap items-center gap-2">
          <p className="text-sm text-lw-text">{setting.title}</p>
          <SourceChip setting={setting} />
          {setting.source === "database" && <ResetButton setting={setting} busy={busy[setting.key]} onReset={() => void reset(setting.key)} />}
          {setting.pending_restart && <span className={cn(chip, "bg-lw-needs-soft text-lw-needs")}>Restart to apply</span>}
          {setting.own_rule && <span className={cn(chip, "bg-[var(--theme-selection,rgba(139,92,246,0.2))] text-[var(--theme-keyword,#c4b5fd)]")} title="Changed only with a signed approval">Signed</span>}
          {showMode && <span className="text-[11px] text-lw-text-2">{modeLabel(setting.mode)}</span>}
        </div>
        {(setting.help ?? setting.description) && <p className="mt-0.5 text-[12.5px] text-lw-text-2">{setting.help ?? setting.description}</p>}
        {setting.secret && setting.secret_status && <SecretState setting={setting} />}
        {key.shown && <KeyLine keyName={setting.key} extra={defaultWords} onCopy={key.copy} />}
        {key.copied && <p role="status" className="text-xs text-lw-text-2">Key copied.</p>}
        {outcome?.kind === "refused" && <p role="alert" className="mt-1 text-xs text-lw-error">{outcome.text}</p>}
        {busy[setting.key] && <p role="status" className="mt-1 text-xs text-lw-text-2">Saving…</p>}
        {!busy[setting.key] && outcome && outcome.kind !== "refused" && <p role="status" className="mt-1 text-xs text-lw-text-2">{outcome.text}</p>}
      </div>
      <div className={cn("ml-auto flex min-w-0 items-center gap-1", busy[setting.key] && "opacity-60")}>
        {settingPickers[setting.key]
          ? <PickerControl setting={setting} picker={settingPickers[setting.key]} disabled={setting.pinned || partners.some((k) => busy[k])}
            setKey={async (k, v) => (await set(k, v)) !== undefined} resetKey={async (k) => (await reset(k)) !== undefined} />
          : setting.kind === "strings"
            ? <TagsControl setting={setting} disabled={setting.pinned || busy[setting.key]} onSet={(v) => void set(setting.key, v)} />
            : <SettingControl setting={setting} disabled={setting.pinned || busy[setting.key]} onSet={(value) => set(setting.key, value)} />}
        {setting.pinned && <PinnedMenu setting={setting} baseUrl={baseUrl} onCopy={key.copy} />}
      </div>
    </div>
  )
}

// RegistrySettings lists settings of the registry as rows: those whose key
// starts with one of prefixes, less those in exclude (shown by a card of
// their own). showMode puts each row's "applies" words on the row, when
// its group does not share one.
export function RegistrySettings({ settings, baseUrl, prefixes, exclude = [], showMode }: { settings: ConfigSetting[]; baseUrl: string; prefixes: string[]; exclude?: string[]; showMode?: boolean }) {
  const actions = useSettingActions(baseUrl)
  const shown = settings.filter((s) => prefixes.some((p) => s.key === p || s.key.startsWith(p + ".")) && !exclude.includes(s.key))
  if (shown.length === 0) return null
  return (
    <SettingsRows>
      {shown.map((setting) => <RegistrySettingRow key={setting.key} setting={setting} baseUrl={baseUrl} actions={actions} showMode={showMode} />)}
    </SettingsRows>
  )
}

// FileOnlyRow is a setting only config.yaml holds: read-only, with its
// value and where to change it.
export function FileOnlyRow({ keyName, title, value }: { keyName: string; title: string; value?: string }) {
  const key = useRowKey(keyName)
  return <div id={`setting-${keyName}`} data-testid={`setting-${keyName}`} tabIndex={-1} className="flex min-h-14 flex-wrap items-center gap-x-5 gap-y-1 px-4 py-3" {...key.handlers}>
    <div className="min-w-0 flex-[1_1_16rem]">
      <div className="flex flex-wrap items-center gap-2">
        <p className="text-sm text-lw-text">{title}</p>
        <span className={cn(chip, "border border-lw-line bg-lw-raised text-lw-text-2")}><Lock size={10} aria-hidden />config.yaml only</span>
      </div>
      <p className="mt-0.5 text-[12.5px] text-lw-text-2">Read-only here. Change it in config.yaml, then restart Legato.</p>
      {key.shown && <KeyLine keyName={keyName} onCopy={key.copy} />}
    </div>
    <span className="ml-auto break-all font-lw-mono text-sm text-lw-text-2">{value || "not set"}</span>
  </div>
}
