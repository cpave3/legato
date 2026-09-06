import { useEffect, useRef, useState } from "react"
import { Settings2 } from "lucide-react"
import { apiFetch } from "../lib/api"
import { modelSettingsResponse, settingLabels, type ModelSettings, type ModelSettingsPatch, type ModelSettingsState } from "../lib/modelSettings"
import { Dropdown } from "./ui/Dropdown"

export function NativeModelSettings({ agentId, baseUrl, model, busy, onSaved }: {
  agentId: string; baseUrl: string; model: string; busy: boolean; onSaved: (state: ModelSettingsState) => void
}) {
  const [open, setOpen] = useState(false)
  const [state, setState] = useState<ModelSettingsState | null>(null)
  const [draft, setDraft] = useState<ModelSettingsPatch>({})
  const [reset, setReset] = useState(false)
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const form = useRef<HTMLFormElement>(null)
  const generation = useRef(0)
  useEffect(() => () => { generation.current++ }, [])
  useEffect(() => { if (open && !loading) form.current?.querySelector<HTMLElement>("select, input, button")?.focus() }, [open, loading])

  const close = () => { if (!saving) { generation.current++; setOpen(false); trigger.current?.focus() } }
  const show = async () => {
    if (open) { close(); return }
    const request = ++generation.current
    setOpen(true); setLoading(true); setError(null); setDraft({}); setReset(false); setState(null)
    try {
      const response = await apiFetch(baseUrl, `/api/agents/${agentId}/chimera/model`)
      const value = await modelSettingsResponse(response) as ModelSettingsState | null
      if (request !== generation.current) return
      if (value?.capabilities?.schemaVersion === 1 && value.model === model) setState(value)
    } catch (reason) { if (request === generation.current) setError(reason instanceof Error ? reason.message : String(reason)) }
    finally { if (request === generation.current) setLoading(false) }
  }
  const change = (key: keyof ModelSettings, value: string | number | boolean | null) => {
    setDraft((current) => {
      const next = { ...current, [key]: value }
      if (key === "thinking" && value === "off") { next.reasoningEffort = null; next.thinkingBudgetTokens = null }
      return next
    })
  }
  const apply = async () => {
    if (!state || busy || saving) return
    const request = generation.current
    setSaving(true); setError(null)
    try {
      const response = await apiFetch(baseUrl, `/api/agents/${agentId}/chimera/model`, {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ expectedModel: model, settings: draft, ...(reset ? { resetSettings: true } : {}) }),
      })
      const result = await modelSettingsResponse(response) as { settingsState?: ModelSettingsState }
      if (request !== generation.current) return
      if (!result?.settingsState || result.settingsState.model !== model) throw new Error("The model changed. Reopen its settings.")
      onSaved(result.settingsState); setOpen(false); trigger.current?.focus()
    } catch (reason) { if (request === generation.current) setError(reason instanceof Error ? reason.message : String(reason)) }
    finally { if (request === generation.current) setSaving(false) }
  }

  const field = (key: keyof ModelSettings) => {
    const capability = state?.capabilities.settings[key]
    if (capability?.status !== "supported") return null
    const inherited = state?.defaults[key] ?? capability.default
    const selected = Object.hasOwn(draft, key) ? draft[key] : reset ? null : state?.overrides[key]
    const thinking = Object.hasOwn(draft, "thinking") ? draft.thinking ?? state?.defaults.thinking : reset ? state?.defaults.thinking : state?.effective.thinking
    const disabled = saving || busy || ((key === "reasoningEffort" || key === "thinkingBudgetTokens") && thinking === "off")
    const defaultLabel = inherited === undefined ? "Default" : `Default (${String(inherited)})`
    return <label key={key} className="flex flex-col gap-1 text-xs text-zinc-300">
      {settingLabels[key]}
      {capability.type === "number" ? <input type="number" value={selected == null ? "" : String(selected)} placeholder={defaultLabel}
        disabled={disabled} min={capability.min} max={capability.max} step={capability.integer ? 1 : "any"}
        onChange={(event) => change(key, event.target.value === "" ? null : Number(event.target.value))}
        className="rounded border border-zinc-700 bg-zinc-950 p-1.5" />
        : <select value={selected == null ? "" : String(selected)} disabled={disabled}
          onChange={(event) => change(key, event.target.value === "" ? null : capability.type === "boolean" ? event.target.value === "true" : event.target.value)}
          className="rounded border border-zinc-700 bg-zinc-950 p-1.5">
          <option value="">{defaultLabel}</option>
          {(capability.type === "boolean" ? ["true", "false"] : capability.values ?? []).map((value) => <option key={value} value={value}>{value === "true" ? "On" : value === "false" ? "Off" : value}</option>)}
        </select>}
    </label>
  }
  const supported = state && Object.values(state.capabilities.settings).some((capability) => capability.status === "supported")
  return <Dropdown open={open} onOpenChange={(value) => { if (!value) close() }} minWidth={0}
    trigger={<button ref={trigger} type="button" title="Model settings" aria-label="Model settings" aria-expanded={open} aria-haspopup="dialog"
      onClick={() => { void show() }} className="rounded border border-zinc-700 p-1.5 text-zinc-400 hover:bg-zinc-800"><Settings2 size={12} /></button>}>
    <form ref={form} role="dialog" aria-label="Model settings" className="flex max-h-[65vh] w-72 max-w-[calc(100vw-2rem)] flex-col gap-3 overflow-auto p-3"
      onSubmit={(event) => { event.preventDefault(); void apply() }} onKeyDown={(event) => { if (event.key === "Escape") { event.stopPropagation(); close() } }}>
      <h3 className="text-sm font-medium text-zinc-200">Model settings</h3>
      <p className="break-all text-xs text-zinc-400">{model}</p>
      {state && <p className="break-all text-xs text-zinc-500">Resolved model: {state.capabilities.modelId}</p>}
      {loading ? <p className="text-xs text-zinc-400">Loading settings…</p> : !supported ? <p className="text-xs text-zinc-400">Settings information is unavailable for this model. Configure a capability source or model profile in Chimera.</p> : <>
        {field("thinking")}{field("reasoningEffort")}
        <details><summary className="cursor-pointer text-xs text-zinc-300">Advanced</summary><div className="mt-2 flex flex-col gap-3">
          {(Object.keys(settingLabels) as (keyof ModelSettings)[]).filter((key) => key !== "thinking" && key !== "reasoningEffort").map(field)}
        </div></details>
        <p className="text-xs text-zinc-500">Applies to this session. Default uses the configured model value.</p>
        {(Object.keys({ ...state?.defaults, ...state?.overrides }) as (keyof ModelSettings)[]).filter((key) => state?.capabilities.settings[key]?.status !== "supported").map((key) =>
          <p key={key} className="text-xs text-amber-300">{settingLabels[key]}: {state?.capabilities.settings[key]?.reason ?? "Support is unknown for this model."}</p>)}
        {busy && <p className="text-xs text-amber-300">Wait for the current run to finish before applying settings.</p>}
        <button type="button" disabled={busy || saving} onClick={() => { setReset(true); setDraft({}) }} className="text-left text-xs text-indigo-300">Reset to defaults</button>
      </>}
      {error && <p role="alert" className="text-xs text-red-400">{error}</p>}
      <div className="flex justify-end gap-2 text-xs">
        <button type="button" disabled={saving} onClick={close} className="rounded border border-zinc-700 px-2 py-1 text-zinc-300">Cancel</button>
        <button type="submit" disabled={!supported || busy || saving || loading} className="rounded bg-indigo-600 px-2 py-1 text-white disabled:opacity-40">{saving ? "Saving…" : "Apply"}</button>
      </div>
    </form>
  </Dropdown>
}
