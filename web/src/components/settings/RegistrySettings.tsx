import { useEffect, useState } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { Lock, RotateCcw, SlidersHorizontal } from "lucide-react"
import { moveSettings, resetConfigSetting, setConfigSetting, type ConfigSetting } from "../../lib/configSettings"
import { queryKeys } from "../../lib/query"
import { cn } from "../../lib/utils"
import { SettingsRows, Toggle } from "./SettingsUI"
import { useReeveName } from "../../lib/reeveName"

// SettingSource says where a setting's value comes from: the default,
// config.yaml (pinned, with a lock), or set here (with Reset), and whether it
// waits for a restart.
export function SettingSource({ setting, onReset, busy }: { setting: ConfigSetting; onReset?: () => void; busy?: boolean }) {
  return (
    <span className="flex flex-wrap items-center gap-1.5 text-[11px]" data-testid={`source-${setting.key}`}>
      {setting.source === "config.yaml" && (
        <span className="inline-flex items-center gap-1 rounded bg-lw-raised px-1.5 py-0.5 text-lw-text-2" title="Set in config.yaml: change it there">
          <Lock size={10} aria-hidden /> config.yaml
        </span>
      )}
      {setting.source === "default" && <span className="rounded px-1.5 py-0.5 text-lw-faint">default</span>}
      {setting.source === "database" && (
        <>
          <span className="rounded bg-lw-accent-soft px-1.5 py-0.5 text-lw-accent-text">set here</span>
          {onReset && (
            <button type="button" className="inline-flex items-center gap-1 rounded px-1 py-0.5 text-lw-text-2 hover:text-lw-text disabled:opacity-50"
              onClick={onReset} disabled={busy} aria-label={`Reset ${setting.title}`}>
              <RotateCcw size={10} aria-hidden /> Reset
            </button>
          )}
        </>
      )}
      {setting.pending_restart && <span className="rounded bg-lw-needs-soft px-1.5 py-0.5 text-lw-needs">applies at restart</span>}
    </span>
  )
}

// useSettingActions sets and resets one setting, refetching the registry.
function useSettingActions(baseUrl: string) {
  const queryClient = useQueryClient()
  const reeve = useReeveName()
  const [busy, setBusy] = useState("")
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [notices, setNotices] = useState<Record<string, string>>({})
  const run = async (key: string, action: () => Promise<unknown>) => {
    setBusy(key)
    setErrors((e) => ({ ...e, [key]: "" }))
    setNotices((n) => ({ ...n, [key]: "" }))
    try {
      const result = await action() as { status?: string } | undefined
      if (result?.status === "needs_signature") {
        setNotices((n) => ({ ...n, [key]: `This is one of ${reeve.mid}'s own rules: it changes once you approve it on your phone or with a passkey.` }))
      }
      await queryClient.invalidateQueries({ queryKey: queryKeys.configSettings(baseUrl) })
      await queryClient.invalidateQueries({ queryKey: queryKeys.settings(baseUrl) })
    } catch (reason) {
      setErrors((e) => ({ ...e, [key]: reason instanceof Error ? reason.message : String(reason) }))
    } finally {
      setBusy("")
    }
  }
  return {
    busy, errors, notices,
    set: (key: string, value: unknown) => run(key, () => setConfigSetting(baseUrl, key, value)),
    reset: (key: string) => run(key, () => resetConfigSetting(baseUrl, key)),
    move: (key: string) => run(key, () => moveSettings(baseUrl, { settings: [key], entries: [] })),
  }
}

