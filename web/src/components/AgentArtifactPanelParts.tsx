import type { KeyboardEvent as ReactKeyboardEvent } from "react"
import { ChevronLeft, ChevronRight, ClipboardCheck, Copy, Download, LayoutGrid, Mic, PanelRight, Trash2, X } from "lucide-react"
import type { AgentUIItem } from "../lib/agentUi"
import { artifactLabel, nextRevision, revisionWindow, type ArtifactGroups } from "../lib/artifactPanel"
import { briefingTabId, briefingTabLabel, type ReviewBriefingPage } from "../lib/reviewBriefing"
import { AgentUIItemView } from "./AgentUIItemView"
import { ReviewBriefingView } from "./ReviewBriefingView"
import { LoadingState } from "./ui/LoadingState"

type ArtifactTab = AgentUIItem | ReviewBriefingPage

function artifactTabKey(entry: ArtifactTab): string {
  return "path" in entry ? briefingTabId(entry.path) : entry.id
}

const tabClass = (active: boolean) => `flex min-h-9 max-w-[14rem] shrink-0 items-center gap-1.5 rounded px-2.5 py-1 text-xs transition-colors ${active ? "bg-zinc-800 text-zinc-100" : "text-zinc-400 hover:bg-zinc-900 hover:text-zinc-200"}`

// The index a roving-focus key moves to among count tabs, or null for a key
// that does not move.
function rovedIndex(key: string, index: number, count: number): number | null {
  switch (key) {
    case "ArrowRight": return (index + 1) % count
    case "ArrowLeft": return (index + count - 1) % count
    case "Home": return 0
    case "End": return count - 1
    default: return null
  }
}

interface ArtifactTabListProps {
  tabs: ArtifactTab[]
  pages: ReviewBriefingPage[]
  activeTab: string | undefined
  tabId: (id: string) => string
  panelId: string
  onActivate: (id: string) => void
}

// The tabs of what the agent showed and the branch's briefing pages, with
// one tab in the focus order and the arrow keys moving between them.
function ArtifactTabList({ tabs, pages, activeTab, tabId, panelId, onActivate }: ArtifactTabListProps) {
  function navigate(event: ReactKeyboardEvent<HTMLButtonElement>, index: number) {
    const next = rovedIndex(event.key, index, tabs.length)
    if (next === null) return
    event.preventDefault()
    const id = artifactTabKey(tabs[next])
    document.getElementById(tabId(id))?.focus()
    onActivate(id)
  }
  return (
    <div role="tablist" aria-label="Shown by the agent" className="flex min-w-0 flex-1 gap-1 overflow-x-auto py-1.5">
      {tabs.map((entry, index) => {
        const id = artifactTabKey(entry)
        const active = id === activeTab
        return (
          <button
            key={"path" in entry ? entry.path : entry.id}
            role="tab"
            type="button"
            aria-selected={active}
            id={tabId(id)}
            aria-controls={panelId}
            tabIndex={active ? 0 : -1}
            onKeyDown={(event) => navigate(event, index)}
            title={"path" in entry ? entry.path : undefined}
            onClick={() => onActivate(id)}
            className={tabClass(active)}
          >
            {"path" in entry ? <BriefingTabContent page={entry} pages={pages} /> : <ItemTabContent item={entry} />}
          </button>
        )
      })}
    </div>
  )
}

function BriefingTabContent({ page, pages }: { page: ReviewBriefingPage; pages: ReviewBriefingPage[] }) {
  return (
    <>
      <ClipboardCheck size={13} aria-hidden="true" className="shrink-0" />
      <span className="truncate">{briefingTabLabel(page, pages)}</span>
    </>
  )
}

function ItemTabContent({ item }: { item: AgentUIItem }) {
  const Icon = item.placement === "panel" ? PanelRight : LayoutGrid
  return (
    <>
      <Icon size={13} aria-hidden="true" className="shrink-0" />
      <span className="truncate">{artifactLabel(item)}</span>
      {item.revision > 1 && <span className="shrink-0 rounded bg-zinc-700/70 px-1 text-[10px] text-zinc-300">r{item.revision}</span>}
    </>
  )
}

