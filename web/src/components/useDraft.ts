import { useCallback, useEffect, useRef, useState } from "react"
import { appendProposal, onComposerProposal } from "../lib/agentUi"
import { insertImagePath } from "../lib/agentImage"

// useDraft is the message an operator is writing to an agent: kept in the
// browser per agent, added to by what is dictated, an image's path, or text
// a card or an artifact proposes.
export function useDraft(agentId: string, composerEnabled: boolean) {
  const [draft, setDraft] = useState("")
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const draftKey = `legato:chimera-draft:${agentId}`

  // A card or an artifact the agent showed may put text in the message box.
  // It is added to what is there and never sent from here.
  useEffect(() => {
    if (!composerEnabled) return
    return onComposerProposal(agentId, (text) => {
      setDraft((current) => appendProposal(current, text))
      textareaRef.current?.focus()
    })
  }, [agentId, composerEnabled])

  // Load/save draft
  useEffect(() => {
    const saved = localStorage.getItem(draftKey)
    if (saved) setDraft(saved)
  }, [draftKey])
  useEffect(() => {
    localStorage.setItem(draftKey, draft)
  }, [draftKey, draft])

  // What was dictated joins the message after a space.
  const appendWords = useCallback((text: string) => {
    setDraft((current) => current ? current + " " + text : text)
  }, [])

  // An image pasted into the message box is stored by Legato, and its path
  // goes into the message where the cursor is.
  const insertImage = useCallback((path: string) => {
    setDraft((current) => insertImagePath(current, path, textareaRef.current?.selectionStart))
  }, [])

  const focusBox = useCallback(() => {
    textareaRef.current?.focus()
  }, [])

  return { draft, setDraft, textareaRef, appendWords, insertImage, focusBox }
}
