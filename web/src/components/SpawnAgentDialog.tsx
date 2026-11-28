import { useEffect, useId, useLayoutEffect, useRef, useState } from "react"
import { X } from "lucide-react"
import { useModalLayer } from "./ui/useModalLayer"
import { useServer } from "../hooks/useServer"
import { useSettings } from "../hooks/useSettings"
import { saveDirectoryPreset } from "../lib/spawnDirectories"
import { canSavePath, pathVisibility, whereName } from "../lib/spawnSuggestions"
import { buildSpawnRequest, spawnWorkingDir, type SpawnAgentRequest } from "../lib/spawnRequest"
import { useDirectoryField } from "./spawn/useDirectoryField"
import { useCloneChoice } from "./spawn/useCloneChoice"
import { useEngramChoice } from "./spawn/useEngramChoice"
import { useAgentChoice } from "./spawn/useAgentChoice"
import { useMacroChoice } from "./spawn/useMacroChoice"
import { useSpawnSubmit } from "./spawn/useSpawnSubmit"
import { useResources } from "./spawn/useResources"
import { ResourcesField } from "./spawn/ResourcesField"
import { useManagedHooks } from "./spawn/useManagedHooks"
import { ManagedHooksField } from "./spawn/ManagedHooksField"
import { AccountField, AgentField } from "./spawn/AgentField"
import { SessionPolicyField } from "./spawn/SessionPolicyField"
import { MacroField } from "./spawn/MacroField"
import { EngramField } from "./spawn/EngramField"
import { WhereField } from "./spawn/WhereField"
import { SandboxToggles } from "./spawn/StartFields"
import { ErrorNote, SpawnFooter, SpawnSheet } from "./spawn/SpawnChrome"
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
  // The settings as they are when the dialog opens, which the parts read
  // then without starting over when the settings change. Kept current
  // before any effect of this commit reads it.
  const settingsRef = useRef(settings.data)
  useLayoutEffect(() => { settingsRef.current = settings.data })
  // Changes whenever the dialog opens afresh, when each part starts over.
  const openKey = open ? JSON.stringify([baseUrl, initialWorkingDir, defaultBranch, taskId]) : ""
  const agent = useAgentChoice({ open, openKey, baseUrl, resumableAgentKinds })
  const { agentKind, setAgentKind, account, setAccount, effectiveAdapter, offersSessionChoice, sessionPolicy, ready } = agent

  const [title, setTitle] = useState("")
  // Out of Legato's sandbox for this one agent: asked afresh each time.
  const [unsandboxed, setUnsandboxed] = useState(false)
  const [hostNetwork, setHostNetwork] = useState(false)
  const sandboxed = Boolean(settings.data?.sandbox_enabled)
  // Only for an agent in the sandbox, with Docker in it.
  const hostNetworkOffered = Boolean(sandboxed && settings.data?.sandbox_docker_enabled) && !unsandboxed
  const [prompt, setPrompt] = useState("")
  const macro = useMacroChoice({ open, openKey, baseUrl, setPrompt, setAgentKind, setAccount })
  const resources = useResources(openKey, baseUrl)
  const managedHooks = useManagedHooks(openKey, baseUrl, effectiveAdapter)
  const sending = useSpawnSubmit(openKey, onSubmit)
  const { loading, error } = sending

  const formRef = useRef<HTMLFormElement>(null)
  useModalLayer({ open, onClose, containerRef: formRef, dismissible: !loading })
  const listId = useId()

  const clone = useCloneChoice({ openKey, baseUrl, taskId, defaultBranch, settings: settings.data, settingsRef })
  const { inClone, repository: cloneRepository } = clone
  const dir = useDirectoryField({ openKey, baseUrl, initialWorkingDir, settingsRef, folder: clone.whereKind === "folder" })
  const { path, selected } = dir
  const engram = useEngramChoice({
    open, openKey, baseUrl, taskId, adapter: effectiveAdapter,
    dir: inClone ? "" : path.trim(),
    repo: inClone ? cloneRepository?.repo ?? "" : "",
  })

  useEffect(() => {
    if (!openKey) return
    setTitle("")
    setUnsandboxed(false)
    setHostNetwork(false)
    setPrompt("")
  }, [openKey])

  const where = whereName(inClone, cloneRepository?.name, selected, path)
  const canSave = canSavePath(inClone, dir.serverKnowsDirectories, path, selected)

  const submit = () => {
    if (loading || !ready) return
    const savePreset = dir.savePath && canSave ? () => saveDirectoryPreset(baseUrl, spawnWorkingDir({ inClone, path })) : null
    void sending.run(savePreset, () => buildSpawnRequest({
      title, agentKind, effectiveAdapter, account, path, inClone, jobClone: clone.jobClone, cloneRepo: cloneRepository?.repo,
      branch: clone.branch, baseBranch: clone.baseBranch, offersSessionChoice, sessionPolicy, resources: resources.picked,
      hooksOff: managedHooks.off, unsandboxed, hostNetworkOffered, hostNetwork, prompt, tier: macro.tier,
      engramShown: engram.shown, engram: engram.engram, canRemember: engram.canRemember, rememberEngram: engram.remember,
    }))
  }

  if (!open) return null

  // A dialog that is starting an agent stays until that has an answer.
  const close = () => { if (!loading) onClose() }

  const customPathShown = !inClone && (dir.showAll || dir.presets.length === 0)

  return (
    <SpawnSheet name={name} formRef={formRef} onStart={submit} onClose={close}>
      {/* The sheet's handle on a phone, the heading, and the close button,
          which waits while an agent is being started. */}
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
        <ErrorNote message={clone.jobClonesError} />
        <ErrorNote message={error} />

        {titled && (
          <label className="flex flex-col gap-1.5">
            <span className={fieldLabel}>Title</span>
            <input
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

        <AgentField agent={agent} />

        {agent.accountsShown && <AccountField agent={agent} />}

        {engram.shown && <EngramField engram={engram} />}

        {offersSessionChoice && <SessionPolicyField agent={agent} name={`${listId}-session`} />}

        <WhereField dir={dir} clone={clone} listId={listId} customPathShown={customPathShown} layoutKey={JSON.stringify([error, offersSessionChoice])} />
        {resources.resources.length > 0 && <ResourcesField choice={resources} />}
        <ManagedHooksField choice={managedHooks} />
        {macro.macros.favorites.length + macro.macros.rest.length > 0 && <MacroField macro={macro} />}
        {/* The agent's first message, sent once it is ready. */}
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
        <SandboxToggles
          sandboxed={sandboxed}
          unsandboxed={unsandboxed}
          setUnsandboxed={setUnsandboxed}
          hostNetworkOffered={hostNetworkOffered}
          hostNetwork={hostNetwork}
          setHostNetwork={setHostNetwork}
        />
      </div>

      <SpawnFooter
        action={action}
        busyLabel={ready ? acting : "Loading…"}
        where={where}
        busy={loading || !ready}
        loading={loading}
        onClose={close}
        saveVisibility={pathVisibility(inClone, customPathShown, "flex")}
        canSave={canSave}
        savePath={dir.savePath}
        setSavePath={dir.setSavePath}
      />
    </SpawnSheet>
  )
}
