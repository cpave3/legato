import { useId, type ReactNode } from "react"
import { agentKindLabel } from "../lib/agentKind"
import { macroAccountsShown, macroAgentChoices, unlistedAccount } from "../lib/macroForm"
import type { AgentTier, Macro } from "../lib/macros"
import { Dialog, DialogBody, DialogClose, DialogFooter, DialogHeader, DialogPanel, DialogTitle } from "./ui/Dialog"
import { primaryButton, quietButton } from "./settings/SettingsUI"
import type { Adapters } from "../lib/adapters"
import { useMacroForm, type MacroFormState } from "./useMacroForm"

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

// The name, which a built-in keeps; renaming one of the person's own warns
// of the workflows that use the old name.
function NameField({ form, macro, id }: { form: MacroFormState; macro: Macro | null; id: string }) {
  const builtin = Boolean(macro?.builtin)
  return (
    <div className="flex flex-col gap-1.5">
      <label htmlFor={`${id}-name`} className="flex items-baseline gap-1.5 text-[13px] font-medium">Name <Marker required /></label>
      <input id={`${id}-name`} aria-describedby={`${id}-name-help`} className={input} value={form.name} required disabled={builtin} onChange={(e) => form.setName(e.target.value)} />
      <span id={`${id}-name-help`} className="contents">
        {builtin && <span className={help}>Built-in names can’t change.</span>}
        {form.renamed && !form.taken && <span className={`${help} text-lw-needs`}>Workflows that use the old name, {macro?.name}, will fail.</span>}
        {form.taken && <span role="alert" className={`${help} text-lw-error`}>Another macro has that name.</span>}
      </span>
    </div>
  )
}

// Claude Code's account, keeping one the settings no longer list.
function AccountField({ form, adapters, id }: { form: MacroFormState; adapters: Adapters; id: string }) {
  const { account } = form
  return (
    <div className="flex flex-col gap-1.5">
      <label htmlFor={`${id}-account`} className="text-[13px] font-medium">Account</label>
      <select id={`${id}-account`} className={input} value={account} onChange={(e) => form.setAccount(e.target.value)}>
        <option value="">{form.agent ? "Default account" : "Follow usage profile"}</option>
        {(adapters.accounts ?? []).map((a) => <option key={a.name} value={a.name}>{a.label || a.name}</option>)}
        {unlistedAccount(adapters, account) && <option value={account}>{account} (not configured)</option>}
      </select>
    </div>
  )
}

function tierLabel(tier: AgentTier): string {
  return tier.description ? `${tier.name} · ${tier.description}` : tier.name
}

// The agent's tier, warning of one it does not have.
function TierField({ form, id }: { form: MacroFormState; id: string }) {
  const { tier, unknownTier } = form
  const agentLabel = agentKindLabel(form.effectiveAgent)
  return (
    <div className="flex flex-col gap-1.5">
      <label htmlFor={`${id}-tier`} className="flex items-baseline gap-1.5 text-[13px] font-medium">Model tier <Marker /></label>
      <select id={`${id}-tier`} aria-describedby={`${id}-tier-help`} className={input} value={tier} onChange={(e) => form.setTier(e.target.value)}>
        <option value="">None</option>
        {form.agentTiers.map((t) => <option key={t.name} value={t.name}>{tierLabel(t)}</option>)}
        {unknownTier && <option value={tier}>{tier}</option>}
      </select>
      {unknownTier
        ? <span id={`${id}-tier-help`} role="alert" className={`${help} text-lw-error`}>{agentLabel} has no tier “{tier}”. It starts without one.</span>
        : <span id={`${id}-tier-help`} className={help}>{agentLabel}’s tiers from config.yaml. Used when this macro starts the agent.</span>}
    </div>
  )
}

// MacroForm makes a macro (macro null) or edits one: a dialog, the whole
// screen on a phone.
export function MacroForm({ baseUrl, macro, macros, adapters, tiers, onClose, onSaved }: {
  baseUrl: string
  macro: Macro | null
  macros: Macro[]
  adapters: Adapters
  tiers: Record<string, AgentTier[]>
  onClose: () => void
  onSaved: () => void | Promise<void>
}) {
  const form = useMacroForm({ baseUrl, macro, macros, adapters, tiers, onSaved })
  const id = useId()

  return (
    <Dialog open onClose={onClose} className="p-0 sm:p-4">
      <DialogPanel className="h-[100dvh] max-h-none max-w-none rounded-none border-lw-line bg-lw-surface font-lw text-lw-text sm:h-auto sm:max-h-[calc(100dvh-2rem)] sm:max-w-xl sm:rounded-xl">
        <DialogHeader className="border-lw-line">
          <DialogTitle className="text-lg text-lw-text">{macro ? `Edit ${macro.name}` : "New macro"}</DialogTitle>
          <DialogClose />
        </DialogHeader>
        <DialogBody className="space-y-5">
          {macro?.builtin && <p className="rounded-lg bg-lw-raised px-3 py-2.5 text-xs text-lw-text-2">Built-in macro. <b className="font-medium text-lw-text">Reset</b> restores the original.</p>}
          <NameField form={form} macro={macro} id={id} />
          <div className="flex flex-col gap-1.5">
            <label htmlFor={`${id}-message`} className="flex items-baseline gap-1.5 text-[13px] font-medium">Message <Marker required /></label>
            <textarea id={`${id}-message`} aria-describedby={`${id}-message-help`} className={`${input} min-h-[132px] resize-y font-lw-mono text-[13px] leading-5`} value={form.message} required onChange={(e) => form.setMessage(e.target.value)} />
            <span id={`${id}-message-help`} className={help}>Sent as written, below the task’s key and title.</span>
          </div>
          <div className="flex flex-col gap-1.5">
            <Choices label={<>Offer it on <Marker /></>} name="applies_to" value={form.applies} onChange={form.setApplies}
              options={[["", "Tasks and pull requests"], ["task", "Tasks"], ["pr", "Pull requests"]]} />
            <span className={help}>Tasks: card menus and the spawn dialog. Pull requests: a card’s pull request lines.</span>
          </div>
          <div className="flex flex-col gap-1.5">
            <Choices label={<>Agent to start <Marker /></>} name="agent" value={form.agent} onChange={form.chooseAgent} options={macroAgentChoices(adapters, form.agent)} />
            <span className={help}>Used when the task has no agent running.</span>
          </div>
          {macroAccountsShown(form.effectiveAgent, adapters, form.account) && <AccountField form={form} adapters={adapters} id={id} />}
          {(form.agentTiers.length > 0 || form.unknownTier) && <TierField form={form} id={id} />}
          {form.error && <p role="alert" className="text-sm text-lw-error">{form.error}</p>}
        </DialogBody>
        <DialogFooter className="border-lw-line">
          <button type="button" className={quietButton} onClick={onClose}>Cancel</button>
          <button type="button" className={primaryButton} disabled={!form.savable} onClick={() => void form.save()}>{form.saving ? "Saving…" : "Save"}</button>
        </DialogFooter>
      </DialogPanel>
    </Dialog>
  )
}
