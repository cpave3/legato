import { prepareGitHubWorkflow, type GitHubPullRequestContext, type PreparedWorkflow, type PullRequestPurpose } from "./actions"
import type { ExtensionConfig } from "./config"
import { LegatoClient, type WorkflowDiscovery, type WorkflowRun, type WorkflowRunView } from "./legato"
import { hasOriginPermission } from "./permissions"
import { cacheReviewContext, clearReviewContext, loadReviewContext, parseGitHubPullRequestUrl, saveDiscussionDraft, savePanelView, saveReviewContext, type PanelView } from "./review-context"
import type { DiscussionRef } from "./discussion-panel"
import { loadReviewModel, type ReviewPanelModel } from "./review-model"
import { loadConfig } from "./storage"
import { consumeTabCreated, loadWindowHistory, predecessorFor, queueActivation, recordTabCreated } from "./tab-history"
import { handleMarkedTab, stripStreamDeckMarker } from "./tab-reuse"

type MessageSender = { tab?: { id?: number } }
type MessageResponse = {
  ok: true; prepared?: PreparedWorkflow; runUrl?: string; agentUrl?: string; taskUrl?: string
  navigationWarning?: string; review?: ReviewPanelModel; discussion?: DiscussionRef | null
} | { ok: false; error: string }

type Dependencies = {
  loadConfig: () => Promise<ExtensionConfig>
  hasPermission: (baseUrl: string) => Promise<boolean>
  discover: (config: ExtensionConfig) => Promise<WorkflowDiscovery>
  start: (config: ExtensionConfig, prepared: PreparedWorkflow) => Promise<WorkflowRunView>
  openRun: (url: string) => Promise<void>
  importTicket: (config: ExtensionConfig, ticketId: string) => Promise<{ id: string }>
  openTask: (url: string) => Promise<void>
  loadReviewModel: (tabId: number, selectedRunId?: string) => Promise<ReviewPanelModel>
  getTabUrl: (tabId: number) => Promise<string | undefined>
  saveReviewContext: (tabId: number, context: GitHubPullRequestContext) => Promise<void>
  listRuns: (config: ExtensionConfig, workflow: string, inputs: Record<string, string>) => Promise<WorkflowRun[]>
}

const defaultDependencies: Dependencies = {
  loadConfig,
  hasPermission: hasOriginPermission,
  discover: (config) => new LegatoClient(config.baseUrl, config.token).discover(),
  start: (config, prepared) => new LegatoClient(config.baseUrl, config.token).startWorkflow(prepared.workflowName, prepared.inputs),
  openRun: openLegatoUrl,
  importTicket: (config, ticketId) => new LegatoClient(config.baseUrl, config.token).importRemoteTicket(ticketId),
  openTask: openLegatoUrl,
  loadReviewModel,
  getTabUrl: async (tabId) => (await chrome.tabs.get(tabId)).url,
  saveReviewContext,
  listRuns: (config, workflow, inputs) => new LegatoClient(config.baseUrl, config.token).listWorkflowRuns(workflow, inputs),
}

async function openLegatoUrl(url: string): Promise<void> {
  const matching = await chrome.tabs.query({ url: `${new URL(url).origin}/*` })
  const existing = matching.find((tab) => tab.id !== undefined)
  if (existing?.id !== undefined) {
    await chrome.tabs.update(existing.id, { url, active: true })
    if (existing.windowId !== undefined) await chrome.windows.update(existing.windowId, { focused: true })
  } else {
    await chrome.tabs.create({ url, active: true })
  }
}

function validGitHubContext(value: unknown): value is GitHubPullRequestContext {
  if (!value || typeof value !== "object") return false
  const candidate = value as Partial<GitHubPullRequestContext>
  return typeof candidate.repository === "string"
    && /^[^/]+\/[^/]+$/.test(candidate.repository)
    && Number.isSafeInteger(candidate.pullRequestNumber)
    && (candidate.pullRequestNumber ?? 0) > 0
}

function validJiraContext(value: unknown): value is { ticketId: string } {
  if (!value || typeof value !== "object") return false
  return typeof (value as { ticketId?: unknown }).ticketId === "string"
    && /^[A-Z][A-Z0-9_]*-\d+$/.test((value as { ticketId: string }).ticketId)
}

