import { useModalLayer } from "./ui/useModalLayer"
import { useEffect, useRef, useState } from "react"
import { Settings2 } from "lucide-react"
import { apiFetch } from "../lib/api"
import { modelSettingsResponse, settingLabels, type ModelSettings, type ModelSettingsPatch, type ModelSettingsState } from "../lib/modelSettings"
import {
  changeDraft, currentThinking, defaultSettingLabel, hasSupportedSetting, needsThinking, parseNumberSetting, parseSelectedSetting,
  selectedSetting, settingOptionLabel, settingOptions, supportedCapability, unsupportedConfigured,
  type ModelSettingCapability, type ModelSettingsDraft, type ModelSettingValue,
} from "../lib/modelSettingsForm"
import { reasonText } from "../lib/nativeAgentView"
import { Dropdown } from "./ui/Dropdown"
import { LoadingState } from "./ui/LoadingState"

// NativeModelSettings is a button that opens the settings of the session's
// model beside it.
export function NativeModelSettings({ agentId, baseUrl, model, busy, onSaved }: {
  agentId: string; baseUrl: string; model: string; busy: boolean; onSaved: (state: ModelSettingsState) => void
}) {
  const [open, setOpen] = useState(false)
  const trigger = useRef<HTMLButtonElement>(null)
  const close = () => { setOpen(false); trigger.current?.focus() }
  return <Dropdown kind="dialog" open={open} onOpenChange={(value) => { if (!value) setOpen(false) }} minWidth={0}
    trigger={<button ref={trigger} type="button" title="Model settings" aria-label="Model settings" aria-expanded={open} aria-haspopup="dialog"
      onClick={() => open ? close() : setOpen(true)} className="rounded border border-zinc-700 p-1.5 text-zinc-400 hover:bg-zinc-800"><Settings2 size={12} /></button>}>
    {open && <NativeModelSettingsForm agentId={agentId} baseUrl={baseUrl} model={model} busy={busy} onSaved={onSaved} onClose={close} />}
  </Dropdown>
}

// NativeModelSettingsForm is every setting of the session's model that it
// takes, read when the form opens and applied together.
export function NativeModelSettingsForm({ agentId, baseUrl, model, busy, onSaved, onClose }: {
  agentId: string; baseUrl: string; model: string; busy: boolean; onSaved: (state: ModelSettingsState) => void; onClose: () => void
}) {
  const { state, loading, error, setError, generationRef } = useModelSettings(agentId, baseUrl, model)
  const [draft, setDraft] = useState<ModelSettingsPatch>({})
  const [reset, setReset] = useState(false)
  const { saving, apply } = useApplyModelSettings({ agentId, baseUrl, model, generationRef, setError, onSaved, onClose })
  const form = useRef<HTMLFormElement>(null)

  const close = () => { if (!saving) { generationRef.current++; onClose() } }
  useModalLayer({ open: true, onClose: close, containerRef: form, dismissible: !saving })
  const supported = hasSupportedSetting(state)
  return <form ref={form} role="dialog" aria-modal="true" aria-label="Model settings" className="flex max-h-[calc(100dvh-2rem)] w-72 max-w-[calc(100vw-2rem)] flex-col gap-3 overflow-auto p-3"
    onSubmit={(event) => { event.preventDefault(); if (state && !busy) void apply(draft, reset) }}>
    <ModelHeading model={model} resolved={state?.capabilities.modelId} />
    {loading ? <LoadingState size="inline" label="Loading settings…" /> : !supported ? <p className="text-xs text-zinc-400">Settings information is unavailable for this model. Configure a capability source or model profile in Chimera.</p>
      : <ModelSettingsFields values={{ state, draft, reset }} busy={busy} saving={saving}
        onChange={(key, value) => setDraft((current) => changeDraft(current, key, value))}
        onReset={() => { setReset(true); setDraft({}) }} />}
    {error && <p role="alert" className="text-xs text-red-400">{error}</p>}
    <ModelSettingsActions saving={saving} canApply={supported && !busy && !loading} onCancel={close} />
  </form>
}

function ModelHeading({ model, resolved }: { model: string; resolved?: string }) {
  return <>
    <h3 className="text-sm font-medium text-zinc-200">Model settings</h3>
    <p className="break-all text-xs text-zinc-400">{model}</p>
    {resolved !== undefined && <p className="break-all text-xs text-zinc-500">Resolved model: {resolved}</p>}
  </>
}

function ModelSettingsActions({ saving, canApply, onCancel }: { saving: boolean; canApply: boolean; onCancel: () => void }) {
  return <div className="flex justify-end gap-2 text-xs">
    <button type="button" disabled={saving} onClick={onCancel} className="rounded border border-zinc-700 px-2 py-1 text-zinc-300">Cancel</button>
    <button type="submit" disabled={!canApply || saving} className="rounded bg-indigo-600 px-2 py-1 text-white disabled:opacity-40">{saving ? "Saving…" : "Apply"}</button>
  </div>
}

