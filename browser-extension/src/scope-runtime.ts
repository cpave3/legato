// Scope in the service worker: the toolbar button, the script registrations,
// and the messages of the options page and the content script.
import { LegatoClient, type LiveSocketLike } from "./legato"
import { hasOriginPermission } from "./permissions"
import { handleScopeAction, type ScopeActionResult } from "./scope-action"
import { createScopeLive } from "./scope-live"
import { scopeOrigins } from "./scope-origins"
import { cropScreenshot } from "./scope-screenshot"
import { createScopeMessageHandler } from "./scope-service"
import { loadConfig } from "./storage"
import { hostPatternFor, normalizeTicketProviders, TICKET_PROVIDERS_KEY } from "./ticket-providers"

// An origin's access is given back on Remove, unless the extension still
// needs it for Legato itself or for a ticket site.
async function releasePermission(origin: string): Promise<void> {
  const config = await loadConfig().catch(() => undefined)
  if (config && new URL(config.baseUrl).origin === origin) return
  const providers = normalizeTicketProviders((await chrome.storage.local.get(TICKET_PROVIDERS_KEY))[TICKET_PROVIDERS_KEY])
  if (providers.some((provider) => provider.web_origin === origin)) return
  await chrome.permissions.remove({ origins: [hostPatternFor(origin)] }).catch(() => false)
}

// Pin mode is toggled in the tab. A tab that was open before the script was
// registered has no script yet, so it is put there first.
async function toggleInTab(tabId: number, force?: boolean): Promise<unknown> {
  const message = { type: "scopeTogglePin", force }
  try {
    return await chrome.tabs.sendMessage(tabId, message)
  } catch {
    await chrome.scripting.executeScript({ target: { tabId }, files: ["scope.js"] })
    return chrome.tabs.sendMessage(tabId, message)
  }
}

// The button says what went wrong on its badge and its tooltip.
function report(tabId: number | undefined, result: ScopeActionResult): void {
  if (tabId === undefined) return
  const problem = result.kind === "denied" ? `Chrome did not allow Scope on ${result.origin}.`
    : result.kind === "error" ? `Scope could not start on ${result.origin}: ${result.message}`
      : result.kind === "unsupported" ? "Scope works on http and https pages only."
        : ""
  void chrome.action.setBadgeText({ tabId, text: problem ? "!" : "" }).catch(() => undefined)
  void chrome.action.setTitle({ tabId, title: problem || "Legato integration · Scope: toggle pin mode" }).catch(() => undefined)
}

// The configured Legato, once the person has let the extension reach it.
async function legato(): Promise<{ baseUrl: string; token: string; client: LegatoClient }> {
  const config = await loadConfig()
  if (!(await hasOriginPermission(config.baseUrl))) throw new Error("Open extension options and grant access to the configured Legato origin")
  return { baseUrl: config.baseUrl, token: config.token, client: new LegatoClient(config.baseUrl, config.token) }
}

const live = createScopeLive({
  target: legato,
  connect: (url) => new WebSocket(url) as unknown as LiveSocketLike,
  fetchPins: async (origin) => (await legato()).client.listScopePins(origin),
  notify: (tabId, message) => chrome.tabs.sendMessage(tabId, message),
  setTimeout: (callback, ms) => setTimeout(callback, ms) as unknown as number,
  clearTimeout: (id) => clearTimeout(id),
})

export const handleScopeMessage = createScopeMessageHandler({
  get extensionUrl() { return chrome.runtime.getURL("") },
  origins: scopeOrigins,
  releasePermission,
  captureTab: (windowId) => chrome.tabs.captureVisibleTab(windowId, { format: "png" }),
  crop: cropScreenshot,
  legato: async () => {
    const { baseUrl, client } = await legato()
    return { baseUrl, api: client }
  },
  live,
})

export function startScope(): void {
  // The extension has no popup, and the side panel opens only from the
  // GitHub widgets (sidePanel.open), not from the button, so the button's
  // click comes here.
  chrome.action.onClicked.addListener((tab) => {
    void handleScopeAction(tab, {
      request: (pattern) => chrome.permissions.request({ origins: [pattern] }),
      isListed: (origin) => scopeOrigins.isAllowed(origin),
      add: (origin) => scopeOrigins.add(origin),
      toggle: toggleInTab,
    }).then((result) => report(tab.id, result))
  })
  const sync = (): void => {
    void scopeOrigins.list().catch((error: unknown) => console.warn("Legato Scope sites:", error instanceof Error ? error.message : String(error)))
  }
  chrome.runtime.onInstalled.addListener(sync)
  chrome.runtime.onStartup.addListener(sync)
  chrome.permissions.onAdded.addListener(sync)
  chrome.permissions.onRemoved.addListener(sync)
  chrome.tabs.onRemoved.addListener((tabId) => live.unwatch(tabId))
}
