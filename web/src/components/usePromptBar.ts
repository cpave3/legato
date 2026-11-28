import { useCallback, useEffect, useRef, useState, type KeyboardEvent, type RefObject } from "react"

import { useMacroHotkeys } from "../hooks/useMacroHotkeys"
import { useMacroPicker } from "../hooks/useMacroPicker"
import { appendProposal, onComposerProposal } from "../lib/agentUi"
import { apiFetch } from "../lib/api"
import { macroMessage } from "../lib/macros"
import { terminalKey } from "../lib/promptBar"

// The terminal prompt bar's state: each agent's draft, and the macros menu.
// See PromptBar.tsx.

function draftKey(id: string) { return `legato:draft:${id}` }

function keepDraft(id: string, value: string) {
  if (value) localStorage.setItem(draftKey(id), value)
  else localStorage.removeItem(draftKey(id))
}

// The message box's text, kept as each agent's draft on this device: saved
// as it is typed, put back when its agent is shown again, and joined by what
// the agent's side panel proposes (never sent from here). inputRef follows
// the text for callbacks that outlive a render.
export function usePromptDraft(agentId: string, textareaRef: RefObject<HTMLTextAreaElement | null>) {
  const [input, setInput] = useState(() => localStorage.getItem(draftKey(agentId)) ?? "")
  const inputRef = useRef(input)
  const prevAgentIdRef = useRef(agentId)

  // Sync textarea height whenever input changes (submit clear, agent switch, typing).
  useEffect(() => {
    const el = textareaRef.current
    if (el) {
      el.style.height = "auto"
      el.style.height = Math.min(el.scrollHeight, 5 * 24 + 12) + "px"
    }
  }, [input, textareaRef])

  // Keep inputRef in sync for use in the agentId change effect.
  useEffect(() => {
    inputRef.current = input
  }, [input])

  // On agent switch: save old draft, load new draft.
  useEffect(() => {
    if (prevAgentIdRef.current === agentId) return
    keepDraft(prevAgentIdRef.current, inputRef.current)
    prevAgentIdRef.current = agentId
    setInput(localStorage.getItem(draftKey(agentId)) ?? "")
  }, [agentId])

  const change = (value: string) => {
    setInput(value)
    keepDraft(agentId, value)
  }

  useEffect(() => onComposerProposal(agentId, (text) => {
    const next = appendProposal(inputRef.current, text)
    setInput(next)
    localStorage.setItem(draftKey(agentId), next)
    textareaRef.current?.focus()
  }), [agentId, textareaRef])

  const clear = () => {
    setInput("")
    localStorage.removeItem(draftKey(agentId))
  }
  return { input, inputRef, change, clear }
}

// Sends the message (trimmed, when there is one) and clears the box, unless
// it did not go out. While the box is empty, the navigation keys go to the
// terminal (terminalKey); otherwise Enter sends and Shift+Enter breaks the
// line.
export function useMessageKeys({ input, onSubmitMessage, clear, onSendKeys }: {
  input: string
  onSubmitMessage: (content: string) => boolean | void
  clear: () => void
  onSendKeys: (keys: string) => void
}) {
  const submit = () => {
    const trimmed = input.trim()
    if (!trimmed || onSubmitMessage(trimmed) === false) return
    clear()
  }
  const onKeyDown = (e: KeyboardEvent) => {
    const tmuxKey = terminalKey(e.key, input)
    if (tmuxKey) {
      e.preventDefault()
      onSendKeys(tmuxKey)
      return
    }
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault()
      submit()
    }
  }
  return { submit, onKeyDown }
}

export interface PromptBarHandle {
  focus: () => void
  voiceStart: () => void
  voiceSend: () => void
  voiceCancel: () => void
  voiceIsRecording: () => boolean
}

type MacroItem ={ name: string; keys: string; favorite?: boolean }

// The macros menu: the macros, read eagerly so numbered shortcuts are ready
// when it opens; M and the numbers (useMacroHotkeys); a click outside
// closes it. A macro goes to the agent as a message.
export function useMacroMenu(baseUrl: string, onSubmitMessage: (content: string) => boolean | void) {
  const [open, setOpen] = useState(false)
  const [macros, setMacros] = useState<MacroItem[]>([])
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    apiFetch(baseUrl, "/api/macros")
      .then((r) => r.ok ? r.json() : { macros: [] })
      .then((data: { macros?: MacroItem[] }) => {
        setMacros(Array.isArray(data.macros) ? data.macros : [])
      })
      .catch(() => setMacros([]))
  }, [baseUrl])

  useEffect(() => {
    if (!open) return
    const onClick = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener("click", onClick, true)
    return () => document.removeEventListener("click", onClick, true)
  }, [open])

  const pick = useCallback((keys: string) => {
    onSubmitMessage(macroMessage(keys))
    setOpen(false)
  }, [onSubmitMessage])
  const openMenu = useCallback(() => setOpen(true), [])
  const closeMenu = useCallback(() => setOpen(false), [])
  const picker = useMacroPicker(macros, open)
  useMacroHotkeys({ macros: picker.shown, open, onOpen: openMenu, onClose: closeMenu, onSelect: pick })
  return { open, toggle: () => setOpen((v) => !v), ref, pick, picker }
}