const pullRequestMessages = new Set([
  "prepareGitHubWorkflow", "startPreparedWorkflow", "detectGitHubReview", "detectGitHubDiscussion",
])

function purposeOf(value: unknown): PullRequestPurpose {
  if (value === undefined || value === "review") return "review"
  if (value === "discuss") return "discuss"
  throw new Error("Unsupported pull request action")
}

function agentUrlFor(config: ExtensionConfig, taskId: string): string {
  return `${config.baseUrl}/agents?task=${encodeURIComponent(taskId)}`
}

// The newest discussion of this pull request whose run has not ended. Its
// agent lives as long as the run waits on it.
// A finished review whose agent is still running comes first: that agent has
// read the change set closely already.
async function findDiscussion(deps: Dependencies, config: ExtensionConfig, context: GitHubPullRequestContext): Promise<DiscussionRef | null> {
  const discovery = await deps.discover(config)
  if (discovery.definitions?.some((definition) => definition.name === config.workflowName)) {
    const review = prepareGitHubWorkflow(config, context, discovery, "review")
    const reviewed = (await deps.listRuns(config, review.workflowName, review.inputs)).find(waitsOnItsAgent)
    if (reviewed) return discussionRef(config, reviewed)
  }
  const prepared = prepareGitHubWorkflow(config, context, discovery, "discuss")
  const runs = await deps.listRuns(config, prepared.workflowName, prepared.inputs)
  const live = runs.find((run) => (run.status === "running" || run.status === "waiting") && run.task_id)
  return live ? discussionRef(config, live) : null
}

function discussionRef(config: ExtensionConfig, run: WorkflowRun): DiscussionRef {
  return { runId: run.id, taskId: run.task_id!, agentUrl: agentUrlFor(config, run.task_id!) }
}

// Whether a run is paused on a step that waits for its agent to end, which
// means the agent is still running.
function waitsOnItsAgent(run: WorkflowRun): boolean {
  if (run.status !== "waiting" || !run.task_id || !run.definition_json) return false
  try {
    const definition = JSON.parse(run.definition_json) as { steps?: Array<{ id?: string; trigger?: string }> }
    return definition.steps?.find((step) => step.id === run.current_step_id)?.trigger === "agent_ended"
  } catch {
    return false
  }
}