// ModelSettingsFields is the settings the model takes, the ones it was given
// but does not take, and Reset.
function ModelSettingsFields({ values, busy, saving, onChange, onReset }: {
  values: ModelSettingsDraft; busy: boolean; saving: boolean
  onChange: (key: keyof ModelSettings, value: ModelSettingValue) => void; onReset: () => void
}) {
  const { state } = values
  const field = (key: keyof ModelSettings) => <ModelSettingField key={key} settingKey={key} values={values} locked={saving || busy} onChange={onChange} />
  return <>
    {field("thinking")}{field("reasoningEffort")}
    <details><summary className="cursor-pointer text-xs text-zinc-300">Advanced</summary><div className="mt-2 flex flex-col gap-3">
      {(Object.keys(settingLabels) as (keyof ModelSettings)[]).filter((key) => key !== "thinking" && key !== "reasoningEffort").map(field)}
    </div></details>
    <p className="text-xs text-zinc-500">Applies to this session. Default uses the configured model value.</p>
    {unsupportedConfigured(state).map((key) =>
      <p key={key} className="text-xs text-amber-300">{settingLabels[key]}: {state?.capabilities.settings[key]?.reason ?? "Support is unknown for this model."}</p>)}
    {busy && <p className="text-xs text-amber-300">Wait for the current run to finish before applying settings.</p>}
    <button type="button" disabled={busy || saving} onClick={onReset} className="text-left text-xs text-indigo-300">Reset to defaults</button>
  </>
}

// useApplyModelSettings saves a draft once at a time; an answer after the
// form closed (a newer generation) changes nothing.
function useApplyModelSettings({ agentId, baseUrl, model, generationRef, setError, onSaved, onClose }: {
  agentId: string; baseUrl: string; model: string; generationRef: { current: number }
  setError: (error: string | null) => void; onSaved: (state: ModelSettingsState) => void; onClose: () => void
}) {
  const [saving, setSaving] = useState(false)
  const apply = async (draft: ModelSettingsPatch, reset: boolean) => {
    if (saving) return
    const request = generationRef.current
    const current = () => request === generationRef.current
    setSaving(true); setError(null)
    try {
      const saved = await saveModelSettings(baseUrl, agentId, model, draft, reset)
      if (!current()) return
      onSaved(saved); onClose()
    } catch (reason) { if (current()) setError(reasonText(reason)) }
    finally { if (current()) setSaving(false) }
  }
  return { saving, apply }
}

// useModelSettings reads the settings of the session's model once, and keeps
// a generation so that an answer after the form closed changes nothing.
function useModelSettings(agentId: string, baseUrl: string, model: string) {
  const [state, setState] = useState<ModelSettingsState | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const generationRef = useRef(0)
  useEffect(() => () => { generationRef.current++ }, [])

  useEffect(() => {
    const request = ++generationRef.current
    apiFetch(baseUrl, `/api/agents/${agentId}/chimera/model`)
      .then((response) => modelSettingsResponse(response) as Promise<ModelSettingsState | null>)
      .then((value) => {
        if (request !== generationRef.current) return
        if (value?.capabilities?.schemaVersion === 1 && value.model === model) setState(value)
      })
      .catch((reason) => { if (request === generationRef.current) setError(reasonText(reason)) })
      .finally(() => { if (request === generationRef.current) setLoading(false) })
  }, [agentId, baseUrl, model])
  return { state, loading, error, setError, generationRef }
}

// saveModelSettings applies the draft and returns the session's new settings,
// refusing them when the session has moved to another model.
async function saveModelSettings(baseUrl: string, agentId: string, model: string, draft: ModelSettingsPatch, reset: boolean) {
  const response = await apiFetch(baseUrl, `/api/agents/${agentId}/chimera/model`, {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ expectedModel: model, settings: draft, ...(reset ? { resetSettings: true } : {}) }),
  })
  const result = await modelSettingsResponse(response) as { settingsState?: ModelSettingsState }
  if (!result?.settingsState || result.settingsState.model !== model) throw new Error("The model changed. Reopen its settings.")
  return result.settingsState
}

// ModelSettingField is one setting the model takes, as a number or a choice.
function ModelSettingField({ settingKey, values, locked, onChange }: {
  settingKey: keyof ModelSettings; values: ModelSettingsDraft; locked: boolean; onChange: (key: keyof ModelSettings, value: ModelSettingValue) => void
}) {
  const capability = supportedCapability(values.state, settingKey)
  if (!capability) return null
  const inherited = values.state?.defaults[settingKey] ?? capability.default
  const selected = selectedSetting(values, settingKey)
  const disabled = locked || (needsThinking(settingKey) && currentThinking(values) === "off")
  const control = { capability, value: selected == null ? "" : String(selected), disabled, defaultLabel: defaultSettingLabel(inherited), onChange: (value: ModelSettingValue) => onChange(settingKey, value) }
  return <label className="flex flex-col gap-1 text-xs text-zinc-300">
    {settingLabels[settingKey]}
    {capability.type === "number" ? <NumberSetting {...control} /> : <ChoiceSetting {...control} />}
  </label>
}

interface SettingControlProps {
  capability: ModelSettingCapability; value: string; disabled: boolean; defaultLabel: string; onChange: (value: ModelSettingValue) => void
}

function NumberSetting({ capability, value, disabled, defaultLabel, onChange }: SettingControlProps) {
  return <input type="number" value={value} placeholder={defaultLabel}
    disabled={disabled} min={capability.min} max={capability.max} step={capability.integer ? 1 : "any"}
    onChange={(event) => onChange(parseNumberSetting(event.target.value))}
    className="rounded border border-zinc-700 bg-zinc-950 p-1.5" />
}

function ChoiceSetting({ capability, value, disabled, defaultLabel, onChange }: SettingControlProps) {
  return <select value={value} disabled={disabled}
    onChange={(event) => onChange(parseSelectedSetting(capability, event.target.value))}
    className="rounded border border-zinc-700 bg-zinc-950 p-1.5">
    <option value="">{defaultLabel}</option>
    {settingOptions(capability).map((option) => <option key={option} value={option}>{settingOptionLabel(option)}</option>)}
  </select>
}
