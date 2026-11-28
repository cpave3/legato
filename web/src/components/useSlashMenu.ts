import { useCallback, useId, useLayoutEffect, useMemo, useRef, useState, type RefObject } from "react"
import { rankSlashItems, type SlashItem } from "../lib/slashMenu"
import { activeSlashIndex, filterSlashItems, slashCompletion, slashListKey, slashQueryOf, type BrowserFilter } from "../lib/slashCompletion"

function moveCaretToEnd(box: HTMLTextAreaElement | null) {
  if (!box) return
  box.focus()
  box.setSelectionRange(box.value.length, box.value.length)
}

// useSlashMenu is the message box's menu of commands and skills: open while
// the draft is a slash and a name, until it is closed on this draft, or as a
// browser opened from its button.
export function useSlashMenu(draft: string, setDraft: (draft: string) => void, textareaRef: RefObject<HTMLTextAreaElement | null>, items: SlashItem[], catalogKey: string) {
  const [browserOpen, setBrowserOpen] = useState(false)
  const [browserQuery, setBrowserQuery] = useState("")
  const [browserFilter, setBrowserFilter] = useState<BrowserFilter>("all")
  // The draft the slash menu was closed on (Escape, or an item completed);
  // typing again reopens it.
  const [slashClosedOn, setSlashClosedOn] = useState<string | null>(null)
  // The highlighted row, kept only while the list it was chosen in stands.
  const [slashActive, setSlashActive] = useState({ list: "", index: 0 })
  const caretToEnd = useRef(false)
  const listId = useId()

  useLayoutEffect(() => {
    setBrowserOpen(false)
    setBrowserQuery("")
    setBrowserFilter("all")
    setSlashClosedOn(null)
  }, [catalogKey])

  const query = slashQueryOf(draft, browserOpen, browserQuery)
  const matches = useMemo(
    () => query === undefined ? [] : rankSlashItems(filterSlashItems(items, browserOpen, browserFilter), query),
    [items, query, browserOpen, browserFilter],
  )
  const open = browserOpen || query !== undefined && slashClosedOn !== draft
  const list = useMemo(() => slashListKey(matches), [matches])
  const index = activeSlashIndex(slashActive, list, matches.length)
  const setIndex = useCallback((index: number) => setSlashActive({ list, index }), [list])
  const optionId = useCallback((index: number) => `${listId}-option-${index}`, [listId])

  // A completed item leaves the cursor at the end, so the person keeps typing.
  useLayoutEffect(() => {
    if (!caretToEnd.current) return
    caretToEnd.current = false
    moveCaretToEnd(textareaRef.current)
  }, [draft, textareaRef])

  const select = useCallback((item: SlashItem) => {
    const next = slashCompletion(item, draft, browserOpen)
    setBrowserOpen(false)
    caretToEnd.current = true
    setSlashClosedOn(next)
    setDraft(next)
  }, [browserOpen, draft, setDraft])

  // Closes the menu on this draft; it stays closed until the draft changes.
  const dismiss = useCallback(() => {
    setBrowserOpen(false)
    setSlashClosedOn(draft)
  }, [draft])

  // Closes it and goes back to the message box.
  const escape = useCallback(() => {
    dismiss()
    textareaRef.current?.focus()
  }, [dismiss, textareaRef])

  const toggleBrowser = useCallback(() => {
    setBrowserOpen((open) => !open)
    setBrowserQuery("")
    setBrowserFilter("all")
    setSlashClosedOn(draft)
  }, [draft])

  return {
    open, matches, index, setIndex, listId, optionId, select, dismiss, escape, toggleBrowser,
    browser: { open: browserOpen, query: browserQuery, setQuery: setBrowserQuery, filter: browserFilter, setFilter: setBrowserFilter },
  }
}
