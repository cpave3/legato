import { useEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type PointerEvent as ReactPointerEvent } from "react"
import { ChevronLeft, ChevronRight, ClipboardCheck, Copy, Download, LayoutGrid, Mic, PanelRight, Trash2, X } from "lucide-react"
import { useAgentUIItem, useAgentUIItems } from "../hooks/useAgentUI"
import { useReviewBriefing } from "../hooks/useReviewBriefing"
import { useProposeMessage } from "../hooks/useProposeMessage"
import { useServer } from "../hooks/useServer"
import { useToast } from "../hooks/useToast"
import { buildStandaloneDocument, deleteAgentUIItem, inlineImages, itemImages, type AgentUIItem } from "../lib/agentUi"
import { briefingPathOf, briefingTabId, briefingTabLabel } from "../lib/reviewBriefing"
import { AgentUIItemView } from "./AgentUIItemView"
import { ReviewBriefingView } from "./ReviewBriefingView"
import { LoadingState } from "./ui/LoadingState"

export interface ArtifactFocus {
  itemId: string
  // 0 follows the latest revision; a number pins one.
  revision: number
}

interface AgentArtifactPanelProps {
  agentId: string
  focus: ArtifactFocus | null
  onFocus: (itemId: string, revision: number) => void
  onClose: () => void
  // A phone gets the panel as a full-screen sheet; a wide screen as a
  // resizable column beside the conversation.
  mobile: boolean
}

const MIN_WIDTH_REM = 20
const MAX_WIDTH_REM = 64
const DEFAULT_WIDTH_REM = 34
const WIDTH_KEY = "legato:artifact-panel-width"

function storedWidth(): number {
  try {
    const stored = Number(localStorage.getItem(WIDTH_KEY))
    if (Number.isFinite(stored) && stored >= MIN_WIDTH_REM && stored <= MAX_WIDTH_REM) return stored
  } catch {
    // Storage can be unavailable; the default width is fine.
  }
  return DEFAULT_WIDTH_REM
}

function clampWidth(value: number): number {
  return Math.min(MAX_WIDTH_REM, Math.max(MIN_WIDTH_REM, value))
}

// What an item is saved as. A video is saved as its manifest: the file that
// says what the video shows, which is what the agent wrote.
// A studio is saved as what it holds: where its storyboard is.
const EXTENSION: Record<AgentUIItem["format"], string> = { html: "html", markdown: "md", components: "json", video: "manifest.json", studio: "studio.json" }
const MEDIA_TYPE: Record<AgentUIItem["format"], string> = { html: "text/html", markdown: "text/markdown", components: "application/json", video: "application/json", studio: "application/json" }

function fileName(item: AgentUIItem): string {
  const stem = (item.title || item.id).replace(/[^A-Za-z0-9._-]+/g, "-").replace(/^-+|-+$/g, "") || item.id
  return `${stem}.${EXTENSION[item.format]}`
}

function itemLabel(item: AgentUIItem): string {
  return item.title || (item.placement === "panel" ? "Artifact" : "Card")
}

