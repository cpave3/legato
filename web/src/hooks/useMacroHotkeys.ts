import { useEffect } from "react"

interface MacroHotkey {
  keys: string
}

interface UseMacroHotkeysOptions {
  macros: MacroHotkey[]
  open: boolean
  onOpen: () => void
  onClose: () => void
  onSelect: (keys: string) => void
}

function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  if (["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName)) return true
  return target.isContentEditable || target.closest('[contenteditable]:not([contenteditable="false"])') !== null
}

export function useMacroHotkeys({ macros, open, onOpen, onClose, onSelect }: UseMacroHotkeysOptions) {
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (
        event.defaultPrevented
        || event.repeat
        || event.ctrlKey
        || event.altKey
        || event.metaKey
        || event.shiftKey
        || isEditableTarget(event.target)
      ) return

      if (!open && event.key.toLowerCase() === "m") {
        event.preventDefault()
        onOpen()
        return
      }

      if (!open) return
      if (event.key === "Escape") {
        event.preventDefault()
        onClose()
        return
      }

      if (/^[1-9]$/.test(event.key)) {
        const macro = macros[Number(event.key) - 1]
        if (macro) {
          event.preventDefault()
          onSelect(macro.keys)
        }
      }
    }

    window.addEventListener("keydown", handleKeyDown)
    return () => window.removeEventListener("keydown", handleKeyDown)
  }, [macros, onClose, onOpen, onSelect, open])
}
