import type { GitHubPullRequestContext } from "./actions"

const key = (tabId: number): string => `reviewContext:${tabId}`
const memory = new Map<number, GitHubPullRequestContext>()

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
