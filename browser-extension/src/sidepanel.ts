import { createDiscussionPanel, lookAgainMs, type DiscussionPanelView, type DiscussionRef } from "./discussion-panel"
import { browserDependencies, createDiscussionSession, type DiscussionState } from "./discussion-session"
import { discussionStatus, renderDiscussionFeed } from "./discussion-view"
import { showInPullRequest, type JumpReply } from "./file-jump"
import type { FileReference } from "./file-references"
import { loadPanelView, savePanelView, takeDiscussionDraft, type PanelView } from "./review-context"
import type { ReviewPanelModel } from "./review-model"
import { createReviewPanelController } from "./sidepanel-controller"
import { renderReviewPanelView } from "./sidepanel-view"
import { loadConfig } from "./storage"

const stateElement = document.querySelector<HTMLElement>("#state")!
const runSelect = document.querySelector<HTMLSelectElement>("#run-select")!
const details = document.querySelector<HTMLElement>("#details")!
const refreshButton = document.querySelector<HTMLButtonElement>("#refresh")!
let windowId: number | undefined
let pollTimer: number | undefined
let view: PanelView = "review"

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
    renderReviewPanelView(document, model, { onOpenFile: openFile })
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

// The discussion: one Legato session followed over its WebSocket.
const discussionView = document.querySelector<HTMLElement>("#discussion-view")!
const reviewView = document.querySelector<HTMLElement>("#review-view")!
const tabButtons = { review: document.querySelector<HTMLButtonElement>("#tab-review")!, discussion: document.querySelector<HTMLButtonElement>("#tab-discussion")! }
const discussionState = document.querySelector<HTMLElement>("#discussion-state")!
const discussionSection = document.querySelector<HTMLElement>("#discussion")!
const discussionStatusChip = document.querySelector<HTMLElement>("#discussion-status")!
const discussionOpen = document.querySelector<HTMLAnchorElement>("#discussion-open")!
const discussionError = document.querySelector<HTMLElement>("#discussion-error")!
const feed = document.querySelector<HTMLElement>("#discussion-feed")!
const composer = document.querySelector<HTMLFormElement>("#composer")!
const composerInput = document.querySelector<HTMLTextAreaElement>("#composer-input")!
const composerSend = document.querySelector<HTMLButtonElement>("#composer-send")!
let activeSession: ReturnType<typeof createDiscussionSession> | undefined

function renderDiscussion(state: DiscussionState): void {
  const nearBottom = feed.scrollHeight - feed.scrollTop - feed.clientHeight < 60
  renderDiscussionFeed(feed, state.items, {
    onPermission: (requestId, decision) => activeSession?.resolvePermission(requestId, decision),
    onOpenFile: openFile,
  })
  const status = discussionStatus(state)
  discussionStatusChip.textContent = status.label
  discussionStatusChip.dataset.tone = status.tone
  discussionError.hidden = !state.error
  discussionError.textContent = state.error ?? ""
  composerSend.disabled = !state.connected
  composerSend.textContent = state.busy ? "Queue" : "Send"
  if (nearBottom) feed.scrollTop = feed.scrollHeight
}

const discussionPanel = createDiscussionPanel({
  find: async (tabId) => {
    const response = await chrome.runtime.sendMessage({ type: "getDiscussion", tabId }) as { ok: boolean; discussion?: DiscussionRef | null; error?: string }
    if (!response.ok) throw new Error(response.error || "Discussion lookup failed")
    return response.discussion ?? null
  },
  startSession: (ref) => {
    feed.replaceChildren()
    let stopped = false
    let session: ReturnType<typeof createDiscussionSession> | undefined
    // The session needs the Legato address and token, which only extension
    // pages may read.
    void loadConfig().then((config) => {
      if (stopped) return
      session = createDiscussionSession({ baseUrl: config.baseUrl, token: config.token, taskId: ref.taskId }, browserDependencies, renderDiscussion)
      activeSession = session
      session.start()
    }).catch((error: unknown) => showDiscussion({ kind: "error", message: error instanceof Error ? error.message : String(error) }))
    return {
      stop: () => {
        stopped = true
        session?.stop()
        if (activeSession === session) activeSession = undefined
      },
    }
  },
  show: (next) => showDiscussion(next),
  schedule: (callback) => window.setTimeout(callback, lookAgainMs),
  cancel: (id) => window.clearTimeout(id),
})

