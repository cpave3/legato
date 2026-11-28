import { useState } from "react"
import type { Adapters } from "../lib/adapters"
import { canSaveMacro, macroEffectiveAgent, macroFields, macroFormStart, macroNameStatus, macroTiers, persistMacro, tierAfterAgent } from "../lib/macroForm"
import type { AgentTier, Macro } from "../lib/macros"

interface MacroFormOptions {
  baseUrl: string
  // The macro edited, or null for a new one.
  macro: Macro | null
  macros: Macro[]
  adapters: Adapters
  tiers: Record<string, AgentTier[]>
  onSaved: () => void | Promise<void>
}

// useMacroForm is the macro form's state: its fields, what they mean
// (macroForm's rules), and saving.
export function useMacroForm({ baseUrl, macro, macros, adapters, tiers, onSaved }: MacroFormOptions) {
  const [start] = useState(() => macroFormStart(macro))
  const [name, setName] = useState(start.name)
  const [message, setMessage] = useState(start.message)
  const [applies, setApplies] = useState<string>(start.applies)
  const [agent, setAgent] = useState(start.agent)
  const [tier, setTier] = useState(start.tier)
  const [account, setAccount] = useState(start.account)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState("")

  const effectiveAgent = macroEffectiveAgent(agent, account, adapters)
  const { renamed, taken } = macroNameStatus(name, macro, macros)
  const savable = !saving && canSaveMacro(name, message, taken)

  // Another agent forgets the account, and the tier it does not have.
  const chooseAgent = (next: string) => {
    setAgent(next)
    setAccount("")
    setTier(tierAfterAgent(tiers, next, adapters.default, tier))
  }

  const save = async () => {
    if (!savable) return
    setSaving(true)
    setError("")
    try {
      await persistMacro(baseUrl, macro, name, macroFields(message, applies, agent, tier, account))
      await onSaved()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
      setSaving(false)
    }
  }

  return {
    name, setName, message, setMessage, applies, setApplies, agent, chooseAgent, tier, setTier, account, setAccount,
    saving, error, save, savable, renamed, taken, effectiveAgent,
    ...macroTiers(tiers, effectiveAgent, tier),
  }
}

export type MacroFormState = ReturnType<typeof useMacroForm>
