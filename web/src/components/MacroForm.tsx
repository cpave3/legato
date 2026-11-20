import { useId, useState, type ReactNode } from "react"
import { agentKindLabel } from "../lib/agentKind"
import { macroMessage, renameMacro, saveMacro, type AgentTier, type Macro, type MacroFields } from "../lib/macros"
import { Dialog, DialogBody, DialogClose, DialogFooter, DialogHeader, DialogPanel, DialogTitle } from "./ui/Dialog"
import { primaryButton, quietButton } from "./settings/SettingsUI"

const input = "min-h-11 w-full rounded-lg border border-lw-line-strong bg-lw-ground px-3 py-2 text-base text-lw-text focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-lw-accent-ring disabled:opacity-60 sm:min-h-10 sm:text-sm"
const pill = "inline-flex min-h-11 cursor-pointer items-center rounded-lg border px-3 text-sm has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-lw-accent-ring sm:min-h-9"
const help = "text-xs leading-[18px] text-lw-text-2"

function Marker({ required }: { required?: boolean }) {
  return required
    ? <span className="text-[11px] font-medium text-lw-needs">required</span>
    : <span className="text-[11px] text-lw-text-2">optional</span>
}

function Choices({ label, name, value, options, onChange }: { label: ReactNode; name: string; value: string; options: [string, string][]; onChange: (value: string) => void }) {
  return (
    <fieldset className="flex flex-col gap-1.5">
      <legend className="mb-1.5 flex items-baseline gap-1.5 text-[13px] font-medium">{label}</legend>
      <div className="flex flex-wrap gap-1.5">
        {options.map(([id, text]) => (
          <label key={id} className={`${pill} ${value === id ? "border-lw-accent bg-lw-accent-soft text-lw-text" : "border-lw-line bg-lw-ground text-lw-text-2"}`}>
            <input type="radio" name={name} value={id} checked={value === id} onChange={() => onChange(id)} className="sr-only" />
            {text}
          </label>
        ))}
      </div>
    </fieldset>
  )
}

const same = (a: string, b: string) => a.trim().toLowerCase() === b.trim().toLowerCase()

