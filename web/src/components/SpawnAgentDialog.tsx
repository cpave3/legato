import { useQuery } from "@tanstack/react-query"
import { fetchAdapters } from "../lib/adapters"
import { queryKeys } from "../lib/query"
import { useModalLayer } from "./ui/useModalLayer"
import { useCallback, useEffect, useId, useRef, useState } from "react"
import { ChevronDown, X } from "lucide-react"
import { accessLabel, listResources, type Resource } from "../lib/resources"
import { macroMessage, pickerMacros, useMacros } from "../lib/macros"
import { useServer } from "../hooks/useServer"
import { useSettings } from "../hooks/useSettings"
import { agentKindLabel } from "../lib/agentKind"
import { engramMarkLabel, fetchEngramPicker, takesEngram, type EngramPicker } from "../lib/engrams"
import { directoryName, saveDirectoryPreset } from "../lib/spawnDirectories"
import { suggestDelayMs } from "../lib/spawnSuggestions"
import { buildSpawnRequest, spawnWorkingDir, type SessionPolicy, type SpawnAgentRequest } from "../lib/spawnRequest"
import { useDirectoryField } from "./spawn/useDirectoryField"
import { useCloneChoice } from "./spawn/useCloneChoice"
import { WhereField } from "./spawn/WhereField"
import { fieldLabel, focusRing } from "./spawn/styles"

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
  defaultBranch = "",
  taskId = "",
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
  // The branch a fresh clone ends on unless the person types another. Empty
  // leaves it to the server: legato/<task id>.
  defaultBranch?: string
  // The task the agent is for: its own clone of a repository is offered
  // first, and no new one beside it. Empty for an agent with no task yet.
  taskId?: string
  onSubmit: (request: SpawnAgentRequest) => Promise<void>
}) {
  const { baseUrl } = useServer()
  const settings = useSettings()
  const settingsRef = useRef(settings.data)
  settingsRef.current = settings.data

  const [title, setTitle] = useState("")
  // Kept from one opening to the next: the person's last choice of tool.
  const [agentKind, setAgentKind] = useState("")
  const [account, setAccount] = useState("")
  const adapterQuery = useQuery({ queryKey: queryKeys.adapters(baseUrl), queryFn: () => fetchAdapters(baseUrl), enabled: open, staleTime: 0, retry: false })
  const adapters = adapterQuery.data
  // False until the adapters are known, since the default one decides
  // whether there is a session to resume.
  const ready = adapterQuery.isFetched
  const [sessionPolicy, setSessionPolicy] = useState<SessionPolicy>("resume")
  // Out of Legato's sandbox for this one agent: asked afresh each time.
  const [unsandboxed, setUnsandboxed] = useState(false)
  const [hostNetwork, setHostNetwork] = useState(false)
  // Only for an agent in the sandbox, with Docker in it.
  const hostNetworkOffered = Boolean(settings.data?.sandbox_enabled && settings.data?.sandbox_docker_enabled) && !unsandboxed
  const [prompt, setPrompt] = useState("")
  // "Start with macro": the macros, the one chosen, and the tier it names.
  const macroList = useMacros(baseUrl, open ? "open" : null)
  const [macroName, setMacroName] = useState("")
  // The task's macros by the pickers' rule (LEG-619). A select cannot hide
  // the rest behind Show all: it groups the favourites first.
  const spawnMacros = pickerMacros(macroList.macros, { targets: ["task"] })
  const [tier, setTier] = useState("")
  const chooseMacro = (name: string) => {
    setMacroName(name)
    const macro = macroList.macros.find((item) => item.name === name)
    setTier(macro?.tier ?? "")
    setAccount(macro?.account ?? "")
    if (!macro) return
    setPrompt(macroMessage(macro.keys))
    if (macro.agent || macro.account) setAgentKind(macro.agent || "claude-code")
  }
  // The files and folders the person keeps for agents, and the ones picked.
  const [resources, setResources] = useState<Resource[]>([])
  const [picked, setPicked] = useState<string[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState("")
  // The engrams the agent can start as, and the one picked ("" for none).
  const [engramPicker, setEngramPicker] = useState<EngramPicker | null>(null)
  const [engram, setEngram] = useState("")
  // Ticked: the pick is bound to the place and made its default, or with
  // no engram the place's default is cleared. Unticked by every new pick.
  const [rememberEngram, setRememberEngram] = useState(false)
  const pickEngram = (name: string) => {
    setEngram(name)
    setRememberEngram(false)
  }

  const formRef = useRef<HTMLFormElement>(null)
  useModalLayer({ open, onClose, containerRef: formRef, dismissible: !loading })
  const titleRef = useRef<HTMLInputElement>(null)
  const listId = useId()

  const defaultAdapter = adapters?.default ?? ""
  const effectiveAdapter = agentKind || defaultAdapter
  const offersSessionChoice = resumableAgentKinds.includes(effectiveAdapter)
  // Changes whenever the dialog opens afresh, when each part starts over.
  const openKey = open ? JSON.stringify([baseUrl, initialWorkingDir, defaultBranch, taskId]) : ""
  const clone = useCloneChoice({ openKey, baseUrl, taskId, defaultBranch, settings: settings.data, settingsRef })
  const { inClone, repository: cloneRepository, jobClone, branch, baseBranch, jobClonesError } = clone
  const dir = useDirectoryField({ openKey, baseUrl, initialWorkingDir, settingsRef, folder: clone.whereKind === "folder" })
  const { path, presets, selected, serverKnowsDirectories, savePath, showAll } = dir

  useEffect(() => {
    if (!open) return
    let current = true
    setError("")
    setLoading(false)
    setTitle("")
    setSessionPolicy("resume")
    setUnsandboxed(false)
    setHostNetwork(false)
    setPrompt("")
    setMacroName("")
    setTier("")
    setEngramPicker(null)
    setEngram("")
    setRememberEngram(false)
    setPicked([])
    listResources(baseUrl)
      .then((listed) => { if (current) setResources(listed) })
      .catch(() => { if (current) setResources([]) })
    return () => { current = false }
  }, [open, baseUrl, initialWorkingDir, defaultBranch, taskId])

  const where = inClone ? cloneRepository?.name ?? "" : selected?.name ?? directoryName(path)
  const canSave = !inClone && serverKnowsDirectories && path.trim() !== "" && !selected

  // The engrams are asked for again whenever "Where" changes: the place
  // decides which are bound there and which is picked first.
  const engramRepo = inClone ? cloneRepository?.repo ?? "" : ""
  const engramDir = inClone ? "" : path.trim()
  useEffect(() => {
    if (!open) return
    const controller = new AbortController()
    const timer = window.setTimeout(() => {
      fetchEngramPicker(baseUrl, { dir: engramDir, repo: engramRepo, taskId }, controller.signal)
        .then((picker) => {
          if (controller.signal.aborted) return
          setEngramPicker(picker)
          setEngram(picker.enabled ? picker.preselect ?? "" : "")
          setRememberEngram(false)
        })
        .catch(() => {})
    }, suggestDelayMs)
    return () => {
      window.clearTimeout(timer)
      controller.abort()
    }
  }, [open, baseUrl, engramDir, engramRepo, taskId])

  // The default follows the usage profile of the work's repository, which
  // may differ from the global default, so it names no adapter; every
  // adapter, the global default's too, stays an explicit override.
  const agentOptions = [
    { value: "", label: "Default", note: "usage profile" },
    ...[...new Set([...(adapters?.adapters ?? []), ...(agentKind && agentKind !== "shell" ? [agentKind] : [])])]
      .map((adapter) => ({ value: adapter, label: agentKindLabel(adapter), note: "" })),
    { value: "shell", label: agentKindLabel("shell"), note: "" },
  ]
  const engramShown = !!engramPicker?.enabled && takesEngram(effectiveAdapter)
  const engramChoice = engramPicker?.engrams.find((choice) => choice.name === engram)
  // The engrams of this place first (the task's last one leads them), then
  // the rest.
  const placeEngrams = (engramPicker?.engrams ?? []).filter((choice) => choice.bound || choice.mark === "last" || choice.mark === "default")
  const otherEngrams = (engramPicker?.engrams ?? []).filter((choice) => !placeEngrams.includes(choice))
  const placeWord = engramPicker?.place?.kind === "folder" ? "This folder" : "This repo"
  const placeNoun = engramPicker?.place?.kind === "folder" ? "folder" : "repo"
  const placeDefault = engramPicker?.place?.default ?? ""
  // Remember is offered only when ticking it would change something.
  const canRemember = engramShown && !!engramPicker?.place && (engram ? engram !== placeDefault : placeDefault !== "")
  const toolLabel = agentKindLabel(effectiveAdapter)

  const submit = useCallback(async () => {
    if (loading || !ready) return
    setLoading(true)
    setError("")
    try {
      if (savePath && canSave) await saveDirectoryPreset(baseUrl, spawnWorkingDir({ inClone, path }))
      await onSubmit(buildSpawnRequest({
        title, agentKind, effectiveAdapter, account, path, inClone, jobClone, cloneRepo: cloneRepository?.repo, branch, baseBranch,
        offersSessionChoice, sessionPolicy, resources: picked, unsandboxed, hostNetworkOffered, hostNetwork, prompt, tier,
        engramShown, engram, canRemember, rememberEngram,
      }))
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setLoading(false)
    }
  }, [loading, ready, path, savePath, canSave, baseUrl, onSubmit, title, agentKind, effectiveAdapter, account, offersSessionChoice, sessionPolicy, picked, unsandboxed, hostNetworkOffered, hostNetwork, prompt, tier, cloneRepository, baseBranch, branch, inClone, jobClone, engramShown, engram, canRemember, rememberEngram])

  if (!open) return null

  // A dialog that is starting an agent stays until that has an answer.
  const close = () => { if (!loading) onClose() }

  const customPathShown = !inClone && (showAll || presets.length === 0)
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
        <div className="flex shrink-0 items-center justify-between py-1 pl-4 pr-2 sm:px-5 sm:pb-3 sm:pt-4">
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
          {jobClonesError && (
            <div role="alert" className="rounded-md border border-lw-danger-line bg-lw-error-soft px-3 py-2 text-xs text-lw-danger">
              {jobClonesError}
            </div>
          )}
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
                data-autofocus
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
                    onClick={() => { setAgentKind(option.value); setAccount("") }}
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

          {effectiveAdapter === "claude-code" && (adapters?.accounts?.length ?? 0) > 0 && (
            <label className="flex flex-col gap-1.5">
              <span className={fieldLabel}>Account</span>
              <select aria-label="Account" value={account} onChange={(event) => setAccount(event.target.value)} disabled={offersSessionChoice && sessionPolicy === "resume"}
                className={`h-[46px] w-full min-w-0 rounded-[10px] border border-lw-line-strong bg-lw-ground px-3 text-base text-lw-text sm:h-9 sm:rounded-md sm:text-sm ${focusRing}`}>
                <option value="">{offersSessionChoice && sessionPolicy === "resume" ? "Saved session account" : agentKind ? "Default account" : "Follow usage profile"}</option>
                {!agentKind && <option value="default">Default account</option>}
                {adapters?.accounts?.map((item) => <option key={item.name} value={item.name}>{item.label || item.name}</option>)}
              </select>
            </label>
          )}

          {engramShown && (
            <div className="flex flex-col gap-1.5">
              <label className="flex flex-col gap-1.5">
                <span className={fieldLabel}>Engram</span>
                <span className="relative flex">
                  <select
                    aria-label="Engram"
                    value={engram}
                    onChange={(event) => pickEngram(event.target.value)}
                    className={`h-[46px] w-full min-w-0 appearance-none truncate rounded-[10px] border border-lw-line-strong bg-lw-ground pl-3 pr-9 text-base text-lw-text outline-none sm:h-9 sm:rounded-md sm:text-sm ${focusRing}`}
                  >
                    <option value="">No engram</option>
                    {placeEngrams.length > 0 && (
                      <optgroup label={placeWord}>
                        {placeEngrams.map((choice) => (
                          <option key={choice.name} value={choice.name}>
                            {engramMarkLabel(choice.mark) ? `${choice.name} · ${engramMarkLabel(choice.mark)}` : choice.name}
                          </option>
                        ))}
                      </optgroup>
                    )}
                    {placeEngrams.length > 0
                      ? otherEngrams.length > 0 && (
                        <optgroup label="Other engrams">
                          {otherEngrams.map((choice) => <option key={choice.name} value={choice.name}>{choice.name}</option>)}
                        </optgroup>
                      )
                      : otherEngrams.map((choice) => <option key={choice.name} value={choice.name}>{choice.name}</option>)}
                  </select>
                  <ChevronDown aria-hidden="true" size={16} className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-lw-faint" />
                </span>
              </label>
              {engramChoice?.summary && <p className="text-xs text-lw-faint">{engramChoice.summary}</p>}
              {canRemember && engramPicker?.place && (
                <label className="flex cursor-pointer items-start gap-2.5 text-sm text-lw-text-2 sm:text-xs">
                  <input
                    type="checkbox"
                    checked={rememberEngram}
                    onChange={(event) => setRememberEngram(event.target.checked)}
                    className="mt-0.5 accent-lw-accent"
                  />
                  <span>
                    <span className="text-lw-text">{engram ? `Remember for this ${placeNoun}` : `Clear this ${placeNoun}'s default`}</span>
                    {engram && (
                      <span className="block text-lw-faint">Bind {engram} to {engramPicker.place.label} and make it the default.</span>
                    )}
                  </span>
                </label>
              )}
            </div>
          )}

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

          <WhereField dir={dir} clone={clone} listId={listId} customPathShown={customPathShown} layoutKey={JSON.stringify([error, offersSessionChoice])} />
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
          {spawnMacros.favorites.length + spawnMacros.rest.length > 0 && <label className="flex flex-col gap-1.5">
            <span className={fieldLabel}>Start with macro <span className="text-lw-faint">· optional</span></span>
            <select aria-label="Start with macro" value={macroName} onChange={(event) => chooseMacro(event.target.value)}
              className={`min-h-11 rounded-[10px] border border-lw-line-strong bg-lw-ground px-3 text-base text-lw-text sm:min-h-9 sm:rounded-md sm:text-sm ${focusRing}`}>
              <option value="">None</option>
              {spawnMacros.rest.length === 0
                ? spawnMacros.favorites.map((macro) => <option key={macro.name} value={macro.name}>{macro.name}</option>)
                : <>
                  <optgroup label="Favourites">{spawnMacros.favorites.map((macro) => <option key={macro.name} value={macro.name}>{macro.name}</option>)}</optgroup>
                  <optgroup label="Other macros">{spawnMacros.rest.map((macro) => <option key={macro.name} value={macro.name}>{macro.name}</option>)}</optgroup>
                </>}
            </select>
            {tier && <span className="text-xs text-lw-text-2">Model tier: {tier}</span>}
          </label>}
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
          {hostNetworkOffered && (
            <label className={`flex min-h-11 cursor-pointer items-start gap-2.5 rounded-[10px] border px-3 py-2.5 text-sm sm:min-h-0 sm:rounded-md sm:text-xs ${hostNetwork ? "border-lw-needs/50 bg-lw-needs-soft text-lw-text" : "border-lw-line-strong bg-lw-ground text-lw-text-2"}`}>
              <input type="checkbox" checked={hostNetwork} onChange={(event) => setHostNetwork(event.target.checked)} className="mt-0.5 accent-lw-accent" />
              <span>
                <span className="font-medium text-lw-text">Allow the host network for its containers</span>
                <span className="block text-lw-faint">Its containers may use network_mode: host, and reach what this machine reaches, Legato's own local services included.</span>
              </span>
            </label>
          )}
        </div>

        <div className="flex shrink-0 flex-col gap-3 border-t border-lw-line px-4 pb-[max(0.75rem,env(safe-area-inset-bottom))] pt-3 sm:flex-row sm:items-center sm:gap-2 sm:px-5 sm:pb-3">
          <label className={`${inClone ? "hidden" : customPathShown ? "flex" : "hidden sm:flex"} items-center gap-2 text-xs text-lw-text-2 ${canSave ? "" : "opacity-50"}`}>
            <input
              type="checkbox"
              checked={savePath && canSave}
              disabled={!canSave}
              onChange={(event) => dir.setSavePath(event.target.checked)}
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
            className="h-[52px] w-full rounded-xl bg-lw-accent px-4 text-base font-semibold text-[var(--theme-canvas,#fff)] transition-opacity hover:opacity-90 disabled:opacity-40 sm:h-[34px] sm:w-auto sm:rounded-md sm:text-[13px] sm:font-medium"
          >
            <span className="sm:hidden">{loading || !ready ? busyLabel : where ? `${action} in ${where}` : action}</span>
            <span className="hidden sm:inline">
              {loading || !ready ? busyLabel : <>{action} <span className="text-[var(--theme-canvas,#fff)]/75">{spawnKeys}</span></>}
            </span>
          </button>
        </div>
      </form>
    </div>
  )
}