// SettingControl edits a setting by its kind; a pinned one is shown, not
// edited.
function SettingControl({ setting, disabled, onSet }: { setting: ConfigSetting; disabled: boolean; onSet: (value: unknown) => void }) {
  const text = setting.kind === "strings" ? (setting.value as string[] ?? []).join(", ") : String(setting.value ?? "")
  const [draft, setDraft] = useState(text)
  useEffect(() => setDraft(text), [text])
  const label = setting.title
  switch (setting.kind) {
    case "bool":
      return disabled
        ? <span className="text-sm text-lw-text-2">{setting.value ? "On" : "Off"}</span>
        : <Toggle label={label} on={Boolean(setting.value)} onChange={() => onSet(!setting.value)} />
    case "enum":
      return (
        <select aria-label={label} value={String(setting.value ?? "")} disabled={disabled}
          onChange={(e) => onSet(e.target.value)}
          className="min-h-9 rounded-lg border border-lw-line bg-lw-ground px-2 text-sm text-lw-text disabled:opacity-60">
          {(setting.enum ?? []).map((option) => <option key={option} value={option}>{option || "none"}</option>)}
        </select>
      )
    default: {
      const commit = () => {
        if (draft === text) return
        if (setting.kind === "strings") onSet(draft.split(",").map((s) => s.trim()).filter(Boolean))
        else if (setting.kind === "int") onSet(Number(draft))
        else onSet(draft)
      }
      return (
        <input aria-label={label} value={draft} disabled={disabled}
          onChange={(e) => setDraft(e.target.value)} onBlur={commit}
          onKeyDown={(e) => { if (e.key === "Enter") commit() }}
          className="min-h-9 w-56 max-w-full rounded-lg border border-lw-line bg-lw-ground px-2 text-sm text-lw-text disabled:opacity-60" />
      )
    }
  }
}

// RegistrySettingRow is one setting of the registry: what it is, where its
// value comes from, and its control.
export function RegistrySettingRow({ setting, baseUrl, actions }: { setting: ConfigSetting; baseUrl: string; actions?: ReturnType<typeof useSettingActions> }) {
  const own = useSettingActions(baseUrl)
  const { busy, errors, notices, set, reset, move } = actions ?? own
  return (
    <div className="flex min-h-16 flex-wrap items-center gap-3 px-4 py-3" id={`setting-${setting.key}`} data-testid={`setting-${setting.key}`}>
      <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-lw-raised text-lw-link">
        {setting.pinned ? <Lock size={15} aria-label="Pinned by config.yaml" /> : <SlidersHorizontal size={15} aria-hidden />}
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <p className="text-sm text-lw-text">{setting.title}</p>
          {setting.own_rule && <span className="rounded bg-[#8b5cf6]/20 px-1.5 text-[10px] text-[#c4b5fd]" title="Changed only with a signed approval">signed</span>}
          <code className="text-[11px] text-lw-faint">{setting.key}</code>
          <SettingSource setting={setting} busy={busy === setting.key} onReset={() => void reset(setting.key)} />
        </div>
        {setting.description && <p className="mt-0.5 text-xs text-lw-text-2">{setting.description}</p>}
        {setting.pinned && !setting.secret && (
          <button type="button" className="mt-1 text-xs text-lw-link hover:underline disabled:opacity-50" disabled={busy === setting.key}
            onClick={() => void move(setting.key)}>
            Move to the database (restarts Legato)
          </button>
        )}
        {errors[setting.key] && <p role="alert" className="mt-1 text-xs text-lw-error">{errors[setting.key]}</p>}
        {notices[setting.key] && <p role="status" className="mt-1 text-xs text-lw-text-2">{notices[setting.key]}</p>}
      </div>
      <div className={cn("shrink-0", busy === setting.key && "opacity-60")}>
        <SettingControl setting={setting} disabled={setting.pinned || busy === setting.key} onSet={(value) => void set(setting.key, value)} />
      </div>
    </div>
  )
}

// RegistrySettings lists settings of the registry as generic rows: those
// whose key starts with one of prefixes, less those in exclude (shown by a
// section of their own).
export function RegistrySettings({ settings, baseUrl, prefixes, exclude = [] }: { settings: ConfigSetting[]; baseUrl: string; prefixes: string[]; exclude?: string[] }) {
  const actions = useSettingActions(baseUrl)
  const shown = settings.filter((s) => prefixes.some((p) => s.key === p || s.key.startsWith(p + ".")) && !exclude.includes(s.key))
  if (shown.length === 0) return null
  return (
    <SettingsRows>
      {shown.map((setting) => <RegistrySettingRow key={setting.key} setting={setting} baseUrl={baseUrl} actions={actions} />)}
    </SettingsRows>
  )
}
