import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from "react"
import { X } from "lucide-react"
import { accessLabel, listResources, type Resource } from "../lib/resources"
import { useServer } from "../hooks/useServer"
import { useSettings } from "../hooks/useSettings"
import { apiFetch } from "../lib/api"
import { agentKindLabel } from "../lib/agentKind"
import {
  completionOf,
  directoryName,
  fetchDirectoryPresets,
  fetchDirectorySuggestions,
  presetFor,
  saveDirectoryPreset,
  suggestionParts,
  suggestionsWhile,
  type DirectoryPreset,
  type DirectorySuggestion,
  type DirectorySuggestions,
} from "../lib/spawnDirectories"

export type SessionPolicy = "resume" | "new"

// What the person asked for when they confirmed the dialog.
export interface SpawnAgentRequest {
  // Empty when nothing was typed, or when the dialog has no title field.
  title: string
  // Empty for the default adapter.
  agentKind: string
  // As it was typed: a path under the home directory may start with a tilde.
  workingDir: string
  // Set only when the chosen agent tool has a saved session to resume.
  sessionPolicy?: SessionPolicy
  // The names of the resources the agent is given; absent when none were picked.
  resources?: string[]
  // False starts the agent outside Legato's sandbox; absent otherwise.
  sandbox?: false
  // The agent's first message; absent when nothing was written.
  prompt?: string
}

interface AdapterInfo {
  adapters: string[]
  default: string
}

// How long the path rests before its suggestions are asked for.
const suggestDelayMs = 120
// On a phone the presets past this many wait behind a row.
const presetsShownOnPhone = 3
// The suggestion list: its distance from the path field and from the edge of
// the window, how tall it may get, and the room below the field under which
// it opens upward when there is more room there.
const listGap = 6
const listMargin = 8
const listMaxHeight = 264
const listMinRoom = 132

const fieldLabel = "text-[13px] text-lw-text-2 sm:text-xs"
const focusRing = "focus:border-lw-accent focus:ring-[3px] focus:ring-lw-accent/25"

interface ListPlace {
  left: number
  width: number
  top?: number
  bottom?: number
  // How tall the list may be where it is.
  room: number
}