interface ArtifactPanelHeaderProps {
  studio: boolean
  title: string
  selected: AgentUIItem | undefined
  groups: ArtifactGroups
  tabs: ArtifactTabListProps
  onFocus: (itemId: string, revision: number) => void
  onClose: () => void
}

// The panel's header: its name, then the studio's title or the tabs, and
// the way between the studio and what the agent made.
export function ArtifactPanelHeader({ studio, title, selected, groups, tabs, onFocus, onClose }: ArtifactPanelHeaderProps) {
  const { ordered, artifacts, newestStudio } = groups
  return (
    <div className="flex shrink-0 items-center gap-2 border-b border-zinc-800 pl-3 pr-1">
      <h2 className="flex shrink-0 items-center gap-1.5 text-sm font-semibold text-zinc-200">
        {studio && <Mic size={14} aria-hidden="true" className="text-indigo-300" />}
        {title}
      </h2>
      {studio ? (
        <StudioTitle title={selected?.title} made={ordered.length} onOpen={() => { const last = artifacts[artifacts.length - 1] ?? ordered[ordered.length - 1]; onFocus(last.id, 0) }} />
      ) : (
        <ArtifactTabList {...tabs} />
      )}
      {!studio && newestStudio && <StudioButton onOpen={() => onFocus(newestStudio.id, 0)} />}
      <button
        type="button"
        aria-label={studio ? "Close studio" : "Close artifacts"}
        onClick={onClose}
        className="flex h-11 w-11 shrink-0 items-center justify-center rounded text-zinc-400 transition-colors hover:bg-zinc-900 hover:text-zinc-200"
      >
        <X size={18} />
      </button>
    </div>
  )
}

// The header of a studio: its title, and a way back to what the agent made.
function StudioTitle({ title, made, onOpen }: { title: string | undefined; made: number; onOpen: () => void }) {
  return (
    <>
      <span className="min-w-0 flex-1 truncate py-3 text-xs text-zinc-400">{title}</span>
      {made > 0 && (
        <button
          type="button"
          aria-label={`Artifacts (${made})`}
          title="What this agent has shown"
          onClick={onOpen}
          className="flex min-h-9 shrink-0 items-center gap-1.5 rounded px-2.5 py-1 text-xs text-zinc-400 transition-colors hover:bg-zinc-900 hover:text-zinc-200"
        >
          <PanelRight size={13} aria-hidden="true" />
          Artifacts <span className="tabular-nums">{made}</span>
        </button>
      )}
    </>
  )
}

interface ArtifactBodyProps {
  agentId: string
  briefingPage: ReviewBriefingPage | undefined
  selected: AgentUIItem | undefined
  item: AgentUIItem | null
  listLoading: boolean
  listError: string | null
  itemLoading: boolean
  itemError: unknown
  onFocus: (itemId: string, revision: number) => void
  onProposeMessage: (text: string) => void
}

// What fills the panel: a briefing page, the item in focus, or why neither
// can be drawn yet.
export function ArtifactBody({ agentId, briefingPage, selected, item, listLoading, listError, itemLoading, itemError, onFocus, onProposeMessage }: ArtifactBodyProps) {
  if (briefingPage) {
    return <ReviewBriefingView key={briefingPage.path} agentId={agentId} page={briefingPage} onOpenPage={(path) => onFocus(briefingTabId(path), 0)} />
  }
  if (listError) return <p role="alert" className="p-4 text-sm text-red-300">Could not load what the agent showed: {listError}</p>
  if (!selected) {
    return listLoading ? <LoadingState label="Loading what the agent showed…" /> : <p className="p-4 text-sm text-zinc-500">This agent has not shown anything yet.</p>
  }
  if (itemError) return <p role="alert" className="p-4 text-sm text-zinc-400">This revision is no longer stored.</p>
  if (itemLoading || !item) return <div role="status" aria-label="Loading" className="m-4 h-24 animate-pulse rounded-lg bg-zinc-800/60" />
  // Keyed so a different item or revision starts from a fresh frame. A
  // studio is kept through its revisions: one is only the agent turning it
  // to another scene, and a fresh start would drop the microphone and the
  // person's place.
  return <AgentUIItemView key={item.format === "studio" ? item.id : `${item.id}:${item.revision}`} item={item} fill onProposeMessage={onProposeMessage} />
}

