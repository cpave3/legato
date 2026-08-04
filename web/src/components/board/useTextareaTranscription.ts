import { useCallback, useLayoutEffect, useRef, type Dispatch, type SetStateAction } from "react"

interface TextareaSelection {
  start: number
  end: number
}

export function useTextareaTranscription(setValue: Dispatch<SetStateAction<string>>) {
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const savedSelectionRef = useRef<TextareaSelection | null>(null)
  const pendingCaretRef = useRef<number | null>(null)

  const rememberSelection = useCallback(() => {
    const textarea = textareaRef.current
    if (!textarea) return
    savedSelectionRef.current = {
      start: textarea.selectionStart,
      end: textarea.selectionEnd,
    }
  }, [])

  const insertTranscription = useCallback((transcription: string) => {
    setValue((current) => {
      const textarea = textareaRef.current
      const selection = textarea && textarea === document.activeElement
        ? { start: textarea.selectionStart, end: textarea.selectionEnd }
        : savedSelectionRef.current
      const validSelection = selection !== null
        && Number.isInteger(selection.start)
        && Number.isInteger(selection.end)
        && selection.start >= 0
        && selection.start <= selection.end
        && selection.end <= current.length

      if (validSelection) {
        pendingCaretRef.current = selection.start + transcription.length
        return current.slice(0, selection.start) + transcription + current.slice(selection.end)
      }

      const separator = current ? "\n" : ""
      pendingCaretRef.current = current.length + separator.length + transcription.length
      return current + separator + transcription
    })
  }, [setValue])

  useLayoutEffect(() => {
    const caret = pendingCaretRef.current
    const textarea = textareaRef.current
    if (caret === null || !textarea) return
    textarea.focus()
    textarea.setSelectionRange(caret, caret)
    savedSelectionRef.current = { start: caret, end: caret }
    pendingCaretRef.current = null
  })

  return { textareaRef, rememberSelection, insertTranscription }
}
