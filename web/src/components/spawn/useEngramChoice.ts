import { useEffect, useState } from "react"
import { fetchEngramPicker, type EngramPicker, type EngramPlace } from "../../lib/engrams"
import { canRememberEngram, engramGroups, engramOffered, engramPlaceWords, engramPreselect } from "../../lib/spawnEngram"
import { suggestDelayMs } from "../../lib/spawnSuggestions"

type Answer = (picker: EngramPicker) => void

function askEngrams(baseUrl: string, place: EngramPlace, signal: AbortSignal, onAnswer: Answer) {
  fetchEngramPicker(baseUrl, place, signal)
    .then((picker) => { if (!signal.aborted) onAnswer(picker) })
    .catch(() => {})
}

interface EngramChoiceOptions {
  open: boolean
  // Changes whenever the dialog opens afresh; empty while it is closed.
  openKey: string
  baseUrl: string
  taskId: string
  // Where the agent starts: a folder, or the repository of a clone.
  dir: string
  repo: string
  // The agent tool that runs, which must take a launch prompt.
  adapter: string
}

// useEngramChoice is the engram the agent starts as. The engrams are asked
// for again whenever "Where" changes: the place decides which are bound
// there and which is picked first.
export function useEngramChoice({ open, openKey, baseUrl, taskId, dir, repo, adapter }: EngramChoiceOptions) {
  const [picker, setPicker] = useState<EngramPicker | null>(null)
  // The one picked ("" for none).
  const [engram, setEngram] = useState("")
  // Ticked: the pick is bound to the place and made its default, or with
  // no engram the place's default is cleared. Unticked by every new pick.
  const [remember, setRemember] = useState(false)

  useEffect(() => {
    if (!openKey) return
    setPicker(null)
    setEngram("")
    setRemember(false)
  }, [openKey])

  useEffect(() => {
    if (!open) return
    const controller = new AbortController()
    const answer = (next: EngramPicker) => {
      setPicker(next)
      setEngram(engramPreselect(next))
      setRemember(false)
    }
    const timer = window.setTimeout(() => askEngrams(baseUrl, { dir, repo, taskId }, controller.signal, answer), suggestDelayMs)
    return () => {
      window.clearTimeout(timer)
      controller.abort()
    }
  }, [open, baseUrl, dir, repo, taskId])

  const shown = engramOffered(picker, adapter)
  return {
    picker,
    engram,
    remember,
    setRemember,
    pick: (name: string) => {
      setEngram(name)
      setRemember(false)
    },
    shown,
    choice: picker?.engrams.find((candidate) => candidate.name === engram),
    groups: engramGroups(picker),
    words: engramPlaceWords(picker),
    canRemember: canRememberEngram(shown, picker, engram),
  }
}

export type EngramChoiceState = ReturnType<typeof useEngramChoice>