export function BriefingFooter({ page, branch }: { page: ReviewBriefingPage; branch: string | undefined }) {
  return (
    <div className="flex shrink-0 items-center gap-1.5 border-t border-zinc-800 px-3 py-2.5">
      <span className="min-w-0 truncate text-xs text-zinc-400" title={page.path}>
        {page.path}{branch ? ` on ${branch}` : ""}
      </span>
    </div>
  )
}

const toolButton = "flex min-h-9 items-center gap-1.5 rounded border border-zinc-700 px-2.5 py-1 text-xs text-zinc-300 transition-colors hover:bg-zinc-800 hover:text-zinc-100 disabled:cursor-not-allowed disabled:opacity-40"
const stepButton = "flex h-9 w-9 items-center justify-center rounded text-zinc-400 transition-colors hover:bg-zinc-800 hover:text-zinc-100 disabled:cursor-not-allowed disabled:opacity-30"

interface RevisionBarProps {
  selected: AgentUIItem
  item: AgentUIItem | null
  busy: boolean
  onFocus: (itemId: string, revision: number) => void
  onCopy: () => void
  onDownload: () => void
  onRemove: () => void
}

// The foot of an artifact: stepping through its revisions, and copying,
// downloading or removing it.
export function RevisionBar({ selected, item, busy, onFocus, onCopy, onDownload, onRemove }: RevisionBarProps) {
  const { revision, latest, oldest, available } = revisionWindow(selected, item)
  const noContent = !item?.content
  return (
    <div className="flex shrink-0 flex-wrap items-center gap-1.5 border-t border-zinc-800 px-2 py-1.5">
      <button type="button" aria-label="Previous revision" disabled={revision <= oldest} onClick={() => onFocus(selected.id, revision - 1)} className={stepButton}>
        <ChevronLeft size={16} />
      </button>
      <span className="text-xs tabular-nums text-zinc-400">Revision {revision} of {latest}</span>
      <button type="button" aria-label="Next revision" disabled={revision >= latest} onClick={() => onFocus(selected.id, nextRevision(revision, latest))} className={stepButton}>
        <ChevronRight size={16} />
      </button>
      {available < latest && <span className="text-[11px] text-zinc-600">{available} kept</span>}
      <div className="ml-auto flex items-center gap-1.5">
        <button type="button" aria-label="Copy source" title="Copy source" disabled={noContent} onClick={onCopy} className={toolButton}>
          <Copy size={13} /><span className="hidden sm:inline">Copy source</span>
        </button>
        <button type="button" aria-label="Download" title="Download" disabled={noContent || busy} onClick={onDownload} className={toolButton}>
          <Download size={13} /><span className="hidden sm:inline">Download</span>
        </button>
        <button type="button" aria-label="Remove" title="Remove" disabled={busy} onClick={onRemove} className={toolButton}>
          <Trash2 size={13} />
        </button>
      </div>
    </div>
  )
}

function StudioButton({ onOpen }: { onOpen: () => void }) {
  return (
    <button
      type="button"
      aria-label="Studio"
      title="The studio, where you record"
      onClick={onOpen}
      className="flex min-h-9 shrink-0 items-center gap-1.5 rounded px-2.5 py-1 text-xs text-zinc-400 transition-colors hover:bg-zinc-900 hover:text-zinc-200"
    >
      <Mic size={13} aria-hidden="true" />
      Studio
    </button>
  )
}