export function createMessageHandler(deps: Dependencies = defaultDependencies) {
  const active = new Set<string>()
  return async (message: unknown, sender: MessageSender): Promise<MessageResponse> => {
    try {
      if (!message || typeof message !== "object") throw new Error("Unsupported extension message")
      const request = message as { type?: string; context?: unknown; tabId?: unknown; selectedRunId?: unknown; purpose?: unknown }
      if (request.type === "getReviewModel") {
        if (!Number.isSafeInteger(request.tabId)) throw new Error("A valid GitHub tab is required")
        if (request.selectedRunId !== undefined && typeof request.selectedRunId !== "string") throw new Error("Invalid selected review run")
        const tabId = request.tabId as number
        const context = parseGitHubPullRequestUrl((await deps.getTabUrl(tabId)) ?? "")
        if (!context) throw new Error("No GitHub pull request context is available for this tab")
        await deps.saveReviewContext(tabId, context)
        return { ok: true, review: await deps.loadReviewModel(tabId, request.selectedRunId as string | undefined) }
      }
      if (request.type === "getDiscussion") {
        if (!Number.isSafeInteger(request.tabId)) throw new Error("A valid GitHub tab is required")
        const context = parseGitHubPullRequestUrl((await deps.getTabUrl(request.tabId as number)) ?? "")
        if (!context) throw new Error("No GitHub pull request context is available for this tab")
        const config = await deps.loadConfig()
        if (!(await deps.hasPermission(config.baseUrl))) throw new Error("Open extension options and grant access to the configured Legato origin")
        return { ok: true, discussion: await findDiscussion(deps, config, context) }
      }
      const jira = request.type === "importJiraTicket"
      const github = pullRequestMessages.has(request.type ?? "")
      if (jira && !validJiraContext(request.context)) throw new Error("Invalid Jira ticket context")
      if (github && !validGitHubContext(request.context)) throw new Error("Invalid GitHub pull request context")
      if (!jira && !github) throw new Error("Unsupported extension message")
      const purpose = purposeOf(request.purpose)

      const tabId = sender.tab?.id
      if (tabId === undefined) throw new Error("The action must originate from a browser tab")
      if (request.type === "detectGitHubReview") {
        const context = request.context as GitHubPullRequestContext
        await deps.saveReviewContext(tabId, context)
        return { ok: true, review: await deps.loadReviewModel(tabId) }
      }
      const mutates = request.type === "startPreparedWorkflow" || jira
      const actionKey = jira ? `jira:${(request.context as { ticketId: string }).ticketId}` : `tab:${tabId}`
      if (mutates && active.has(actionKey)) return { ok: false, error: jira ? "This Jira ticket is already being imported" : "A Legato action is already running for this tab" }
      if (mutates) active.add(actionKey)
      try {
        const config = await deps.loadConfig()
        if (!(await deps.hasPermission(config.baseUrl))) {
          throw new Error("Open extension options and grant access to the configured Legato origin")
        }
        if (jira) {
          const context = request.context as { ticketId: string }
          const task = await deps.importTicket(config, context.ticketId)
          const taskUrl = `${config.baseUrl}/board?highlight_task=${encodeURIComponent(task.id)}`
          try {
            await deps.openTask(taskUrl)
            return { ok: true, taskUrl }
          } catch {
            return { ok: true, taskUrl, navigationWarning: "Ticket imported, but Chrome could not open the task. Open Legato to view it." }
          }
        }

        const context = request.context as GitHubPullRequestContext
        if (request.type === "detectGitHubDiscussion") return { ok: true, discussion: await findDiscussion(deps, config, context) }
        const prepared = prepareGitHubWorkflow(config, context, await deps.discover(config), purpose)
        if (request.type === "prepareGitHubWorkflow") return { ok: true, prepared }
        const view = await deps.start(config, prepared)
        // A discussion is followed in the side panel, which the widget opened
        // when the person confirmed; the PR stays in front.
        if (purpose === "discuss" && view.run.task_id) return { ok: true, agentUrl: agentUrlFor(config, view.run.task_id) }
        const runUrl = `${config.baseUrl}/workflows/runs/${encodeURIComponent(view.run.id)}`
        try {
          await deps.openRun(runUrl)
          return { ok: true, runUrl }
        } catch {
          return { ok: true, runUrl, navigationWarning: "Workflow started, but Chrome could not open the run. Open Legato to view it." }
        }
      } finally {
        if (mutates) active.delete(actionKey)
      }
    } catch (error) {
      return { ok: false, error: error instanceof Error ? error.message : String(error) }
    }
  }
}

export function openReviewPanelFromGesture(
  tabId: number,
  context: GitHubPullRequestContext,
  effects: {
    cache: (tabId: number, context: GitHubPullRequestContext) => void
    persist: (tabId: number, context: GitHubPullRequestContext) => Promise<void>
    open: (tabId: number) => Promise<void>
    notify: (tabId: number) => void
  },
  // Opening a panel that is already open closes it in Chrome, so an open
  // panel is only told what changed.
  options: { alreadyOpen?: boolean } = {},
): Promise<void> {
  effects.cache(tabId, context)
  const opening = options.alreadyOpen ? Promise.resolve() : effects.open(tabId)
  return effects.persist(tabId, context).then(() => {
    effects.notify(tabId)
    return opening
  })
}

// Which windows have the side panel open: each open panel holds a port named
// for its window. A count, since a window may briefly have two while one
// reloads.
export function createPanelTracker() {
  const open = new Map<number, number>()
  return {
    opened: (windowId: number): (() => void) => {
      open.set(windowId, (open.get(windowId) ?? 0) + 1)
      let closed = false
      return () => {
        if (closed) return
        closed = true
        const left = (open.get(windowId) ?? 1) - 1
        if (left > 0) open.set(windowId, left)
        else open.delete(windowId)
      }
    },
    isOpen: (windowId: number | undefined): boolean => windowId !== undefined && open.has(windowId),
  }
}

