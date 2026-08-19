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