// SpawnAgentDialog asks how to start an agent: a title (when the agent is not
// a task's), the agent tool, whether to resume that tool's saved session
// (when the task has one), and where it starts. "Where" is a preset or a
// typed path, which the server completes with the directories of its own
// machine. It is a centered dialog on a wide screen and a sheet from the
// bottom on a phone. Starting the agent is the caller's: onSubmit is given
// what was chosen, and the dialog shows why it failed when it rejects.
export function SpawnAgentDialog({
  open,
  onClose,
  name,
  description,
  action,
  acting,
  titled = false,
  initialWorkingDir = "",
  resumableAgentKinds = [],
  onSubmit,
}: {
  open: boolean
  onClose: () => void
  // The dialog's heading and accessible name.
  name: string
  description: string
  // The button's words, before and while the agent is started: "Spawn" and
  // "Spawning…".
  action: string
  acting: string
  // Whether the agent gets a title of its own.
  titled?: boolean
  // Where the agent starts unless the person picks elsewhere. Empty means
  // the server's default directory.
  initialWorkingDir?: string
  // The agent tools that have a saved session for this task.
  resumableAgentKinds?: string[]
  onSubmit: (request: SpawnAgentRequest) => Promise<void>
}) {
  const { baseUrl } = useServer()
  const settings = useSettings()
  const settingsRef = useRef(settings.data)
  settingsRef.current = settings.data

  const [title, setTitle] = useState("")
  // Kept from one opening to the next: the person's last choice of tool.
  const [agentKind, setAgentKind] = useState("")
  const [adapters, setAdapters] = useState<AdapterInfo | null>(null)
  // False until the adapters are known, since the default one decides
  // whether there is a session to resume.
  const [ready, setReady] = useState(false)
  const [sessionPolicy, setSessionPolicy] = useState<SessionPolicy>("resume")
  const [presets, setPresets] = useState<DirectoryPreset[]>([])
  // False for a server with no directory service: its presets are the
  // configured repositories, and a path is typed without help.
  const [serverKnowsDirectories, setServerKnowsDirectories] = useState(false)
  const [path, setPath] = useState("")
  const [found, setFound] = useState<DirectorySuggestions | null>(null)
  const [listOpen, setListOpen] = useState(false)
  const [active, setActive] = useState(0)
  // True once the arrow keys chose a suggestion, which enter then takes.
  const [chosen, setChosen] = useState(false)
  const [place, setPlace] = useState<ListPlace | null>(null)
  const [listHeight, setListHeight] = useState(0)
  const [showAll, setShowAll] = useState(false)
  const [savePath, setSavePath] = useState(false)
  // Out of Legato's sandbox for this one agent: asked afresh each time.
  const [unsandboxed, setUnsandboxed] = useState(false)
  const [prompt, setPrompt] = useState("")
  // The files and folders the person keeps for agents, and the ones picked.
  const [resources, setResources] = useState<Resource[]>([])
  const [picked, setPicked] = useState<string[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState("")

  const formRef = useRef<HTMLFormElement>(null)
  const titleRef = useRef<HTMLInputElement>(null)
  const pathFieldRef = useRef<HTMLDivElement>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const pathRef = useRef(path)
  pathRef.current = path
  const listId = useId()

  useEffect(() => {
    if (!open) return
    let current = true
    setError("")
    setLoading(false)
    setReady(false)
    setTitle("")
    setSessionPolicy("resume")
    setFound(null)
    setListOpen(false)
    setSavePath(false)
    setUnsandboxed(false)
    setPrompt("")
    // Not the path field: its suggestions open with its focus.
    ;(titleRef.current ?? formRef.current)?.focus()

    apiFetch(baseUrl, "/api/adapters")
      .then(async (response) => {
        if (!response.ok) return
        const data = await response.json()
        if (current) setAdapters({ adapters: data.adapters ?? [], default: data.default ?? "" })
      })
      .catch(() => {})
      .finally(() => { if (current) setReady(true) })

    setPicked([])
    listResources(baseUrl)
      .then((listed) => { if (current) setResources(listed) })
      .catch(() => { if (current) setResources([]) })

    fetchDirectoryPresets(baseUrl)
      .catch(() => null)
      .then((listed) => {
        if (!current) return
        const configured = settingsRef.current
        const offered = listed?.presets ?? (configured?.repositories ?? [])
          .filter((repository) => repository.primary_dir)
          .map((repository) => ({ name: repository.name, path: repository.primary_dir, display: repository.primary_dir, saved: false }))
        const start = initialWorkingDir || (listed?.default.display ?? configured?.working_dir ?? "")
        setServerKnowsDirectories(listed !== null)
        setPresets(offered)
        setPath(start)
        // A start that is no preset is a custom path, so the phone shows it.
        setShowAll(!presetFor(start, offered))
      })
    return () => { current = false }
  }, [open, baseUrl, initialWorkingDir])

  const suggests = open && listOpen && serverKnowsDirectories

  useEffect(() => {
    if (!suggests) return
    const controller = new AbortController()
    const nothing = (): DirectorySuggestions => ({ query: path, resolved: "", exists: false, base: "", prefix: "", items: [], total: 0 })
    const timer = window.setTimeout(() => {
      fetchDirectorySuggestions(baseUrl, path, controller.signal)
        // A request that fails has no suggestions, which is also what ends
        // the wait of a list that is held for it.
        .catch(() => null)
        .then((result) => {
          if (controller.signal.aborted || pathRef.current !== path) return
          setFound(result ?? nothing())
          setActive(0)
          setChosen(false)
        })
    }, suggestDelayMs)
    return () => {
      window.clearTimeout(timer)
      controller.abort()
    }
  }, [suggests, baseUrl, path])

  const selected = presetFor(path, presets)
  const where = selected?.name ?? directoryName(path)
  const canSave = serverKnowsDirectories && path.trim() !== "" && !selected

  // What is listed under the path, and what of it the keys can take. The
  // last answer stays up, dimmed, while the one for another directory is on
  // its way; see suggestionsWhile.
  const listed = suggestionsWhile(found, path)
  const shown = suggests ? listed.items : []
  const takeable = listed.held ? [] : shown
  const suggesting = takeable.length > 0
  // Under a path that ends in a slash the suggestions are what is inside the
  // directory, and none of them is highlighted until the arrow keys pick one.
  const unpicked = path.endsWith("/") && !chosen
  const current = suggesting && !unpicked ? Math.min(Math.max(active, 0), takeable.length - 1) : -1
  const ghost = completionOf(path, takeable[current])
  const more = found !== null && !listed.held && found.query === path && found.total > shown.length
  const shownKey = shown.map((item) => item.path).join("\n") + (more ? "\n+" : "")

  const defaultAdapter = adapters?.default ?? ""
  const agentOptions = [
    { value: "", label: defaultAdapter ? agentKindLabel(defaultAdapter) : "Default", note: defaultAdapter ? "default" : "" },
    ...(adapters?.adapters ?? [])
      .filter((adapter) => adapter !== defaultAdapter)
      .map((adapter) => ({ value: adapter, label: agentKindLabel(adapter), note: "" })),
    { value: "shell", label: agentKindLabel("shell"), note: "" },
  ]
  const effectiveAdapter = agentKind || defaultAdapter
  const offersSessionChoice = resumableAgentKinds.includes(effectiveAdapter)
  const toolLabel = agentKindLabel(effectiveAdapter)

  // The list floats over the dialog, so that it neither resizes the dialog
  // nor is cut off by it. It is placed against the path field: under it, or
  // over it when the window ends too soon below.
  useLayoutEffect(() => {
    if (!suggests) return
    const measure = () => {
      const field = pathFieldRef.current?.getBoundingClientRect()
      if (!field) return
      const viewport = window.visualViewport
      const top = viewport?.offsetTop ?? 0
      const bottom = top + (viewport?.height ?? window.innerHeight)
      const below = bottom - field.bottom - listGap - listMargin
      const above = field.top - top - listGap - listMargin
      const opensUp = below < listMinRoom && above > below
      setPlace({
        left: field.left,
        width: field.width,
        room: Math.max(Math.min(listMaxHeight, opensUp ? above : below), 0),
        ...(opensUp ? { bottom: window.innerHeight - field.top + listGap } : { top: field.bottom + listGap }),
      })
    }
    measure()
    window.addEventListener("resize", measure)
    // Capturing: the dialog's own content scrolls, and the field with it.
    window.addEventListener("scroll", measure, true)
    return () => {
      window.removeEventListener("resize", measure)
      window.removeEventListener("scroll", measure, true)
    }
  }, [suggests, showAll, presets.length, error, offersSessionChoice])

  // Its height is set, not left to its content, so that it grows and shrinks
  // smoothly from one answer to the next.
  useLayoutEffect(() => {
    setListHeight(shown.length > 0 ? listRef.current?.scrollHeight ?? 0 : 0)
    // shownKey stands for shown: the same suggestions are the same height.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shownKey, place?.room])

  useEffect(() => {
    if (current >= 0) document.getElementById(`${listId}-${current}`)?.scrollIntoView?.({ block: "nearest" })
  }, [current, listId])

  const takeSuggestion = (item: DirectorySuggestion | undefined) => {
    if (!item) return
    setPath(item.display)
    setActive(0)
    setChosen(false)
    setListOpen(true)
  }

  const submit = useCallback(async () => {
    if (loading || !ready) return
    const workingDir = path.trim()
    setLoading(true)
    setError("")
    try {
      if (savePath && canSave) await saveDirectoryPreset(baseUrl, workingDir)
      await onSubmit({
        title: title.trim(),
        agentKind,
        workingDir,
        ...(offersSessionChoice ? { sessionPolicy } : {}),
        ...(picked.length > 0 ? { resources: picked } : {}),
        ...(unsandboxed ? { sandbox: false as const } : {}),
        ...(prompt.trim() ? { prompt: prompt.trim() } : {}),
      })
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setLoading(false)
    }
  }, [loading, ready, path, savePath, canSave, baseUrl, onSubmit, title, agentKind, offersSessionChoice, sessionPolicy, picked, unsandboxed, prompt])

  if (!open) return null

  // A dialog that is starting an agent stays until that has an answer.
  const close = () => { if (!loading) onClose() }

  const onPathKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "Escape" && shown.length > 0) {
      // Escape closes the suggestions first, and the dialog after.
      event.stopPropagation()
      setListOpen(false)
      return
    }
    if (!suggesting) return
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault()
      const step = event.key === "ArrowDown" ? 1 : -1
      setActive(Math.min(Math.max(current + step, 0), takeable.length - 1))
      setChosen(true)
    } else if (event.key === "Tab" && !event.shiftKey) {
      event.preventDefault()
      takeSuggestion(takeable[Math.max(current, 0)])
    } else if (event.key === "Enter" && chosen && !event.metaKey && !event.ctrlKey) {
      event.preventDefault()
      takeSuggestion(takeable[current])
    }
  }

  const customPathShown = showAll || presets.length === 0
  const hiddenOnPhone = showAll ? 0 : presets.filter((preset, index) => index >= presetsShownOnPhone && preset !== selected).length
  const spawnKeys = /Mac|iPhone|iPad/.test(navigator.platform) ? "⌘↵" : "Ctrl ↵"
  const busyLabel = ready ? acting : "Loading…"

  return (
    <div
      className="fixed inset-0 z-50 flex items-end justify-center bg-black/60 sm:items-center sm:p-4"
      onMouseDown={(event) => { if (event.target === event.currentTarget) close() }}
    >
      <form
        ref={formRef}
        tabIndex={-1}
        role="dialog"
        aria-modal="true"
        aria-label={name}
        onSubmit={(event) => { event.preventDefault(); void submit() }}
        onKeyDown={(event) => {
          if (event.key === "Escape") close()
          if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) { event.preventDefault(); void submit() }
        }}
        // index.css keeps every dialog a margin away from a phone's screen
        // edges; a sheet reaches them, so maxWidth sets that aside.
        style={{ maxWidth: "none" }}
        className="flex max-h-[calc(100dvh-1rem)] w-full flex-col rounded-t-2xl border-t border-lw-line-strong bg-lw-surface font-lw text-[15px] outline-none leading-5 text-lw-text shadow-[0_24px_60px_rgba(0,0,0,.5)] sm:w-[580px] sm:rounded-xl sm:border sm:text-[13px]"
      >
        <div aria-hidden="true" className="flex justify-center pb-1 pt-2 sm:hidden">
          <span className="h-1 w-9 rounded-full bg-lw-line-strong" />
        </div>
        <div className="flex items-center justify-between py-1 pl-4 pr-2 sm:px-5 sm:pb-3 sm:pt-4">
          <div className="flex flex-col">
            <h2 className="text-lg font-semibold sm:text-base">{name}</h2>
            <p className="hidden text-xs text-lw-text-2 sm:block">{description}</p>
          </div>
          <button
            type="button"
            aria-label="Close"
            disabled={loading}
            onClick={close}
            className="flex h-11 w-11 items-center justify-center rounded-md text-lw-text-2 transition-colors hover:bg-lw-raised hover:text-lw-text disabled:opacity-40 sm:h-8 sm:w-8"
          >
            <X size={18} />
          </button>
        </div>

        <div className="flex min-h-0 flex-col gap-4 overflow-y-auto px-4 pb-4 pt-2 sm:px-5 sm:pt-1">
          {error && (
            <div role="alert" className="rounded-md border border-lw-danger-line bg-lw-error-soft px-3 py-2 text-xs text-lw-danger">
              {error}
            </div>
          )}

          {titled && (
            <label className="flex flex-col gap-1.5">
              <span className={fieldLabel}>Title</span>
              <input
                ref={titleRef}
                type="text"
                aria-label="Title"
                value={title}
                onChange={(event) => setTitle(event.target.value)}
                placeholder="Ephemeral session"
                className={`h-[46px] rounded-[10px] border border-lw-line-strong bg-lw-ground px-3.5 text-base text-lw-text outline-none placeholder:text-lw-faint sm:h-9 sm:rounded-md sm:px-3 sm:text-sm ${focusRing}`}
              />
            </label>
          )}

          <div className="flex flex-col gap-1.5">
            <span className={fieldLabel}>Agent</span>
            <div role="radiogroup" aria-label="Agent" className="flex rounded-[10px] border border-lw-line-strong bg-lw-ground p-[3px] sm:rounded-md sm:p-0.5">
              {agentOptions.map((option) => {
                const checked = option.value === agentKind
                return (
                  <button
                    key={option.value}
                    type="button"
                    role="radio"
                    aria-checked={checked}
                    onClick={() => setAgentKind(option.value)}
                    // Each takes the room its own label needs, then a share of what
                    // is left: four equal parts cut the longest label short.
                    className={`h-10 min-w-0 flex-auto truncate rounded-lg px-2 text-sm transition-colors sm:h-[30px] sm:rounded sm:text-[13px] ${
                      checked ? "bg-lw-selected font-medium text-lw-text" : "text-lw-text-2 hover:text-lw-text"
                    }`}
                  >
                    {option.label}
                    {option.note && <> <span className="hidden font-normal text-lw-faint sm:inline">{option.note}</span></>}
                  </button>
                )
              })}
            </div>
          </div>

          {offersSessionChoice && (
            <fieldset className="flex flex-col gap-1.5">
              <legend className={`${fieldLabel} mb-1.5`}>Existing {toolLabel} session</legend>
              <div className="overflow-hidden rounded-[10px] border border-lw-line-strong bg-lw-ground sm:rounded-md">
                {([
                  { value: "resume", label: "Resume previous session", note: `Continue with the previous ${toolLabel} context.` },
                  { value: "new", label: "Start new session", note: "Start fresh for this Legato task." },
                ] as const).map((choice) => (
                  <label
                    key={choice.value}
                    className={`flex min-h-[54px] cursor-pointer items-center gap-3 border-b border-lw-line px-3.5 last:border-b-0 sm:min-h-0 sm:px-3 sm:py-2 ${sessionPolicy === choice.value ? "bg-lw-selected" : ""}`}
                  >
                    <input
                      type="radio"
                      name={`${listId}-session`}
                      value={choice.value}
                      aria-label={choice.label}
                      checked={sessionPolicy === choice.value}
                      onChange={() => setSessionPolicy(choice.value)}
                      className="accent-lw-accent"
                    />
                    <span className="flex flex-col gap-px">
                      <span className="font-medium">{choice.label}</span>
                      <span className="text-xs text-lw-text-2">{choice.note}</span>
                    </span>
                  </label>
                ))}
              </div>
            </fieldset>
          )}

          <div className="flex flex-col gap-1.5">
            <span className={fieldLabel}>Where</span>
            {presets.length > 0 && (
              <div className="overflow-hidden rounded-[10px] border border-lw-line-strong bg-lw-ground sm:grid sm:grid-cols-2 sm:gap-1.5 sm:overflow-visible sm:rounded-none sm:border-0 sm:bg-transparent">
                {presets.map((preset, index) => {
                  const pressed = preset === selected
                  const waits = !showAll && index >= presetsShownOnPhone && !pressed
                  return (
                    <button
                      key={preset.path}
                      type="button"
                      aria-pressed={pressed}
                      onClick={() => { setPath(preset.display); setListOpen(false) }}
                      className={`${waits ? "hidden sm:flex" : "flex"} min-h-[54px] w-full min-w-0 items-center gap-3 border-b border-lw-line px-3.5 text-left transition-colors max-sm:last:border-b-0 sm:min-h-0 sm:rounded-lg sm:border sm:px-2.5 sm:py-2 ${
                        pressed ? "bg-lw-selected sm:border-lw-accent" : "sm:border-lw-line-strong sm:bg-lw-ground sm:hover:border-lw-faint"
                      }`}
                    >
                      <span aria-hidden="true" className={`h-[18px] w-[18px] shrink-0 rounded-full sm:hidden ${pressed ? "border-[5px] border-lw-accent" : "border-[1.5px] border-lw-faint"}`} />
                      <span className="flex min-w-0 flex-col gap-px">
                        <span className="truncate text-[15px] font-medium sm:text-[13px]">{preset.name}</span>
                        <span className="truncate font-lw-mono text-[11px] text-lw-faint">{preset.display}</span>
                      </span>
                    </button>
                  )
                })}
                {!showAll && (
                  <button
                    type="button"
                    aria-expanded="false"
                    onClick={() => setShowAll(true)}
                    className="flex min-h-[50px] w-full items-center justify-between px-3.5 text-left text-sm text-lw-text-2 sm:hidden"
                  >
                    <span>{hiddenOnPhone > 0 ? `${hiddenOnPhone} more · Custom path` : "Custom path"}</span>
                    <span aria-hidden="true">›</span>
                  </button>
                )}
              </div>
            )}

            <div className={`${customPathShown ? "block" : "hidden sm:block"} sm:mt-0.5`}>
              <div
                ref={pathFieldRef}
                className="flex h-[46px] items-center gap-2 rounded-[10px] border border-lw-line-strong bg-lw-ground px-3 font-lw-mono text-base transition-shadow focus-within:border-lw-accent focus-within:ring-[3px] focus-within:ring-lw-accent/25 sm:h-[38px] sm:rounded-md sm:text-[13px]"
              >
                <span aria-hidden="true" className="text-lw-faint">path</span>
                <div className="relative min-w-0 flex-1">
                  {ghost && (
                    <div aria-hidden="true" className="pointer-events-none absolute inset-0 flex items-center overflow-hidden whitespace-pre">
                      <span className="invisible">{path}</span>
                      <span className="text-lw-faint">{ghost}</span>
                    </div>
                  )}
                  <input
                    type="text"
                    role="combobox"
                    aria-label="Path"
                    aria-autocomplete="list"
                    aria-expanded={shown.length > 0}
                    aria-controls={listId}
                    aria-activedescendant={current >= 0 ? `${listId}-${current}` : undefined}
                    autoComplete="off"
                    autoCapitalize="off"
                    spellCheck={false}
                    value={path}
                    onChange={(event) => { setPath(event.target.value); setActive(0); setChosen(false); setListOpen(true) }}
                    onFocus={() => setListOpen(true)}
                    onBlur={() => setListOpen(false)}
                    onKeyDown={onPathKeyDown}
                    placeholder="~/path/to/project"
                    className="relative w-full bg-transparent text-lw-text outline-none placeholder:text-lw-faint"
                  />
                </div>
                {ghost && (
                  <kbd className="hidden rounded border border-lw-line-strong px-1.5 font-lw text-[11px] text-lw-faint sm:block">tab</kbd>
                )}
              </div>
              {suggests && (
                // The frame is there for as long as the field has the focus,
                // at no height while there is nothing to list, so that every
                // change of what is listed is a change of height it can ease.
                <div
                  // mousedown here would take the focus from the field, and
                  // the list with it.
                  onMouseDown={(event) => event.preventDefault()}
                  style={{ position: "fixed", left: place?.left ?? 0, width: place?.width ?? 0, top: place?.top, bottom: place?.bottom, height: shown.length > 0 ? Math.min(listHeight, place?.room ?? 0) + 2 : 0 }}
                  className={`z-10 overflow-hidden rounded-lg border bg-lw-raised font-lw-mono text-xs shadow-[0_12px_30px_rgba(0,0,0,.45)] transition-[height,opacity] duration-150 ease-out motion-reduce:transition-none ${
                    shown.length > 0 ? "border-lw-line-strong opacity-100" : "pointer-events-none border-transparent opacity-0"
                  }`}
                >
                  <div ref={listRef} style={{ maxHeight: place?.room }} className="overflow-y-auto p-1">
                    {shown.length > 0 && (
                      <div
                        id={listId}
                        role="listbox"
                        aria-label="Path suggestions"
                        aria-busy={listed.held}
                        className={`transition-opacity duration-150 motion-reduce:transition-none ${listed.held ? "opacity-40" : "opacity-100"}`}
                      >
                        {shown.map((item, index) => {
                          const parts = suggestionParts(found?.base ?? "", listed.prefix, item)
                          return (
                            <div
                              key={item.path}
                              id={`${listId}-${index}`}
                              role="option"
                              aria-selected={index === current}
                              aria-disabled={listed.held}
                              onMouseDown={() => { if (!listed.held) takeSuggestion(item) }}
                              className={`flex justify-between gap-3 rounded px-2 py-1.5 ${listed.held ? "" : "cursor-pointer"} ${index === current ? "bg-lw-selected" : listed.held ? "" : "hover:bg-lw-selected/60"}`}
                            >
                              <span className="min-w-0 truncate">
                                <span className="text-lw-faint">{parts.base}</span>
                                <span className="text-lw-link">{parts.typed}</span>
                                {parts.rest}
                              </span>
                              <span className={`shrink-0 font-lw ${item.git || item.preset > 0 ? "text-lw-live" : "text-lw-faint"}`}>
                                {[item.git && "git", item.preset > 0 && "preset"].filter(Boolean).join(" · ") || "folder"}
                              </span>
                            </div>
                          )
                        })}
                      </div>
                    )}
                    {more && found && (
                      <p className="px-2 py-1 font-lw text-[11px] text-lw-faint">
                        {shown.length} of {found.total} folders · keep typing to narrow them
                      </p>
                    )}
                  </div>
                </div>
              )}
            </div>
          </div>
          {resources.length > 0 && (
            <fieldset className="flex flex-col gap-1.5">
              <legend className={`${fieldLabel} mb-1.5`}>Resources</legend>
              <div className="flex flex-wrap gap-1.5">
                {resources.map((resource) => {
                  const on = picked.includes(resource.name)
                  const paths = resource.paths.map((p) => `${p.path} (${accessLabel(p.access)})`).join("\n")
                  return (
                    <label
                      key={resource.name}
                      title={paths}
                      className={`flex min-h-11 cursor-pointer items-center gap-2 rounded-[10px] border px-3 text-sm transition-colors sm:min-h-0 sm:rounded-md sm:px-2.5 sm:py-1.5 sm:text-xs ${
                        on ? "border-lw-accent bg-lw-selected text-lw-text" : "border-lw-line-strong bg-lw-ground text-lw-text-2 hover:border-lw-faint"
                      }`}
                    >
                      <input
                        type="checkbox"
                        checked={on}
                        onChange={() => setPicked((current) => on ? current.filter((name) => name !== resource.name) : [...current, resource.name])}
                        className="accent-lw-accent"
                      />
                      {resource.name}
                      {resource.paths.some((p) => p.access === "write") && <span className="text-[11px] text-lw-faint">write</span>}
                    </label>
                  )
                })}
              </div>
            </fieldset>
          )}
          <label className="flex flex-col gap-1.5">
            <span className={fieldLabel}>
              First message <span className="text-lw-faint">· optional</span>
            </span>
            <textarea
              aria-label="First message"
              value={prompt}
              onChange={(event) => setPrompt(event.target.value)}
              rows={3}
              placeholder="What the agent should start on. Sent once it is ready."
              className={`min-h-[92px] resize-y rounded-[10px] border border-lw-line-strong bg-lw-ground px-3.5 py-2.5 text-base text-lw-text outline-none placeholder:text-lw-faint sm:min-h-[72px] sm:rounded-md sm:px-3 sm:py-2 sm:text-sm ${focusRing}`}
            />
          </label>
          {settings.data?.sandbox_enabled && (
            <label className={`flex min-h-11 cursor-pointer items-start gap-2.5 rounded-[10px] border px-3 py-2.5 text-sm sm:min-h-0 sm:rounded-md sm:text-xs ${unsandboxed ? "border-lw-needs/50 bg-lw-needs-soft text-lw-text" : "border-lw-line-strong bg-lw-ground text-lw-text-2"}`}>
              <input type="checkbox" checked={unsandboxed} onChange={(event) => setUnsandboxed(event.target.checked)} className="mt-0.5 accent-lw-accent" />
              <span>
                <span className="font-medium text-lw-text">Run without sandbox</span>
                <span className="block text-lw-faint">This agent sees your whole home folder and its credentials, so folders can be given to it while it runs.</span>
              </span>
            </label>
          )}
        </div>

        <div className="flex flex-col gap-3 border-t border-lw-line px-4 pb-[max(0.75rem,env(safe-area-inset-bottom))] pt-3 sm:flex-row sm:items-center sm:gap-2 sm:px-5 sm:pb-3">
          <label className={`${customPathShown ? "flex" : "hidden sm:flex"} items-center gap-2 text-xs text-lw-text-2 ${canSave ? "" : "opacity-50"}`}>
            <input
              type="checkbox"
              checked={savePath && canSave}
              disabled={!canSave}
              onChange={(event) => setSavePath(event.target.checked)}
              className="accent-lw-accent"
            />
            Save path as preset
          </label>
          <span className="hidden flex-1 sm:block" />
          <button
            type="button"
            disabled={loading}
            onClick={close}
            className="hidden h-[34px] rounded-md px-3.5 text-[13px] text-lw-text-2 transition-colors hover:text-lw-text disabled:opacity-40 sm:block"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={loading || !ready}
            aria-label={where ? `${action} in ${where}` : action}
            className="h-[52px] w-full rounded-xl bg-lw-accent px-4 text-base font-semibold text-white transition-opacity hover:opacity-90 disabled:opacity-40 sm:h-[34px] sm:w-auto sm:rounded-md sm:text-[13px] sm:font-medium"
          >
            <span className="sm:hidden">{loading || !ready ? busyLabel : where ? `${action} in ${where}` : action}</span>
            <span className="hidden sm:inline">
              {loading || !ready ? busyLabel : <>{action} <span className="text-white/75">{spawnKeys}</span></>}
            </span>
          </button>
        </div>
      </form>
    </div>
  )
}
