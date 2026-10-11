import { useCallback } from "react"
import { proposeComposerText } from "../lib/agentUi"
import { useToast } from "./useToast"

// useProposeMessage returns what a card or an artifact calls to put text in
// an agent's message box. It only fills the box and says so: the person reads
// the text and sends it, or does not.
export function useProposeMessage(agentId: string) {
  const toast = useToast()
  return useCallback((text: string) => {
    proposeComposerText(agentId, text)
    toast?.addToast("The agent's content put text in your message box. Check it before you send it.", "info")
  }, [agentId, toast])
}
