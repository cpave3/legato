import { useEffect, useState } from "react"
import { pickerMacros, useMacros } from "../../lib/macros"
import { macroStart } from "../../lib/spawnAgent"

interface MacroChoiceOptions {
  open: boolean
  // Changes whenever the dialog opens afresh; empty while it is closed.
  openKey: string
  baseUrl: string
  // Where a macro's message, tool and account go.
  setPrompt: (prompt: string) => void
  setAgentKind: (kind: string) => void
  setAccount: (account: string) => void
}

// useMacroChoice is "Start with macro": the macros, the one chosen, and
// the tier it names. Choosing one fills the first message, and the tool
// and account it names.
export function useMacroChoice({ open, openKey, baseUrl, setPrompt, setAgentKind, setAccount }: MacroChoiceOptions) {
  const macroList = useMacros(baseUrl, open ? "open" : null)
  const [macroName, setMacroName] = useState("")
  const [tier, setTier] = useState("")

  useEffect(() => {
    if (!openKey) return
    setMacroName("")
    setTier("")
  }, [openKey])

  const choose = (name: string) => {
    setMacroName(name)
    const start = macroStart(macroList.macros.find((item) => item.name === name))
    setTier(start.tier)
    setAccount(start.account)
    if (start.prompt !== undefined) setPrompt(start.prompt)
    if (start.agentKind !== undefined) setAgentKind(start.agentKind)
  }

  return {
    macroName,
    tier,
    choose,
    // The task's macros by the pickers' rule (LEG-619). A select cannot
    // hide the rest behind Show all: it groups the favourites first.
    macros: pickerMacros(macroList.macros, { targets: ["task"] }),
  }
}

export type MacroChoice = ReturnType<typeof useMacroChoice>