function showDiscussion(next: DiscussionPanelView): void {
  discussionSection.hidden = next.kind !== "discussion"
  discussionState.hidden = next.kind === "discussion"
  if (next.kind === "discussion") {
    discussionOpen.href = next.ref.agentUrl
    const tabId = controller.currentTab()
    if (tabId !== undefined) void takeDiscussionDraft(tabId).then((draft) => { if (draft) addToComposer(draft) })
    return
  }
  discussionState.textContent = next.kind === "looking"
    ? "Looking for a discussion…"
    : next.kind === "none"
      ? "No discussion of this pull request is running yet. Start one with Discuss PR on the page; it shows here as soon as its agent starts."
      : next.message
}

// Text picked on the page goes after what is already typed, and the cursor
// after it, so the person writes the question straight on.
function addToComposer(text: string): void {
  const current = composerInput.value.replace(/\s+$/, "")
  composerInput.value = current ? `${current}\n${text}` : text
  composerInput.focus()
  composerInput.setSelectionRange(composerInput.value.length, composerInput.value.length)
}

composer.addEventListener("submit", (event) => {
  event.preventDefault()
  const text = composerInput.value.trim()
  if (!text || !activeSession) return
  activeSession.send(text)
  composerInput.value = ""
  feed.scrollTop = feed.scrollHeight
})
composerInput.addEventListener("keydown", (event) => {
  if (event.key === "Enter" && !event.shiftKey && !event.isComposing) {
    event.preventDefault()
    composer.requestSubmit()
  }
})

function setView(next: PanelView): void {
  view = next
  reviewView.hidden = next !== "review"
  discussionView.hidden = next !== "discussion"
  for (const [name, button] of Object.entries(tabButtons)) button.setAttribute("aria-selected", String(name === next))
  const tabId = controller.currentTab()
  if (next === "discussion") void discussionPanel.follow(tabId)
  else discussionPanel.hide()
}

for (const [name, button] of Object.entries(tabButtons) as Array<[PanelView, HTMLButtonElement]>) {
  button.addEventListener("click", () => {
    setView(name)
    const tabId = controller.currentTab()
    if (tabId !== undefined) void savePanelView(tabId, name)
  })
}

async function followTab(tabId: number | undefined): Promise<void> {
  await controller.followTab(tabId)
  setView(tabId === undefined ? view : await loadPanelView(tabId))
}

chrome.tabs.onActivated.addListener((info) => {
  if (info.windowId === windowId) void followTab(info.tabId)
})
chrome.tabs.onUpdated.addListener((updatedTabId, changeInfo, tab) => {
  if (tab.active && tab.windowId === windowId && changeInfo.url) void followTab(updatedTabId)
})
chrome.runtime.onMessage.addListener((message: unknown) => {
  const update = message as { type?: string; tabId?: number }
  if (update.type === "reviewContextUpdated" && update.tabId !== undefined && update.tabId === controller.currentTab()) void followTab(update.tabId)
})
window.addEventListener("unload", () => discussionPanel.hide())

// A file the review or the agent cites, shown on the pull request's Files
// changed page in the tab the panel follows.
const jumpNotice = document.querySelector<HTMLElement>("#jump-notice")!
let jumpNoticeTimer: number | undefined

function showJumpNotice(text: string, tone: "info" | "error", sticky = false): void {
  window.clearTimeout(jumpNoticeTimer)
  jumpNotice.hidden = false
  jumpNotice.dataset.tone = tone
  jumpNotice.textContent = text
  if (!sticky) jumpNoticeTimer = window.setTimeout(() => { jumpNotice.hidden = true }, tone === "error" ? 8_000 : 5_000)
}

function showJumpReply(reply: JumpReply | undefined): void {
  if (reply?.status === "navigating") showJumpNotice(reply.message ?? "Opening Files changed…", "info", true)
  else if (!reply?.ok || reply.partial) showJumpNotice(reply?.message ?? "Could not show the file.", reply?.ok ? "info" : "error")
  else jumpNotice.hidden = true
}

async function openFile(reference: FileReference): Promise<void> {
  const tabId = controller.currentTab()
  if (tabId === undefined) return
  try {
    showJumpReply(await showInPullRequest(tabId, reference))
  } catch (error) {
    showJumpNotice(`Could not reach the pull request's tab: ${error instanceof Error ? error.message : String(error)}`, "error")
  }
}

// A jump the page finished after loading Files changed.
chrome.runtime.onMessage.addListener((message: unknown, sender) => {
  const result = message as { type?: string; result?: JumpReply }
  if (result.type === "legato:jumpResult" && sender.tab?.id === controller.currentTab()) showJumpReply(result.result)
})

void chrome.windows.getCurrent().then((currentWindow) => {
  windowId = currentWindow.id
  // Holding a port tells the background this window's panel is open.
  if (windowId !== undefined) chrome.runtime.connect({ name: `sidepanel:${windowId}` })
  return chrome.tabs.query({ active: true, windowId })
}).then((tabs) => followTab(tabs[0]?.id))