// MacroForm makes a macro (macro null) or edits one: a dialog, the whole
// screen on a phone.
export function MacroForm({ baseUrl, macro, macros, adapters, tiers, onClose, onSaved }: {
  baseUrl: string
  macro: Macro | null
  macros: Macro[]
  adapters: { adapters: string[]; default: string }
  tiers: Record<string, AgentTier[]>
  onClose: () => void
  onSaved: () => void | Promise<void>
}) {
  const [name, setName] = useState(macro?.name ?? "")
  const [message, setMessage] = useState(macro ? macroMessage(macro.keys) : "")
  const [applies, setApplies] = useState<string>(macro?.applies_to ?? "")
  const [agent, setAgent] = useState(macro?.agent ?? "")
  const [tier, setTier] = useState(macro?.tier ?? "")
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState("")
  const id = useId()

  const builtin = Boolean(macro?.builtin)
  const effectiveAgent = agent || adapters.default
  const agentTiers = tiers[effectiveAgent] ?? []
  const unknownTier = tier !== "" && !agentTiers.some((t) => t.name === tier)
  const renamed = macro !== null && !builtin && name.trim() !== "" && !same(name, macro.name)
  const taken = name.trim() !== "" && macros.some((m) => same(m.name, name) && (macro === null || !same(m.name, macro.name)))
  const agents: [string, string][] = [
    ["", adapters.default ? `Your default (${agentKindLabel(adapters.default)})` : "Your default"],
    ...adapters.adapters.map((kind): [string, string] => [kind, agentKindLabel(kind)]),
  ]
  if (agent && !adapters.adapters.includes(agent)) agents.push([agent, agentKindLabel(agent)])

  const chooseAgent = (next: string) => {
    setAgent(next)
    if (!(tiers[next || adapters.default] ?? []).some((t) => t.name === tier)) setTier("")
  }
  const save = async () => {
    if (saving || !name.trim() || !message.trim() || taken) return
    setSaving(true)
    setError("")
    const fields: MacroFields = { message, applies_to: applies as MacroFields["applies_to"], agent, tier }
    try {
      if (macro === null) {
        await saveMacro(baseUrl, name.trim(), fields)
      } else {
        await saveMacro(baseUrl, macro.name, fields)
        if (!builtin && name.trim() !== macro.name) await renameMacro(baseUrl, macro.name, name.trim())
      }
      await onSaved()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
      setSaving(false)
    }
  }

  return (
    <Dialog open onClose={onClose} className="p-0 sm:p-4">
      <DialogPanel className="h-[100dvh] max-h-none max-w-none rounded-none border-lw-line bg-lw-surface font-lw text-lw-text sm:h-auto sm:max-h-[calc(100dvh-2rem)] sm:max-w-xl sm:rounded-xl">
        <DialogHeader className="border-lw-line">
          <DialogTitle className="text-lg text-lw-text">{macro ? `Edit ${macro.name}` : "New macro"}</DialogTitle>
          <DialogClose />
        </DialogHeader>
        <DialogBody className="space-y-5">
          {builtin && <p className="rounded-lg bg-lw-raised px-3 py-2.5 text-xs text-lw-text-2">Built-in macro. <b className="font-medium text-lw-text">Reset</b> restores the original.</p>}
          <div className="flex flex-col gap-1.5">
            <label htmlFor={`${id}-name`} className="flex items-baseline gap-1.5 text-[13px] font-medium">Name <Marker required /></label>
            <input id={`${id}-name`} aria-describedby={`${id}-name-help`} className={input} value={name} required disabled={builtin} onChange={(e) => setName(e.target.value)} />
            <span id={`${id}-name-help`} className="contents">
              {builtin && <span className={help}>Built-in names can’t change.</span>}
              {renamed && !taken && <span className={`${help} text-lw-needs`}>Workflows that use the old name, {macro?.name}, will fail.</span>}
              {taken && <span role="alert" className={`${help} text-lw-error`}>Another macro has that name.</span>}
            </span>
          </div>
          <div className="flex flex-col gap-1.5">
            <label htmlFor={`${id}-message`} className="flex items-baseline gap-1.5 text-[13px] font-medium">Message <Marker required /></label>
            <textarea id={`${id}-message`} aria-describedby={`${id}-message-help`} className={`${input} min-h-[132px] resize-y font-lw-mono text-[13px] leading-5`} value={message} required onChange={(e) => setMessage(e.target.value)} />
            <span id={`${id}-message-help`} className={help}>Sent as written, below the task’s key and title.</span>
          </div>
          <div className="flex flex-col gap-1.5">
            <Choices label={<>Offer it on <Marker /></>} name="applies_to" value={applies} onChange={setApplies}
              options={[["", "Tasks and pull requests"], ["task", "Tasks"], ["pr", "Pull requests"]]} />
            <span className={help}>Tasks: card menus and the spawn dialog. Pull requests: a card’s pull request lines.</span>
          </div>
          <div className="flex flex-col gap-1.5">
            <Choices label={<>Agent to start <Marker /></>} name="agent" value={agent} onChange={chooseAgent} options={agents} />
            <span className={help}>Used when the task has no agent running.</span>
          </div>
          {(agentTiers.length > 0 || unknownTier) && <div className="flex flex-col gap-1.5">
            <label htmlFor={`${id}-tier`} className="flex items-baseline gap-1.5 text-[13px] font-medium">Model tier <Marker /></label>
            <select id={`${id}-tier`} aria-describedby={`${id}-tier-help`} className={input} value={tier} onChange={(e) => setTier(e.target.value)}>
              <option value="">None</option>
              {agentTiers.map((t) => <option key={t.name} value={t.name}>{t.description ? `${t.name} · ${t.description}` : t.name}</option>)}
              {unknownTier && <option value={tier}>{tier}</option>}
            </select>
            {unknownTier
              ? <span id={`${id}-tier-help`} role="alert" className={`${help} text-lw-error`}>{agentKindLabel(effectiveAgent)} has no tier “{tier}”. It starts without one.</span>
              : <span id={`${id}-tier-help`} className={help}>{agentKindLabel(effectiveAgent)}’s tiers from config.yaml. Used when this macro starts the agent.</span>}
          </div>}
          {error && <p role="alert" className="text-sm text-lw-error">{error}</p>}
        </DialogBody>
        <DialogFooter className="border-lw-line">
          <button type="button" className={quietButton} onClick={onClose}>Cancel</button>
          <button type="button" className={primaryButton} disabled={saving || !name.trim() || !message.trim() || taken} onClick={() => void save()}>{saving ? "Saving…" : "Save"}</button>
        </DialogFooter>
      </DialogPanel>
    </Dialog>
  )
}
