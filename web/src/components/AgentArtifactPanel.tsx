import { useEffect, useId, useRef } from "react"
import { useAgentUIItem, useAgentUIItems } from "../hooks/useAgentUI"
import { useReviewBriefing } from "../hooks/useReviewBriefing"
import { useProposeMessage } from "../hooks/useProposeMessage"
import { useServer } from "../hooks/useServer"
import { useModalLayer } from "./ui/useModalLayer"
import { activeTabOf, groupArtifacts, pickArtifact, pinnedRevision } from "../lib/artifactPanel"
import type { ArtifactFocus } from "../lib/artifactFocus"
import { MAX_WIDTH_REM, MIN_WIDTH_REM, useArtifactPanelWidth } from "./useArtifactPanelWidth"
import { useArtifactActions } from "./useArtifactActions"
import { ArtifactBody, ArtifactPanelHeader, BriefingFooter, RevisionBar } from "./AgentArtifactPanelParts"

interface AgentArtifactPanelProps {
  agentId: string
  focus: ArtifactFocus | null
  onFocus: (itemId: string, revision: number) => void
  onClose: () => void
  // A phone gets the panel as a full-screen sheet; a wide screen as a
  // resizable column beside the conversation.
  mobile: boolean
}

// usePinnedRevisions remembers the revision each tab was last read at, so
// going back to a tab returns to that revision rather than the newest.
function usePinnedRevisions(agentId: string, focus: ArtifactFocus | null, onFocus: (itemId: string, revision: number) => void) {
  const revisions = useRef(new Map<string, number>())
  useEffect(() => {
    if (focus) revisions.current.set(`${agentId}:${focus.itemId}`, focus.revision)
  }, [agentId, focus])
  return (id: string) => onFocus(id, revisions.current.get(`${agentId}:${id}`) ?? 0)
}

// AgentArtifactPanel shows what one agent session asked Legato to show: its
// artifacts, and its cards for a session whose conversation Legato cannot
// draw. The agent's content is drawn by AgentUIItemView, HTML in a sandbox.
export function AgentArtifactPanel({ agentId, focus, onFocus, onClose, mobile }: AgentArtifactPanelProps) {
  const { baseUrl } = useServer()
  const { items, loading: listLoading, error: listError } = useAgentUIItems(agentId)
  const groups = groupArtifacts(items)
  // The review briefing on the agent's branch, if it carries one: pages the
  // pull request will show, listed after the artifacts and read from the
  // checkout rather than shown by the agent.
  const briefing = useReviewBriefing(agentId)
  const { briefingPage, selected } = pickArtifact(items, groups, briefing.pages, focus)
  const studio = selected?.format === "studio"
  const panelName = studio ? "Studio" : "Artifacts"
  const { item, loading: itemLoading, error: itemError } = useAgentUIItem(selected?.id, pinnedRevision(selected, focus))
  const proposeMessage = useProposeMessage(agentId)
  const { widthRem, resizeFromPointer, resizeFromKeyboard } = useArtifactPanelWidth(mobile)
  const actions = useArtifactActions(baseUrl, item, selected)
  const containerRef = useRef<HTMLElement>(null)
  const tabPrefix = useId()
  const activateTab = usePinnedRevisions(agentId, focus, onFocus)
  const activeTab = activeTabOf({ briefingPage, selected })
  const tabId = (id: string) => `${tabPrefix}-tab-${encodeURIComponent(id)}`
  const panelId = `${tabPrefix}-panel`
  // The body is the tab panel of the tab in front; a studio has no tabs.
  const tabPanel = !studio && activeTab ? { role: "tabpanel", "aria-labelledby": tabId(activeTab), tabIndex: 0 } : {}

  useModalLayer({ open: mobile, onClose, containerRef })

  const content = (
    <>
      <ArtifactPanelHeader
        studio={studio}
        title={panelName}
        selected={selected}
        groups={groups}
        tabs={{ tabs: [...groups.artifacts, ...briefing.pages, ...groups.cards], pages: briefing.pages, activeTab, tabId, panelId, onActivate: activateTab }}
        onFocus={onFocus}
        onClose={onClose}
      />

      <div {...tabPanel} id={panelId} className="flex min-h-0 flex-1 flex-col bg-zinc-900">
        <ArtifactBody
          agentId={agentId}
          briefingPage={briefingPage}
          selected={selected}
          item={item}
          listLoading={listLoading}
          listError={listError}
          itemLoading={itemLoading}
          itemError={itemError}
          onFocus={onFocus}
          onProposeMessage={proposeMessage}
        />
      </div>

      {briefingPage && <BriefingFooter page={briefingPage} branch={briefing.branch} />}
      {selected && !studio && (
        <RevisionBar
          selected={selected}
          item={item}
          busy={actions.busy}
          onFocus={onFocus}
          onCopy={() => { void actions.copySource() }}
          onDownload={actions.download}
          onRemove={() => { void actions.remove() }}
        />
      )}
      {actions.confirmDialog}
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
