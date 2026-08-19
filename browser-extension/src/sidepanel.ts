import type { ReviewPanelModel } from "./review-model"
import { createReviewPanelController } from "./sidepanel-controller"
import { renderReviewPanelView } from "./sidepanel-view"

const stateElement = document.querySelector<HTMLElement>("#state")!
const runSelect = document.querySelector<HTMLSelectElement>("#run-select")!
const details = document.querySelector<HTMLElement>("#details")!
const refreshButton = document.querySelector<HTMLButtonElement>("#refresh")!
let windowId: number | undefined
let pollTimer: number | undefined

const controller = createReviewPanelController({
  load: async (tabId, selectedRunId) => {
    let lastError = "Review lookup failed"
    for (let attempt = 0; attempt < 6; attempt++) {
      const response = await chrome.runtime.sendMessage({ type: "getReviewModel", tabId, selectedRunId }) as { ok: boolean; review?: ReviewPanelModel; error?: string }
      if (response.ok && response.review) return response.review
      lastError = response.error || lastError
      if (!lastError.includes("No GitHub pull request context") || attempt === 5) break
      await new Promise((resolve) => window.setTimeout(resolve, 100))
    }
    throw new Error(lastError)
  },
  render: (model) => {
    renderReviewPanelView(document, model)
  },
  showError: (caught) => {
    details.hidden = true
    stateElement.hidden = false
    stateElement.textContent = caught instanceof Error ? caught.message : String(caught)
  },
  schedule: (callback) => { pollTimer = window.setTimeout(callback, 4_000) },
  cancel: () => {
    if (pollTimer !== undefined) window.clearTimeout(pollTimer)
    pollTimer = undefined
  },
})

runSelect.addEventListener("change", () => { void controller.selectRun(runSelect.value) })
refreshButton.addEventListener("click", () => { void controller.refresh() })
window.addEventListener("unload", () => { if (pollTimer !== undefined) window.clearTimeout(pollTimer) })

chrome.tabs.onActivated.addListener((info) => {
  if (info.windowId === windowId) void controller.followTab(info.tabId)
})
chrome.tabs.onUpdated.addListener((updatedTabId, changeInfo, tab) => {
  if (tab.active && tab.windowId === windowId && changeInfo.url) void controller.followTab(updatedTabId)
})
chrome.runtime.onMessage.addListener((message: unknown) => {
  const update = message as { type?: string; tabId?: number }
  if (update.type === "reviewContextUpdated" && update.tabId !== undefined) void controller.contextUpdated(update.tabId)
})

void chrome.windows.getCurrent().then((currentWindow) => {
  windowId = currentWindow.id
  return chrome.tabs.query({ active: true, windowId })
}).then((tabs) => controller.followTab(tabs[0]?.id))
