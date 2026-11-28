import type { AgentUIItem } from "./agentUi"
import type { ArtifactFocus } from "./artifactFocus"
import { briefingPathOf, briefingTabId, type ReviewBriefingPage } from "./reviewBriefing"

// What an item is saved as. A video is saved as its manifest: the file that
// says what the video shows, which is what the agent wrote.
// A studio is saved as what it holds: where its storyboard is.
const EXTENSION: Record<AgentUIItem["format"], string> = { html: "html", markdown: "md", components: "json", video: "manifest.json", studio: "studio.json" }
const MEDIA_TYPE: Record<AgentUIItem["format"], string> = { html: "text/html", markdown: "text/markdown", components: "application/json", video: "application/json", studio: "application/json" }

export function artifactFileName(item: AgentUIItem): string {
  const stem = (item.title || item.id).replace(/[^A-Za-z0-9._-]+/g, "-").replace(/^-+|-+$/g, "") || item.id
  return `${stem}.${EXTENSION[item.format]}`
}

export function artifactMediaType(item: AgentUIItem): string {
  return MEDIA_TYPE[item.format]
}

export function artifactLabel(item: AgentUIItem): string {
  return item.title || (item.placement === "panel" ? "Artifact" : "Card")
}

export interface ArtifactGroups {
  // Everything but studios, artifacts first, each group oldest to newest,
  // as the tabs read.
  ordered: AgentUIItem[]
  artifacts: AgentUIItem[]
  cards: AgentUIItem[]
  newestStudio: AgentUIItem | null
}

// A studio is a tool the person works in, beside what the agent made to be
// read. It shares the panel's column and nothing else: it is not a tab, and
// has no revisions, source or download.
export function groupArtifacts(items: AgentUIItem[]): ArtifactGroups {
  const studios = items.filter((item) => item.format === "studio")
  const made = items.filter((item) => item.format !== "studio")
  const artifacts = made.filter((item) => item.placement === "panel")
  const cards = made.filter((item) => item.placement !== "panel")
  return { ordered: [...artifacts, ...cards], artifacts, cards, newestStudio: studios[studios.length - 1] ?? null }
}

export interface ArtifactSelection {
  // A page of the branch's review briefing, read from the checkout, or the
  // item the agent showed; never both.
  briefingPage?: ReviewBriefingPage
  selected?: AgentUIItem
}

// What the panel draws: the focused item or briefing page, else the newest
// artifact, else the newest card, else the studio. The briefing's first page
// stands in only when the agent has shown no artifact and nothing is focused.
export function pickArtifact(items: AgentUIItem[], groups: ArtifactGroups, pages: ReviewBriefingPage[], focus: ArtifactFocus | null): ArtifactSelection {
  const focusedItem = items.find((item) => item.id === focus?.itemId)
  const focusedPage = pages.find((page) => page.path === briefingPathOf(focus?.itemId))
  const fallbackPage = !focusedItem && groups.artifacts.length === 0 ? pages[0] : undefined
  const briefingPage = focusedPage ?? fallbackPage
  if (briefingPage) return { briefingPage }
  const newest = groups.artifacts[groups.artifacts.length - 1] ?? groups.ordered[groups.ordered.length - 1]
  return { selected: focusedItem ?? newest ?? groups.newestStudio ?? undefined }
}

export interface RevisionWindow {
  revision: number
  latest: number
  oldest: number
  // How many revisions are still stored.
  available: number
}

// Where the revision being read stands among those the server keeps.
export function revisionWindow(selected: AgentUIItem, loaded: AgentUIItem | null): RevisionWindow {
  const latest = selected.revision
  const oldest = selected.oldest_revision ?? 1
  return { revision: loaded?.revision ?? latest, latest, oldest, available: Math.max(1, latest - oldest + 1) }
}

// Which revision Next goes to: 0, the newest, once it reaches the newest, so
// the panel keeps following the agent's revisions from there.
export function nextRevision(revision: number, latest: number): number {
  return revision + 1 >= latest ? 0 : revision + 1
}

// The revision of the selected item to read: the one in focus when the focus
// is on it, else 0, the newest.
export function pinnedRevision(selected: AgentUIItem | undefined, focus: ArtifactFocus | null): number {
  return selected && focus?.itemId === selected.id ? focus.revision : 0
}

// The ID of the tab in front: the briefing page's, else the item's.
export function activeTabOf({ briefingPage, selected }: ArtifactSelection): string | undefined {
  return briefingPage ? briefingTabId(briefingPage.path) : selected?.id
}
