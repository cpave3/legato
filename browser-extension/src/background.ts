import { prepareGitHubWorkflow, type GitHubPullRequestContext, type PreparedWorkflow } from "./actions"
import type { ExtensionConfig } from "./config"
import { LegatoClient, type WorkflowDiscovery, type WorkflowRunView } from "./legato"
import { hasOriginPermission } from "./permissions"
import { loadConfig } from "./storage"
import { consumeTabCreated, loadWindowHistory, predecessorFor, queueActivation, recordTabCreated } from "./tab-history"
import { handleMarkedTab, stripStreamDeckMarker } from "./tab-reuse"

type MessageSender = { tab?: { id?: number } }
type MessageResponse = { ok: true; prepared?: PreparedWorkflow; runUrl?: string; taskUrl?: string; navigationWarning?: string } | { ok: false; error: string }

type Dependencies = {
  loadConfig: () => Promise<ExtensionConfig>
  hasPermission: (baseUrl: string) => Promise<boolean>
  discover: (config: ExtensionConfig) => Promise<WorkflowDiscovery>
  start: (config: ExtensionConfig, prepared: PreparedWorkflow) => Promise<WorkflowRunView>
  openRun: (url: string) => Promise<void>
  importTicket: (config: ExtensionConfig, ticketId: string) => Promise<{ id: string }>
  openTask: (url: string) => Promise<void>
}

const defaultDependencies: Dependencies = {
  loadConfig,
  hasPermission: hasOriginPermission,
  discover: (config) => new LegatoClient(config.baseUrl, config.token).discover(),
  start: (config, prepared) => new LegatoClient(config.baseUrl, config.token).startWorkflow(prepared.workflowName, prepared.inputs),
  openRun: openLegatoUrl,
  importTicket: (config, ticketId) => new LegatoClient(config.baseUrl, config.token).importRemoteTicket(ticketId),
  openTask: openLegatoUrl,
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

export function createMessageHandler(deps: Dependencies = defaultDependencies) {
  const active = new Set<string>()
  return async (message: unknown, sender: MessageSender): Promise<MessageResponse> => {
    try {
      if (!message || typeof message !== "object") throw new Error("Unsupported extension message")
      const request = message as { type?: string; context?: unknown }
      const jira = request.type === "importJiraTicket"
      const github = request.type === "prepareGitHubWorkflow" || request.type === "startPreparedWorkflow"
      if (jira && !validJiraContext(request.context)) throw new Error("Invalid Jira ticket context")
      if (github && !validGitHubContext(request.context)) throw new Error("Invalid GitHub pull request context")
      if (!jira && !github) throw new Error("Unsupported extension message")

      const tabId = sender.tab?.id
      if (tabId === undefined) throw new Error("The action must originate from a browser tab")
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
        const prepared = prepareGitHubWorkflow(config, context, await deps.discover(config))
        if (request.type === "prepareGitHubWorkflow") return { ok: true, prepared }
        const view = await deps.start(config, prepared)
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

const handleMessage = createMessageHandler()
if (typeof chrome !== "undefined" && chrome.runtime?.onMessage) {
  chrome.runtime.onMessage.addListener((message: unknown, sender, sendResponse) => {
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
    if (!changeInfo.url || tab.windowId === undefined || !stripStreamDeckMarker(changeInfo.url).marked) return
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
