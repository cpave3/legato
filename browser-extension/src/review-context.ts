import type { GitHubPullRequestContext } from "./actions"

const key = (tabId: number): string => `reviewContext:${tabId}`
const memory = new Map<number, GitHubPullRequestContext>()

export async function saveReviewContext(tabId: number, context: GitHubPullRequestContext): Promise<void> {
  memory.set(tabId, context)
  await chrome.storage.session.set({ [key(tabId)]: context })
}

export async function loadReviewContext(tabId: number): Promise<GitHubPullRequestContext | null> {
  const cached = memory.get(tabId)
  if (cached) return cached
  const stored = await chrome.storage.session.get(key(tabId))
  const context = (stored[key(tabId)] as GitHubPullRequestContext | undefined) ?? null
  if (context) memory.set(tabId, context)
  return context
}
