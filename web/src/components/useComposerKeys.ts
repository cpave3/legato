import { useCallback } from "react"
import type { SlashItem, SlashMatch } from "../lib/slashMenu"

// What the keys of the message box move in the slash menu while it is open.
export interface SlashKeys {
  open: boolean
  matches: SlashMatch[]
  index: number
  setIndex: (index: number) => void
  select: (item: SlashItem) => void
  escape: () => void
}

// A key typed while an input method composes a character belongs to it.
function isComposing(e: React.KeyboardEvent): boolean {
  return e.nativeEvent.isComposing || e.nativeEvent.keyCode === 229
}

function isPlainEnter(e: React.KeyboardEvent): boolean {
  return e.key === "Enter" && !e.shiftKey
}

// The arrows move the highlight, wrapping at the ends.
function moveHighlight(e: React.KeyboardEvent, menu: SlashKeys): boolean {
  const count = menu.matches.length
  const vertical = e.key === "ArrowDown" || e.key === "ArrowUp"
  if (!vertical || count === 0) return false
  e.preventDefault()
  menu.setIndex((menu.index + (e.key === "ArrowDown" ? 1 : count - 1)) % count)
  return true
}

// Tab or Enter completes the highlighted item.
function pickHighlighted(e: React.KeyboardEvent, menu: SlashKeys): boolean {
  const picks = e.key === "Tab" || e.key === "Enter"
  if (!picks || e.shiftKey || menu.matches.length === 0) return false
  e.preventDefault()
  menu.select(menu.matches[menu.index].item)
  return true
}

// While the menu is open the box's keys are its own: Escape closes it, and
// Enter never sends.
function handleSlashKey(e: React.KeyboardEvent, menu: SlashKeys) {
  e.stopPropagation()
  if (moveHighlight(e, menu) || pickHighlighted(e, menu)) return
  if (e.key === "Escape") {
    e.preventDefault()
    menu.escape()
    return
  }
  if (isPlainEnter(e)) e.preventDefault()
}

// useComposerKeys is the message box's keys: the slash menu's while it is
// open, else Enter sends and Escape interrupts a working agent.
export function useComposerKeys(menu: SlashKeys, busy: boolean, onSend: () => void, onInterrupt: () => void) {
  const { open, matches, index, setIndex, select, escape } = menu
  return useCallback((e: React.KeyboardEvent) => {
    if (isComposing(e)) return
    if (open) {
      handleSlashKey(e, { open, matches, index, setIndex, select, escape })
      return
    }
    if (isPlainEnter(e)) {
      e.preventDefault()
      onSend()
      return
    }
    if (e.key === "Escape" && busy) {
      e.preventDefault()
      onInterrupt()
    }
  }, [open, matches, index, setIndex, select, escape, busy, onSend, onInterrupt])
}
