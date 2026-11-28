import { useState } from "react"
import { X } from "lucide-react"
import type { ConfigSetting } from "../../lib/configSettings"
import { cn } from "../../lib/utils"
import { usePickerData, type AgentPicker, type Picker } from "../../lib/settingPickers"
import { agentName, agentPartners, agentUnsetLabel, fallbackAgent, planAgent, planSecond, runPlan, secondChoice, secondOptions, type AgentPartners } from "../../lib/agentPicker"

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

// AgentPickerControl is an agent menu, with its model or tier beside it
// when the registry pairs one: one setting in two keys.
function AgentPickerControl({ setting, picker, disabled, setKey, resetKey }: {
  setting: ConfigSetting; picker: AgentPicker; disabled: boolean
  setKey: SaveKey; resetKey: ResetKey
}) {
  const data = usePickerData()
  const agent = String(setting.value ?? "")
  const fallback = fallbackAgent(picker, data)
  const effective = agent || fallback
  const partners = agentPartners(picker, data.settings)
  const { second } = partners
  // Each choice clears what would conflict first, and saves only once that
  // is done: the server checks the whole configuration, so a tier saved
  // beside a model, or an old agent's tier beside a new agent, is refused.
  const chooseAgent = (choice: string) => runPlan(planAgent(setting, partners, choice, choice || fallback, data), setKey, resetKey)
  return <span className="flex flex-wrap items-start justify-end gap-2">
    <Menu label={second ? `${setting.title} agent` : setting.title} value={agent} disabled={disabled} greyed={setting.source === "default"}
      empty={agentUnsetLabel(picker, setting.default_label, fallback, data.defaultAgent)}
      options={data.agents.map((a) => ({ value: a, label: agentName(a) }))}
      missing={(v) => `${v} isn't an agent here`} onChange={(v) => void chooseAgent(v)} />
    {second && <SecondMenu setting={setting} partners={partners} second={second} agent={effective} disabled={disabled}
      onChoose={(choice) => void runPlan(planSecond(partners, choice), setKey, resetKey)} />}
    <OtherPartner partners={partners} />
  </span>
}

// OtherPartner anchors the key of two the model menu does not stand for, so
// a link to that setting still finds its row.
function OtherPartner({ partners: { second, tier, model } }: { partners: AgentPartners }) {
  if (!tier || !model) return null
  return <span id={`setting-${(second === tier ? model : tier).key}`} hidden />
}

// SecondMenu is the agent's model or tier, a pinned partner held as it is.
function SecondMenu({ setting, partners, second, agent, disabled, onChoose }: {
  setting: ConfigSetting; partners: AgentPartners; second: ConfigSetting; agent: string; disabled: boolean; onChoose: (choice: string) => void
}) {
  const data = usePickerData()
  const tiers = data.tiers[agent] ?? [], models = data.models[agent] ?? []
  const { current, value } = secondChoice(partners, tiers)
  return <span id={`setting-${second.key}`} className="contents">
    <Menu label={`${setting.title} model or tier`} value={value} disabled={disabled || Boolean(second.pinned)} greyed={!current}
      empty={second.default_label ?? "Default model"} options={secondOptions(partners, tiers, models)}
      missing={(v) => `${v.slice(v.indexOf(":") + 1)} isn't a model or tier of ${agentName(agent)}`} onChange={onChoose} />
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
      {tags.map((t) => <Tag key={t} word={t} missing={missing.includes(t)} onRemove={disabled ? undefined : () => onSet(tags.filter((x) => x !== t))} />)}
      {!disabled && <TagAdder label={`Add to ${setting.title}`} choices={choices?.filter((c) => !tags.includes(c))} draft={draft} onDraft={setDraft} onAdd={add} />}
      {tags.length === 0 && disabled && <span className="text-sm text-lw-text-2">None</span>}
    </span>
    {missing.map((t) => <span key={t} role="alert" className="text-xs text-lw-error">{t} isn't a column here</span>)}
  </span>
}

function Tag({ word, missing, onRemove }: { word: string; missing: boolean; onRemove?: () => void }) {
  return <span className={cn("inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-[13px]", missing ? "border-lw-error text-lw-error" : "border-lw-line bg-lw-raised text-lw-text")}>
    {word}
    {onRemove && <button type="button" aria-label={`Remove ${word}`} onClick={onRemove} className="text-lw-text-2 hover:text-lw-text"><X size={12} aria-hidden /></button>}
  </span>
}

// TagAdder adds a word: from a menu of the choices left, or typed and
// entered.
function TagAdder({ label, choices, draft, onDraft, onAdd }: { label: string; choices?: string[]; draft: string; onDraft: (draft: string) => void; onAdd: (word: string) => void }) {
  if (choices) {
    return <select aria-label={label} value="" onChange={(e) => onAdd(e.target.value)} className={cn(menu, "text-lw-text-2")}>
      <option value="">Add…</option>
      {choices.map((c) => <option key={c} value={c}>{c}</option>)}
    </select>
  }
  return <input aria-label={label} value={draft} placeholder="Add" onChange={(e) => onDraft(e.target.value)}
    onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); onAdd(draft) } }}
    className="min-h-8 w-28 rounded-md border border-lw-line-strong bg-lw-ground px-2 text-[13px] text-lw-text placeholder:text-lw-faint" />
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
