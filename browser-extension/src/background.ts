import { prepareGitHubWorkflow, type GitHubPullRequestContext, type PreparedWorkflow } from "./actions"
import type { ExtensionConfig } from "./config"
import { LegatoClient, type WorkflowDiscovery, type WorkflowRunView } from "./legato"
import { hasOriginPermission } from "./permissions"
import { loadConfig } from "./storage"
import { loadWindowHistory, predecessorFor, recordActivation, saveWindowHistory } from "./tab-history"
import { handleMarkedTab, stripStreamDeckMarker } from "./tab-reuse"

type ExtensionMessage = { type: "prepareGitHubWorkflow" | "startPreparedWorkflow"; context: GitHubPullRequestContext }
type MessageSender = { tab?: { id?: number } }
type MessageResponse = { ok: true; prepared?: PreparedWorkflow; runUrl?: string } | { ok: false; error: string }

type Dependencies = {
  loadConfig: () => Promise<ExtensionConfig>
  hasPermission: (baseUrl: string) => Promise<boolean>
  discover: (config: ExtensionConfig) => Promise<WorkflowDiscovery>
  start: (config: ExtensionConfig, prepared: PreparedWorkflow) => Promise<WorkflowRunView>
  openRun: (url: string) => Promise<void>
}

const defaultDependencies: Dependencies = {
  loadConfig,
  hasPermission: hasOriginPermission,
  discover: (config) => new LegatoClient(config.baseUrl, config.token).discover(),
  start: (config, prepared) => new LegatoClient(config.baseUrl, config.token).startWorkflow(prepared.workflowName, prepared.inputs),
  openRun: async (url) => {
    const matching = await chrome.tabs.query({ url: `${new URL(url).origin}/*` })
    const existing = matching.find((tab) => tab.id !== undefined)
    if (existing?.id !== undefined) {
      await chrome.tabs.update(existing.id, { url, active: true })
      if (existing.windowId !== undefined) await chrome.windows.update(existing.windowId, { focused: true })
    } else {
      await chrome.tabs.create({ url, active: true })
    }
  },
}

function validContext(value: unknown): value is GitHubPullRequestContext {
  if (!value || typeof value !== "object") return false
  const candidate = value as Partial<GitHubPullRequestContext>
  return typeof candidate.repository === "string"
    && /^[^/]+\/[^/]+$/.test(candidate.repository)
    && Number.isSafeInteger(candidate.pullRequestNumber)
    && (candidate.pullRequestNumber ?? 0) > 0
}

export function createMessageHandler(deps: Dependencies = defaultDependencies) {
  const active = new Set<number>()
  return async (message: unknown, sender: MessageSender): Promise<MessageResponse> => {
    try {
      if (!message || typeof message !== "object") throw new Error("Unsupported extension message")
      const request = message as Partial<ExtensionMessage>
      if ((request.type !== "prepareGitHubWorkflow" && request.type !== "startPreparedWorkflow") || !validContext(request.context)) {
        throw new Error("Invalid GitHub pull request context")
      }
      const tabID = sender.tab?.id
      if (tabID === undefined) throw new Error("The action must originate from a browser tab")
      if (request.type === "startPreparedWorkflow" && active.has(tabID)) {
        return { ok: false, error: "A Legato action is already running for this tab" }
      }
      if (request.type === "startPreparedWorkflow") active.add(tabID)
      try {
        const config = await deps.loadConfig()
        if (!(await deps.hasPermission(config.baseUrl))) {
          throw new Error("Open extension options and grant access to the configured Legato origin")
        }
        const prepared = prepareGitHubWorkflow(config, request.context, await deps.discover(config))
        if (request.type === "prepareGitHubWorkflow") return { ok: true, prepared }
        const view = await deps.start(config, prepared)
        const runUrl = `${config.baseUrl}/workflows/runs/${encodeURIComponent(view.run.id)}`
        await deps.openRun(runUrl)
        return { ok: true, runUrl }
      } finally {
        if (request.type === "startPreparedWorkflow") active.delete(tabID)
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

  chrome.tabs.onActivated.addListener((activeInfo) => {
    void chrome.tabs.get(activeInfo.tabId).then(async (tab) => {
      if (!tab.url) return
      const history = await loadWindowHistory(activeInfo.windowId)
      await saveWindowHistory(activeInfo.windowId, recordActivation(history, {
        tabId: activeInfo.tabId,
        windowId: activeInfo.windowId,
        url: tab.url,
        activatedAt: Date.now(),
      }))
    }).catch(() => undefined)
  })

  chrome.tabs.onUpdated.addListener((tabId, changeInfo, tab) => {
    if (!changeInfo.url || tab.windowId === undefined || !stripStreamDeckMarker(changeInfo.url).marked) return
    const eventAt = Date.now()
    void Promise.all([loadConfig(), loadWindowHistory(tab.windowId)]).then(([config, history]) => handleMarkedTab({
      destination: changeInfo.url!,
      newTabId: tabId,
      newWindowId: tab.windowId,
      eventAt,
      configuredOrigin: new URL(config.baseUrl).origin,
      prior: predecessorFor(history, tabId),
    }, chrome.tabs)).catch((error: unknown) => {
      console.warn("Legato Stream Deck tab handoff failed open", error instanceof Error ? error.message : String(error))
    })
  })
}
