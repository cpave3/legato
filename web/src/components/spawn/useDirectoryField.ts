import { useEffect, useLayoutEffect, useReducer, useState, type Dispatch, type KeyboardEvent, type RefObject } from "react"
import { fetchDirectoryPresets, fetchDirectorySuggestions, presetFor, type DirectoryPreset, type DirectorySuggestion } from "../../lib/spawnDirectories"
import {
  directoryStart,
  initialPathField,
  listPlacement,
  noSuggestions,
  pathFieldReducer,
  pathKeyAction,
  stepCursor,
  suggestDelayMs,
  suggestionCursor,
  suggestionView,
  type DirectorySettings,
  type ListPlace,
  type PathFieldEvent,
  type SuggestionView,
} from "../../lib/spawnSuggestions"

// The presets offered and the path, set afresh each time the dialog opens:
// the server's presets (or, from a server with no directory service, the
// configured repositories with a folder) and its default directory.
function useDirectoryPresets(openKey: string, baseUrl: string, initialWorkingDir: string, settingsRef: RefObject<DirectorySettings | undefined>) {
  const [presets, setPresets] = useState<DirectoryPreset[]>([])
  const [serverKnowsDirectories, setServerKnowsDirectories] = useState(false)
  const [path, setPath] = useState("")
  const [showAll, setShowAll] = useState(false)
  const [savePath, setSavePath] = useState(false)

  useEffect(() => {
    if (!openKey) return
    let current = true
    setSavePath(false)
    fetchDirectoryPresets(baseUrl)
      .catch(() => null)
      .then((listed) => {
        if (!current) return
        const start = directoryStart(listed, settingsRef.current, initialWorkingDir)
        setServerKnowsDirectories(start.serverKnowsDirectories)
        setPresets(start.presets)
        setPath(start.path)
        setShowAll(start.showAll)
      })
    return () => { current = false }
  }, [openKey, baseUrl, initialWorkingDir, settingsRef])

  return { presets, serverKnowsDirectories, path, setPath, showAll, setShowAll, savePath, setSavePath }
}

// Asks the server for the directories under the path once it rests, while
// the list is open. A request that fails has no suggestions, which is also
// what ends the wait of a list that is held for it. An answer for a path
// typed over is dropped: the next path aborts it.
function askSuggestions(baseUrl: string, path: string, signal: AbortSignal, dispatch: Dispatch<PathFieldEvent>) {
  fetchDirectorySuggestions(baseUrl, path, signal)
    .catch(() => null)
    .then((result) => {
      if (signal.aborted) return
      dispatch({ type: "answer", found: result ?? noSuggestions(path) })
    })
}

function useSuggestionAnswers(suggests: boolean, baseUrl: string, path: string, dispatch: Dispatch<PathFieldEvent>) {
  useEffect(() => {
    if (!suggests) return
    const controller = new AbortController()
    const timer = window.setTimeout(() => askSuggestions(baseUrl, path, controller.signal, dispatch), suggestDelayMs)
    return () => {
      window.clearTimeout(timer)
      controller.abort()
    }
  }, [suggests, baseUrl, path, dispatch])
}

// The list floats over the dialog, so that it neither resizes the dialog
// nor is cut off by it. It is placed against the path field: under it, or
// over it when the window ends too soon below. layoutKey changes whenever
// something above the field may have moved it.
export function useSuggestionListPlace(fieldRef: RefObject<HTMLDivElement | null>, listRef: RefObject<HTMLDivElement | null>, dir: DirectoryField, layoutKey: string, listId: string) {
  const { suggests, view, current } = dir
  const [place, setPlace] = useState<ListPlace | null>(null)
  const [listHeight, setListHeight] = useState(0)

  useLayoutEffect(() => {
    if (!suggests) return
    const measure = () => {
      const field = fieldRef.current?.getBoundingClientRect()
      if (!field) return
      const viewport = window.visualViewport
      setPlace(listPlacement(field, viewport?.offsetTop ?? 0, viewport?.height ?? window.innerHeight, window.innerHeight))
    }
    measure()
    window.addEventListener("resize", measure)
    // Capturing: the dialog's own content scrolls, and the field with it.
    window.addEventListener("scroll", measure, true)
    return () => {
      window.removeEventListener("resize", measure)
      window.removeEventListener("scroll", measure, true)
    }
  }, [suggests, layoutKey, fieldRef])

  // Its height is set, not left to its content, so that it grows and shrinks
  // smoothly from one answer to the next.
  const shownCount = view.shown.length
  useLayoutEffect(() => {
    setListHeight(shownCount > 0 ? listRef.current?.scrollHeight ?? 0 : 0)
    // shownKey stands for shown: the same suggestions are the same height.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view.shownKey, place?.room])

  useEffect(() => {
    if (current >= 0) document.getElementById(`${listId}-${current}`)?.scrollIntoView?.({ block: "nearest" })
  }, [current, listId])

  return { place, listHeight }
}

// What a key in the path field does to the list; see pathKeyAction.
function onPathKey(event: KeyboardEvent<HTMLInputElement>, view: SuggestionView, current: number, chosen: boolean, dispatch: Dispatch<PathFieldEvent>, take: (item: DirectorySuggestion | undefined) => void) {
  const action = pathKeyAction(event, view.shown.length > 0, view.takeable.length > 0, chosen)
  if (!action) return
  event.preventDefault()
  if (action === "close") {
    event.stopPropagation()
    dispatch({ type: "close" })
  } else if (action === "complete") {
    take(view.takeable[Math.max(current, 0)])
  } else if (action === "take") {
    take(view.takeable[current])
  } else {
    dispatch({ type: "move", to: stepCursor(current, action === "down" ? 1 : -1, view.takeable.length) })
  }
}

interface DirectoryFieldOptions {
  // Changes whenever the dialog opens afresh; empty while it is closed.
  openKey: string
  baseUrl: string
  initialWorkingDir: string
  settingsRef: RefObject<DirectorySettings | undefined>
  // False while the agent starts in a clone, which has no path.
  folder: boolean
}

// useDirectoryField is the spawn dialog's "Where" as a folder: the presets,
// the path, and the suggestions the server completes it with, with the keys
// that move along them.
export function useDirectoryField({ openKey, baseUrl, initialWorkingDir, settingsRef, folder }: DirectoryFieldOptions) {
  const presetState = useDirectoryPresets(openKey, baseUrl, initialWorkingDir, settingsRef)
  const { path, setPath, presets, serverKnowsDirectories } = presetState
  const [state, dispatch] = useReducer(pathFieldReducer, initialPathField)

  useEffect(() => {
    if (openKey) dispatch({ type: "reset" })
  }, [openKey])

  const suggests = openKey !== "" && state.listOpen && serverKnowsDirectories && folder
  useSuggestionAnswers(suggests, baseUrl, path, dispatch)
  const view = suggestionView(state.found, path, suggests)
  const { current, ghost } = suggestionCursor(path, view.takeable, state.active, state.chosen)

  const edit = (value: string) => {
    setPath(value)
    dispatch({ type: "edit" })
  }
  const take = (item: DirectorySuggestion | undefined) => { if (item) edit(item.display) }

  return {
    ...presetState,
    selected: presetFor(path, presets),
    found: state.found,
    suggests,
    view,
    current,
    ghost,
    edit,
    take,
    openList: () => dispatch({ type: "open" }),
    closeList: () => dispatch({ type: "close" }),
    onKeyDown: (event: KeyboardEvent<HTMLInputElement>) => onPathKey(event, view, current, state.chosen, dispatch, take),
  }
}

export type DirectoryField = ReturnType<typeof useDirectoryField>
