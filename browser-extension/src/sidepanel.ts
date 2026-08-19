import type { ReviewPanelModel } from "./review-model"
import { renderReviewPanelView } from "./sidepanel-view"

const stateElement = document.querySelector<HTMLElement>("#state")!
const runSelect = document.querySelector<HTMLSelectElement>("#run-select")!
const details = document.querySelector<HTMLElement>("#details")!
const refreshButton = document.querySelector<HTMLButtonElement>("#refresh")!

let tabId: number | undefined
let selectedRunId: string | undefined
let pollTimer: number | undefined
let initialContextRetries = 0

function schedulePoll(enabled: boolean): void {
  if (pollTimer !== undefined) window.clearTimeout(pollTimer)
  pollTimer = enabled ? window.setTimeout(() => { void refresh() }, 4_000) : undefined
}

async function refresh(): Promise<void> {
  if (tabId === undefined) return
  stateElement.hidden = false
  stateElement.textContent = "Loading review…"
  try {
    const response = await chrome.runtime.sendMessage({ type: "getReviewModel", tabId, selectedRunId }) as { ok: boolean; review?: ReviewPanelModel; error?: string }
    if (!response.ok || !response.review) throw new Error(response.error || "Review lookup failed")
    initialContextRetries = 0
    selectedRunId = response.review.selectedRunId
    renderReviewPanelView(document, response.review)
    schedulePoll(response.review.poll)
  } catch (caught) {
    if (caught instanceof Error && caught.message.includes("No GitHub pull request context") && initialContextRetries < 5) {
      initialContextRetries++
      pollTimer = window.setTimeout(() => { void refresh() }, 100)
      return
    }
    initialContextRetries = 0
    schedulePoll(false)
    details.hidden = true
    stateElement.hidden = false
    stateElement.textContent = caught instanceof Error ? caught.message : String(caught)
  }
}

runSelect.addEventListener("change", () => {
  selectedRunId = runSelect.value
  void refresh()
})
refreshButton.addEventListener("click", () => { void refresh() })
window.addEventListener("unload", () => schedulePoll(false))

async function followTab(nextTabId: number | undefined): Promise<void> {
  if (nextTabId === tabId) return
  tabId = nextTabId
  selectedRunId = undefined
  schedulePoll(false)
  if (tabId === undefined) {
    details.hidden = true
    stateElement.hidden = false
    stateElement.textContent = "Open this panel from a supported GitHub pull request."
    return
  }
  await refresh()
}

chrome.tabs.onActivated.addListener((info) => { void followTab(info.tabId) })
chrome.tabs.onUpdated.addListener((updatedTabId, changeInfo, tab) => {
  if (tab.active && changeInfo.url) void followTab(updatedTabId).then(() => refresh())
})

void chrome.tabs.query({ active: true, currentWindow: true }).then((tabs) => followTab(tabs[0]?.id))
