import { useState } from "react"
import { X } from "lucide-react"
import type { ConfigSetting } from "../../lib/configSettings"
import { cn } from "../../lib/utils"
import { usePickerData, type AgentPicker, type Picker } from "../../lib/settingPickers"

// Save or reset one key: true once it is saved, false when refused.
type SaveKey = (key: string, value: unknown) => Promise<boolean>
type ResetKey = (key: string) => Promise<boolean>

const menu = "min-h-9 max-w-full rounded-lg border border-lw-line-strong bg-lw-ground px-2 text-sm disabled:border-lw-line"

// A menu over known values. A saved value the list lacks stays chosen and
// is shown as an error, never as a blank.
function Menu({ label, value, options, empty, disabled, missing, onChange, greyed }: {
  label: string; value: string; options: { value: string; label: string }[]; empty?: string
  disabled: boolean; missing: (value: string) => string; onChange: (value: string) => void; greyed?: boolean
}) {
  const known = value === "" ? empty !== undefined : options.some((o) => o.value === value)
  return <span className="flex flex-col items-end gap-1">
    <select aria-label={label} value={value} disabled={disabled} aria-invalid={!known || undefined} onChange={(e) => onChange(e.target.value)}
      className={cn(menu, !known ? "border-lw-error text-lw-error" : greyed ? "text-lw-text-2" : "text-lw-text")}>
      {empty !== undefined && <option value="">{empty}</option>}
      {!known && value !== "" && <option value={value}>{value}</option>}
      {options.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
    </select>
    {!known && <span role="alert" className="text-xs text-lw-error">{missing(value)}</span>}
  </span>
}

const agentNames: Record<string, string> = { "claude-code": "Claude Code", codex: "Codex", chimera: "Chimera" }
const agentName = (id: string) => agentNames[id] ?? id

// unsetAgent is what an agent setting's empty choice says: the default
// agent by name, unless unset means something else (the workers' agent).
function unsetAgent(defaultLabel: string | undefined, defaultAgent: string): string {
  const meansDefault = !defaultLabel || defaultLabel === "Same as the default agent" || defaultLabel in agentNames
  if (!meansDefault) return defaultLabel
  return defaultAgent ? `Default agent · ${agentName(defaultAgent)}` : "Default agent"
}

// AgentPickerControl is an agent menu, with its model or tier beside it
// when the registry pairs one: one setting in two keys.
function AgentPickerControl({ setting, picker, disabled, setKey, resetKey }: {
  setting: ConfigSetting; picker: AgentPicker; disabled: boolean
  setKey: SaveKey; resetKey: ResetKey
}) {
  const data = usePickerData()
  const agent = String(setting.value ?? "")
  // Unset, the agent is the fallback setting's (the conductor takes the
  // workers'), else the default agent.
  const fallbackAgent = (picker.fallback ? String(data.settings[picker.fallback]?.value ?? "") : "") || data.defaultAgent
  const effective = agent || fallbackAgent
  const partner = (key?: string) => key ? data.settings[key] : undefined
  const modelOrTier = partner(picker.modelOrTier), tier = partner(picker.tier), model = partner(picker.model)
  const tiers = data.tiers[effective] ?? [], models = data.models[effective] ?? []
  // The model menu: a model or a tier, one key or two.
  const second = modelOrTier ?? tier ?? model
  const options = [
    ...(modelOrTier || tier ? tiers.map((t) => ({ value: `tier:${t}`, label: `Tier: ${t}` })) : []),
    ...(modelOrTier || model ? models.map((m) => ({ value: `model:${m}`, label: m })) : []),
  ]
  const current = modelOrTier ? String(modelOrTier.value ?? "")
    : tier?.value ? `tier:${tier.value}` : model?.value ? `model:${model.value}` : ""
  // A one-key model or tier may name either; read it as whichever it is.
  const currentValue = modelOrTier && current !== "" ? (tiers.includes(current) ? `tier:${current}` : `model:${current}`) : current
  // Each choice clears what would conflict first, and saves only once that
  // is done: the server checks the whole configuration, so a tier saved
  // beside a model, or an old agent's tier beside a new agent, is refused.
  const chooseSecond = async (choice: string) => {
    const [kind, name] = choice === "" ? ["", ""] : [choice.slice(0, choice.indexOf(":")), choice.slice(choice.indexOf(":") + 1)]
    if (modelOrTier) { await (name === "" ? resetKey(modelOrTier.key) : setKey(modelOrTier.key, name)); return }
    for (const [s, k] of [[tier, "tier"], [model, "model"]] as const) {
      if (s?.value && kind !== k && !await resetKey(s.key)) return
    }
    const target = kind === "tier" ? tier : kind === "model" ? model : undefined
    if (target) await setKey(target.key, name)
  }
  const chooseAgent = async (choice: string) => {
    // A model or tier of the old agent means nothing to the new one.
    const next = choice || fallbackAgent
    for (const s of [modelOrTier, tier, model]) {
      if (!s?.value) continue
      const v = String(s.value)
      if (!(data.tiers[next] ?? []).includes(v) && !(data.models[next] ?? []).includes(v) && !await resetKey(s.key)) return
    }
    await (choice === "" ? resetKey(setting.key) : setKey(setting.key, choice))
  }
  const secondDisabled = disabled || Boolean(second?.pinned)
  return <span className="flex flex-wrap items-start justify-end gap-2">
    <Menu label={second ? `${setting.title} agent` : setting.title} value={agent} disabled={disabled} greyed={setting.source === "default"}
      empty={picker.fallback ? `${setting.default_label ?? "Fallback"} · ${agentName(fallbackAgent)}` : unsetAgent(setting.default_label, data.defaultAgent)}
      options={data.agents.map((a) => ({ value: a, label: agentName(a) }))}
      missing={(v) => `${v} isn't an agent here`} onChange={(v) => void chooseAgent(v)} />
    {second && <span id={`setting-${second.key}`} className="contents">
      <Menu label={`${setting.title} model or tier`} value={currentValue} disabled={secondDisabled} greyed={!current}
        empty={second.default_label ?? "Default model"} options={options}
        missing={(v) => `${v.slice(v.indexOf(":") + 1)} isn't a model or tier of ${agentName(effective)}`} onChange={(v) => void chooseSecond(v)} />
    </span>}
    {tier && model && <span id={`setting-${(second === tier ? model : tier).key}`} hidden />}
  </span>
}

// TagsControl edits a list of words: add with Enter, remove with ×. Each
// change saves at once. With choices, it adds from a menu of them.
export function TagsControl({ setting, disabled, onSet, choices }: { setting: ConfigSetting; disabled: boolean; onSet: (value: string[]) => void; choices?: string[] }) {
  const tags = (setting.value as string[] | null) ?? []
  const [draft, setDraft] = useState("")
  const add = (word: string) => {
    const w = word.trim()
    if (!w || tags.includes(w)) return
    onSet([...tags, w])
    setDraft("")
  }
  const missing = choices ? tags.filter((t) => !choices.includes(t)) : []
  return <span className="flex max-w-md flex-col items-end gap-1">
    <span className="flex flex-wrap items-center justify-end gap-1.5">
      {tags.map((t) => <span key={t} className={cn("inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-[13px]", missing.includes(t) ? "border-lw-error text-lw-error" : "border-lw-line bg-lw-raised text-lw-text")}>
        {t}
        {!disabled && <button type="button" aria-label={`Remove ${t}`} onClick={() => onSet(tags.filter((x) => x !== t))} className="text-lw-text-2 hover:text-lw-text"><X size={12} aria-hidden /></button>}
      </span>)}
      {!disabled && (choices
        ? <select aria-label={`Add to ${setting.title}`} value="" onChange={(e) => add(e.target.value)} className={cn(menu, "text-lw-text-2")}>
          <option value="">Add…</option>
          {choices.filter((c) => !tags.includes(c)).map((c) => <option key={c} value={c}>{c}</option>)}
        </select>
        : <input aria-label={`Add to ${setting.title}`} value={draft} placeholder="Add" onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); add(draft) } }}
          className="min-h-8 w-28 rounded-md border border-lw-line-strong bg-lw-ground px-2 text-[13px] text-lw-text placeholder:text-lw-faint" />)}
      {tags.length === 0 && disabled && <span className="text-sm text-lw-text-2">None</span>}
    </span>
    {missing.map((t) => <span key={t} role="alert" className="text-xs text-lw-error">{t} isn't a column here</span>)}
  </span>
}

// PickerControl draws a setting chosen from known values.
export function PickerControl({ setting, picker, disabled, setKey, resetKey }: {
  setting: ConfigSetting; picker: Picker; disabled: boolean
  setKey: SaveKey; resetKey: ResetKey
}) {
  const data = usePickerData()
  const value = String(setting.value ?? "")
  const choose = (v: string) => { void (v === "" ? resetKey(setting.key) : setKey(setting.key, v)) }
  switch (picker.kind) {
    case "agent":
      return <AgentPickerControl setting={setting} picker={picker} disabled={disabled} setKey={setKey} resetKey={resetKey} />
    case "column":
      return <Menu label={setting.title} value={value} disabled={disabled} greyed={setting.source === "default"}
        empty={value === "" ? "None" : undefined} options={data.columns.map((c) => ({ value: c, label: c }))}
        missing={(v) => `${v} isn't a column here`} onChange={choose} />
    case "columns":
      return <TagsControl setting={setting} disabled={disabled} choices={data.columns} onSet={(v) => void setKey(setting.key, v)} />
    case "workspace":
      return <Menu label={setting.title} value={value} disabled={disabled} greyed={setting.source === "default"}
        empty={picker.sameAsNewTasks ? "Same as new tasks" : "None"} options={data.workspaces.map((w) => ({ value: w, label: w }))}
        missing={(v) => `${v} isn't a workspace here`} onChange={choose} />
    case "skill":
      return <Menu label={setting.title} value={value} disabled={disabled} empty="None" options={data.skills.map((s) => ({ value: s, label: s }))}
        missing={(v) => `${v} isn't a skill here`} onChange={choose} />
  }
}
