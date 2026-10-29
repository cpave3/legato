import type { GitHubPullRequestContext } from "./actions"

const key = (tabId: number): string => `reviewContext:${tabId}`
const memory = new Map<number, GitHubPullRequestContext>()

export function parseGitHubPullRequestUrl(href: string): GitHubPullRequestContext | null {
  let url: URL
  try { url = new URL(href) } catch { return null }
  if (url.protocol !== "https:" || url.hostname !== "github.com") return null
  const match = url.pathname.match(/^\/([^/]+)\/([^/]+)\/pull\/([1-9]\d*)(?:\/|$)/)
  return match ? { repository: `${match[1]}/${match[2]}`, pullRequestNumber: Number(match[3]) } : null
}

export function cacheReviewContext(tabId: number, context: GitHubPullRequestContext): void {
  memory.set(tabId, context)
}

export async function saveReviewContext(tabId: number, context: GitHubPullRequestContext): Promise<void> {
  cacheReviewContext(tabId, context)
  await chrome.storage.session.set({ [key(tabId)]: context })
}

export async function clearReviewContext(tabId: number): Promise<void> {
  memory.delete(tabId)
  await chrome.storage.session.remove(key(tabId))
}

export async function loadReviewContext(tabId: number): Promise<GitHubPullRequestContext | null> {
  const cached = memory.get(tabId)
  if (cached) return cached
  const stored = await chrome.storage.session.get(key(tabId))
  const context = (stored[key(tabId)] as GitHubPullRequestContext | undefined) ?? null
  if (context) memory.set(tabId, context)
  return context
}

/** Which part of the side panel a tab asked for last. */
export type PanelView = "review" | "discussion"
const viewKey = (tabId: number): string => `panelView:${tabId}`
const views = new Map<number, PanelView>()

export async function savePanelView(tabId: number, view: PanelView): Promise<void> {
  views.set(tabId, view)
  await chrome.storage.session.set({ [viewKey(tabId)]: view })
}

export async function loadPanelView(tabId: number): Promise<PanelView> {
  const cached = views.get(tabId)
  if (cached) return cached
  const stored = await chrome.storage.session.get(viewKey(tabId))
  return stored[viewKey(tabId)] === "discussion" ? "discussion" : "review"
}

// Text to put in the discussion's message box: what the person picked on the
// page (a file and its lines) before the panel was open to take it.
const draftKey = (tabId: number): string => `discussionDraft:${tabId}`
const drafts = new Map<number, string>()

export async function saveDiscussionDraft(tabId: number, text: string): Promise<void> {
  drafts.set(tabId, text)
  await chrome.storage.session.set({ [draftKey(tabId)]: text })
}

export async function takeDiscussionDraft(tabId: number): Promise<string | null> {
  const stored = await chrome.storage.session.get(draftKey(tabId))
  const text = drafts.get(tabId) ?? (stored[draftKey(tabId)] as string | undefined) ?? null
  drafts.delete(tabId)
  if (text !== null) await chrome.storage.session.remove(draftKey(tabId))
  return text
}