// AgentArtifactPanel shows what one agent session asked Legato to show: its
// artifacts, and its cards for a session whose conversation Legato cannot
// draw. The agent's content is drawn by AgentUIItemView, HTML in a sandbox.
export function AgentArtifactPanel({ agentId, focus, onFocus, onClose, mobile }: AgentArtifactPanelProps) {
  const { baseUrl } = useServer()
  const toast = useToast()
  const { items, loading: listLoading, error: listError } = useAgentUIItems(agentId)
  // A studio is a tool the person works in, beside what the agent made to be
  // read. It shares this column and nothing else: it is not a tab, and has
  // no revisions, source or download.
  const studios = items.filter((item) => item.format === "studio")
  const made = items.filter((item) => item.format !== "studio")
  // Artifacts first, each group oldest to newest, as the tabs read.
  const ordered = [...made.filter((item) => item.placement === "panel"), ...made.filter((item) => item.placement !== "panel")]
  const artifacts = ordered.filter((item) => item.placement === "panel")
  const cards = ordered.filter((item) => item.placement !== "panel")
  const newestStudio = studios[studios.length - 1] ?? null
  // The review briefing on the agent's branch, if it carries one: pages the
  // pull request will show, listed after the artifacts and read from the
  // checkout rather than shown by the agent.
  const briefing = useReviewBriefing(agentId)
  const focusedItem = items.find((item) => item.id === focus?.itemId)
  const focusedPage = briefing.pages.find((page) => page.path === briefingPathOf(focus?.itemId))
  const briefingPage = focusedPage
    ?? (!focusedItem && artifacts.length === 0 ? briefing.pages[0] : undefined)
  const selected = briefingPage ? undefined : focusedItem
    ?? artifacts[artifacts.length - 1]
    ?? ordered[ordered.length - 1]
    ?? newestStudio
  const studio = selected?.format === "studio"
  const panelName = studio ? "Studio" : "Artifacts"
  const pinned = selected && focus?.itemId === selected.id ? focus.revision : 0
  const { item, loading: itemLoading, error: itemError } = useAgentUIItem(selected?.id, pinned)
  const proposeMessage = useProposeMessage(agentId)
  const [widthRem, setWidthRem] = useState(storedWidth)
  const [busy, setBusy] = useState(false)
  const containerRef = useRef<HTMLElement>(null)

  useEffect(() => {
    try {
      localStorage.setItem(WIDTH_KEY, String(widthRem))
    } catch {
      // Not remembered; nothing else depends on it.
    }
  }, [widthRem])

  // A sheet that covers the page takes the keyboard with it.
  useEffect(() => {
    if (mobile) containerRef.current?.focus()
  }, [mobile])

  function resizeFromPointer(event: ReactPointerEvent<HTMLDivElement>) {
    event.preventDefault()
    const startX = event.clientX
    const startWidth = widthRem
    const rootSize = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16
    const move = (moveEvent: PointerEvent) => setWidthRem(clampWidth(startWidth + (startX - moveEvent.clientX) / rootSize))
    const stop = () => {
      window.removeEventListener("pointermove", move)
      window.removeEventListener("pointerup", stop)
    }
    window.addEventListener("pointermove", move)
    window.addEventListener("pointerup", stop)
  }

  function resizeFromKeyboard(event: ReactKeyboardEvent<HTMLDivElement>) {
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return
    event.preventDefault()
    setWidthRem((width) => clampWidth(width + (event.key === "ArrowLeft" ? 2 : -2)))
  }

  const revision = item?.revision ?? selected?.revision ?? 0
  const latest = selected?.revision ?? 0
  const oldest = selected?.oldest_revision ?? 1
  const available = Math.max(1, latest - oldest + 1)

  async function copySource() {
    if (!item?.content) return
    try {
      await navigator.clipboard.writeText(item.content)
      toast?.addToast("Source copied", "success")
    } catch {
      toast?.addToast("The browser did not allow copying", "error")
    }
  }

  function save(item: AgentUIItem, saved: string) {
    const url = URL.createObjectURL(new Blob([saved], { type: MEDIA_TYPE[item.format] }))
    const link = document.createElement("a")
    link.href = url
    link.download = fileName(item)
    link.click()
    URL.revokeObjectURL(url)
  }

  function download() {
    if (!item?.content) return
    // Markdown and component cards are saved as the agent wrote them.
    if (item.format !== "html") {
      save(item, item.content)
      return
    }
    // HTML is saved as a whole page with Legato's theme in it: the agent's
    // styles depend on that theme, and outside Legato nothing supplies it.
    const page = (images: ReturnType<typeof itemImages>) => save(item, buildStandaloneDocument({
      html: item.content ?? "", title: itemLabel(item), resourceDomains: item.resource_domains ?? [], images,
    }))
    const images = itemImages(baseUrl, item)
    if (images.length === 0) {
      page(images)
      return
    }
    // Its image files go into the page too, so it opens where Legato is not.
    setBusy(true)
    void inlineImages(images).then(page).finally(() => setBusy(false))
  }

  async function remove() {
    if (!selected || busy) return
    if (!window.confirm(`Remove "${itemLabel(selected)}" and all of its revisions? The agent can show it again.`)) return
    setBusy(true)
    try {
      await deleteAgentUIItem(baseUrl, selected.id)
      toast?.addToast("Removed", "success")
    } catch (cause) {
      toast?.addToast(cause instanceof Error ? cause.message : "Could not remove it", "error")
    } finally {
      setBusy(false)
    }
  }

  const toolButton = "flex min-h-9 items-center gap-1.5 rounded border border-zinc-700 px-2.5 py-1 text-xs text-zinc-300 transition-colors hover:bg-zinc-800 hover:text-zinc-100 disabled:cursor-not-allowed disabled:opacity-40"
  const stepButton = "flex h-9 w-9 items-center justify-center rounded text-zinc-400 transition-colors hover:bg-zinc-800 hover:text-zinc-100 disabled:cursor-not-allowed disabled:opacity-30"

  const content = (
    <>
      <div className="flex shrink-0 items-center gap-2 border-b border-zinc-800 pl-3 pr-1">
        <h2 className="flex shrink-0 items-center gap-1.5 text-sm font-semibold text-zinc-200">
          {studio && <Mic size={14} aria-hidden="true" className="text-indigo-300" />}
          {panelName}
        </h2>
        {studio ? (
          <>
            <span className="min-w-0 flex-1 truncate py-3 text-xs text-zinc-400">{selected?.title}</span>
            {ordered.length > 0 && (
              <button
                type="button"
                aria-label={`Artifacts (${ordered.length})`}
                title="What this agent has shown"
                onClick={() => { const last = artifacts[artifacts.length - 1] ?? ordered[ordered.length - 1]; onFocus(last.id, 0) }}
                className="flex min-h-9 shrink-0 items-center gap-1.5 rounded px-2.5 py-1 text-xs text-zinc-400 transition-colors hover:bg-zinc-900 hover:text-zinc-200"
              >
                <PanelRight size={13} aria-hidden="true" />
                Artifacts <span className="tabular-nums">{ordered.length}</span>
              </button>
            )}
          </>
        ) : (
        <div role="tablist" aria-label="Shown by the agent" className="flex min-w-0 flex-1 gap-1 overflow-x-auto py-1.5">
          {[...artifacts, ...briefing.pages, ...cards].map((entry) => {
            if ("path" in entry) {
              const active = entry.path === briefingPage?.path
              return (
                <button
                  key={entry.path}
                  role="tab"
                  type="button"
                  aria-selected={active}
                  title={entry.path}
                  onClick={() => onFocus(briefingTabId(entry.path), 0)}
                  className={`flex min-h-9 max-w-[14rem] shrink-0 items-center gap-1.5 rounded px-2.5 py-1 text-xs transition-colors ${active ? "bg-zinc-800 text-zinc-100" : "text-zinc-400 hover:bg-zinc-900 hover:text-zinc-200"}`}
                >
                  <ClipboardCheck size={13} aria-hidden="true" className="shrink-0" />
                  <span className="truncate">{briefingTabLabel(entry, briefing.pages)}</span>
                </button>
              )
            }
            const active = entry.id === selected?.id
            const Icon = entry.placement === "panel" ? PanelRight : LayoutGrid
            return (
              <button
                key={entry.id}
                role="tab"
                type="button"
                aria-selected={active}
                onClick={() => onFocus(entry.id, 0)}
                className={`flex min-h-9 max-w-[14rem] shrink-0 items-center gap-1.5 rounded px-2.5 py-1 text-xs transition-colors ${active ? "bg-zinc-800 text-zinc-100" : "text-zinc-400 hover:bg-zinc-900 hover:text-zinc-200"}`}
              >
                <Icon size={13} aria-hidden="true" className="shrink-0" />
                <span className="truncate">{itemLabel(entry)}</span>
                {entry.revision > 1 && <span className="shrink-0 rounded bg-zinc-700/70 px-1 text-[10px] text-zinc-300">r{entry.revision}</span>}
              </button>
            )
          })}
        </div>
        )}
        {!studio && newestStudio && (
          <button
            type="button"
            aria-label="Studio"
            title="The studio, where you record"
            onClick={() => onFocus(newestStudio.id, 0)}
            className="flex min-h-9 shrink-0 items-center gap-1.5 rounded px-2.5 py-1 text-xs text-zinc-400 transition-colors hover:bg-zinc-900 hover:text-zinc-200"
          >
            <Mic size={13} aria-hidden="true" />
            Studio
          </button>
        )}
        <button
          type="button"
          aria-label={studio ? "Close studio" : "Close artifacts"}
          onClick={onClose}
          className="flex h-11 w-11 shrink-0 items-center justify-center rounded text-zinc-400 transition-colors hover:bg-zinc-900 hover:text-zinc-200"
        >
          <X size={18} />
        </button>
      </div>

      <div className="flex min-h-0 flex-1 flex-col bg-zinc-900">
        {briefingPage ? (
          <ReviewBriefingView key={briefingPage.path} agentId={agentId} page={briefingPage} onOpenPage={(path) => onFocus(briefingTabId(path), 0)} />
        ) : listError ? (
          <p role="alert" className="p-4 text-sm text-red-300">Could not load what the agent showed: {listError}</p>
        ) : !selected ? (
          listLoading ? <LoadingState label="Loading what the agent showed…" /> : <p className="p-4 text-sm text-zinc-500">This agent has not shown anything yet.</p>
        ) : itemError ? (
          <p role="alert" className="p-4 text-sm text-zinc-400">This revision is no longer stored.</p>
        ) : itemLoading || !item ? (
          <div role="status" aria-label="Loading" className="m-4 h-24 animate-pulse rounded-lg bg-zinc-800/60" />
        ) : (
          // Keyed so a different item or revision starts from a fresh frame.
          // A studio is kept through its revisions: one is only the agent
          // turning it to another scene, and a fresh start would drop the
          // microphone and the person's place.
          <AgentUIItemView key={item.format === "studio" ? item.id : `${item.id}:${item.revision}`} item={item} fill onProposeMessage={proposeMessage} />
        )}
      </div>

      {briefingPage && (
        <div className="flex shrink-0 items-center gap-1.5 border-t border-zinc-800 px-3 py-2.5">
          <span className="min-w-0 truncate text-xs text-zinc-400" title={briefingPage.path}>
            {briefingPage.path}{briefing.branch ? ` on ${briefing.branch}` : ""}
          </span>
        </div>
      )}
      {selected && !studio && (
        <div className="flex shrink-0 flex-wrap items-center gap-1.5 border-t border-zinc-800 px-2 py-1.5">
          <button type="button" aria-label="Previous revision" disabled={revision <= oldest} onClick={() => onFocus(selected.id, revision - 1)} className={stepButton}>
            <ChevronLeft size={16} />
          </button>
          <span className="text-xs tabular-nums text-zinc-400">Revision {revision} of {latest}</span>
          <button type="button" aria-label="Next revision" disabled={revision >= latest} onClick={() => onFocus(selected.id, revision + 1 >= latest ? 0 : revision + 1)} className={stepButton}>
            <ChevronRight size={16} />
          </button>
          {available < latest && <span className="text-[11px] text-zinc-600">{available} kept</span>}
          <div className="ml-auto flex items-center gap-1.5">
            <button type="button" aria-label="Copy source" title="Copy source" disabled={!item?.content} onClick={() => { void copySource() }} className={toolButton}>
              <Copy size={13} /><span className="hidden sm:inline">Copy source</span>
            </button>
            <button type="button" aria-label="Download" title="Download" disabled={!item?.content || busy} onClick={download} className={toolButton}>
              <Download size={13} /><span className="hidden sm:inline">Download</span>
            </button>
            <button type="button" aria-label="Remove" title="Remove" disabled={busy} onClick={() => { void remove() }} className={toolButton}>
              <Trash2 size={13} />
            </button>
          </div>
        </div>
      )}
    </>
  )

  if (mobile) {
    return (
      <section
        ref={containerRef}
        role="dialog"
        aria-modal="true"
        aria-label={panelName}
        tabIndex={-1}
        onKeyDown={(event) => { if (event.key === "Escape") onClose() }}
        // Above the app's bottom navigation (z-40), which a sheet that is
        // the whole screen must cover.
        className="fixed inset-0 z-50 flex flex-col bg-zinc-950 outline-none"
        // index.css keeps every dialog a margin away from a phone's screen
        // edges; maxWidth and maxHeight set that aside for this one.
        style={{ maxWidth: "none", maxHeight: "none", paddingTop: "env(safe-area-inset-top)", paddingBottom: "env(safe-area-inset-bottom)" }}
      >
        {content}
      </section>
    )
  }
  return (
    <aside
      ref={containerRef}
      aria-label={panelName}
      className="relative flex min-h-0 shrink-0 flex-col border-l border-zinc-800 bg-zinc-950"
      style={{ width: `${widthRem}rem`, maxWidth: "70%" }}
    >
      <div
        role="separator"
        aria-label="Resize artifacts"
        aria-orientation="vertical"
        aria-valuemin={MIN_WIDTH_REM * 16}
        aria-valuemax={MAX_WIDTH_REM * 16}
        aria-valuenow={Math.round(widthRem * 16)}
        tabIndex={0}
        onPointerDown={resizeFromPointer}
        onKeyDown={resizeFromKeyboard}
        className="absolute inset-y-0 -left-1.5 z-30 w-3 cursor-col-resize touch-none focus:bg-indigo-500/30 focus:outline-none"
      />
      {content}
    </aside>
  )
}