const panels = createPanelTracker()
const handleMessage = createMessageHandler()
if (typeof chrome !== "undefined" && chrome.runtime?.onMessage) {
  chrome.runtime.onConnect.addListener((port) => {
    const match = port.name.match(/^sidepanel:(\d+)$/)
    if (!match) return
    port.onDisconnect.addListener(panels.opened(Number(match[1])))
  })

  chrome.runtime.onMessage.addListener((message: unknown, sender, sendResponse) => {
    const request = message as { type?: string; context?: unknown; view?: unknown; draft?: unknown }
    if (request.type !== "openReviewPanel") return false
    const view: PanelView = request.view === "discussion" ? "discussion" : "review"
    const draft = typeof request.draft === "string" && request.draft.length <= 2_000 ? request.draft : undefined
    const tabId = sender.tab?.id
    if (tabId === undefined || !validGitHubContext(request.context)) {
      sendResponse({ ok: false, error: "A valid GitHub pull request tab is required" })
      return false
    }
    const context = request.context
    void openReviewPanelFromGesture(tabId, context, {
      cache: cacheReviewContext,
      persist: (id, saved) => Promise.all([
        saveReviewContext(id, saved), savePanelView(id, view), draft ? saveDiscussionDraft(id, draft) : undefined,
      ]).then(() => undefined),
      open: (id) => chrome.sidePanel.open({ tabId: id }),
      notify: (id) => { void chrome.runtime.sendMessage({ type: "reviewContextUpdated", tabId: id }).catch(() => undefined) },
    }, { alreadyOpen: panels.isOpen(sender.tab?.windowId) }).then(() => sendResponse({ ok: true })).catch((error: unknown) => {
      sendResponse({ ok: false, error: error instanceof Error ? error.message : String(error) })
    })
    return true
  })

  chrome.runtime.onMessage.addListener((message: unknown, sender, sendResponse) => {
    if ((message as { type?: string }).type === "openReviewPanel") return false
    void handleMessage(message, sender).then(sendResponse)
    return true
  })

  chrome.tabs.onCreated.addListener((tab) => {
    if (tab.id !== undefined) void recordTabCreated(tab.id, Date.now())
  })

  chrome.tabs.onActivated.addListener((activeInfo) => {
    const activatedAt = Date.now()
    void chrome.tabs.get(activeInfo.tabId).then(async (tab) => {
      if (!tab.url) return
      await queueActivation({
        tabId: activeInfo.tabId,
        windowId: activeInfo.windowId,
        url: tab.url,
        activatedAt,
      })
    }).catch(() => undefined)
  })

  chrome.tabs.onUpdated.addListener((tabId, changeInfo, tab) => {
    if (!changeInfo.url || tab.windowId === undefined) return
    void loadReviewContext(tabId).then((stored) => {
      if (!stored) return
      const next = parseGitHubPullRequestUrl(changeInfo.url!)
      if (!next || next.repository.toLowerCase() !== stored.repository.toLowerCase() || next.pullRequestNumber !== stored.pullRequestNumber) {
        void clearReviewContext(tabId).then(() => { void chrome.runtime.sendMessage({ type: "reviewContextUpdated", tabId }).catch(() => undefined) })
      }
    })
    if (!stripStreamDeckMarker(changeInfo.url).marked) return
    const fallbackCreatedAt = Date.now()
    void Promise.all([
      loadConfig(),
      loadWindowHistory(tab.windowId),
      consumeTabCreated(tabId, fallbackCreatedAt),
      chrome.windows.get(tab.windowId),
    ]).then(([config, history, createdAt, window]) => handleMarkedTab({
      destination: changeInfo.url!,
      newTabId: tabId,
      newWindowId: tab.windowId,
      createdAt,
      normalWindow: window.type === "normal",
      configuredOrigin: new URL(config.baseUrl).origin,
      prior: predecessorFor(history, tabId),
    }, chrome.tabs)).catch((error: unknown) => {
      console.warn("Legato Stream Deck tab handoff failed open", error instanceof Error ? error.message : String(error))
    })
  })
}
