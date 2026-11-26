import { useEffect, useRef, useState, type ReactNode } from "react"
import { Link } from "react-router-dom"
import { LazyWorkflowGraph } from "../workflow-graph/LazyWorkflowGraph"
import type { WorkflowGraphProps } from "../workflow-graph/WorkflowGraph"
import { NodeSheet } from "../NodeSheet"
import type { Insets } from "../../lib/graphViewport"

// The widths of the cards' column and the side panel over a workflow's
// graph, in pixels, with the space round each.
export const dockWidth = 360
export const panelWidth = 380
const gap = 16
// The least of the graph a dock that yields leaves in view between the docks.
const graphRoom = 120

// CanvasHeader is the one row over a workflow's graph (LEG-651): the way
// back, the title with a line under it, then the page's state and actions.
export function CanvasHeader({ back, title, subtitle, children }: { back: { to: string; label: string }; title: string; subtitle?: ReactNode; children?: ReactNode }) {
  return <header className="flex shrink-0 flex-wrap items-center gap-x-4 gap-y-2 border-b border-lw-line bg-lw-surface px-4 py-2.5">
    <Link to={back.to} className="text-sm text-lw-link">← {back.label}</Link>
    <span aria-hidden="true" className="hidden h-7 w-px bg-lw-line sm:block" />
    <div className="min-w-0 flex-1 basis-56">
      <h1 className="truncate text-base font-semibold leading-snug" title={title}>{title}</h1>
      {subtitle && <p className="truncate text-xs text-lw-text-2">{subtitle}</p>}
    </div>
    {children}
  </header>
}

// Keys pressed in these belong to them, not to the canvas.
const own = "input, textarea, select, [contenteditable=true], [role=dialog], [role=menu]"

export type WorkflowCanvasProps = {
  /** label names the section the graph fills, such as "Run graph". */
  label: string
  /** graph is what the graph draws; the canvas adds its insets and tools.
   * Null while there is none yet: empty says why, in its place. */
  graph: Omit<WorkflowGraphProps, "canvas"> | null
  empty?: ReactNode
  /** focusId is the node the view opens on when the whole graph is too
   * small to read (where a run is); without one it opens at the start. */
  focusId?: string | null
  /** cards are the left dock (a phone's bottom sheet); none, no dock. */
  cards?: ReactNode
  /** panel is the selected node's details: the right dock, or on a phone
   * a sheet titled panelTitle. */
  panel?: ReactNode
  panelTitle?: string
  /** onClosePanel closes the panel: Escape, and the phone's sheet. */
  onClosePanel: () => void
  /** selected says there is a selection for Escape to clear. */
  selected: boolean
  panelWidth?: number
  /** notices float over the free area (a strip above the graph on a phone). */
  notices?: ReactNode
  /** hiddenPill is what Show panels says while the cards are hidden. */
  hiddenPill: ReactNode
  narrow: boolean
  /** dockYields: where the canvas has no room for both docks and some of
   * the graph, the left dock steps aside while the panel is open, until
   * Show panels asks for it. */
  dockYields?: boolean
}

// WorkflowCanvas is a workflow's graph as the page (LEG-651, LEG-655), with
// cards docked left and the selected node's details docked right: the
// Panels button and . show or hide them, Escape closes the details.
export function WorkflowCanvas({ label, graph, empty, focusId = null, cards, panel, panelTitle = "", onClosePanel, selected, panelWidth: width = panelWidth, notices, hiddenPill, narrow, dockYields = false }: WorkflowCanvasProps) {
  const [panelsOn, setPanelsOn] = useState(true)
  const [dockAsked, setDockAsked] = useState(false)
  // The canvas's own width: the app's rails take some of the window's.
  const section = useRef<HTMLElement>(null)
  const [room, setRoom] = useState(0)
  useEffect(() => {
    const element = section.current
    if (!dockYields || !element) return
    const measure = () => setRoom(element.clientWidth)
    measure()
    if (typeof ResizeObserver === "undefined") return
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    return () => observer.disconnect()
  }, [dockYields])
  const cramped = dockYields && room > 0 && room < dockWidth + width + 4 * gap + graphRoom
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.altKey) return
      if ((event.target as Element | null)?.closest?.(own)) return
      if (event.key === "Escape" && selected) onClosePanel()
      else if (event.key === ".") setPanelsOn(on => !on)
      else return
      event.preventDefault()
    }
    document.addEventListener("keydown", onKey)
    return () => document.removeEventListener("keydown", onKey)
  }, [selected, onClosePanel])
  const showPanel = panelsOn && Boolean(panel) && !narrow
  const hasCards = cards !== undefined && cards !== null && cards !== false
  const showDock = panelsOn && hasCards && (!cramped || !showPanel || dockAsked)
  const insets: Insets = narrow
    ? { top: 0, left: 0, right: 0, bottom: showDock ? 220 : 0 }
    : { top: 0, bottom: 0, left: showDock ? dockWidth + 2 * gap : 0, right: showPanel ? width + 2 * gap : 0 }
  const panelsButton = <button type="button" aria-pressed={panelsOn} title="Show or hide the panels (.)" onClick={() => setPanelsOn(!panelsOn)} className="inline-flex h-8 items-center justify-center rounded-md px-2 text-xs text-lw-text hover:bg-lw-selected aria-pressed:bg-lw-selected">Panels</button>
  return <>
    {/* On a phone the graph's toolbar is at its top, so the notices go
        above the graph instead of over it. */}
    {narrow && notices && <div className="shrink-0 space-y-2 border-b border-lw-line bg-lw-rail px-3 py-2">{notices}</div>}
    <section ref={section} aria-label={label} className="relative min-h-0 flex-1 overflow-hidden bg-lw-ground">
      <div className="absolute inset-0">
        {graph ? <LazyWorkflowGraph {...graph} canvas={{ insets, focusId, tools: panelsButton }} />
          : <p role="status" className="flex h-full items-center justify-center p-4 text-sm text-lw-text-2" style={{ paddingLeft: insets.left, paddingRight: insets.right }}>{empty}</p>}
      </div>
      {!narrow && notices && <div className="pointer-events-none absolute top-4 z-10 flex justify-center px-4" style={{ left: insets.left, right: insets.right }}>
        <div className="pointer-events-auto w-full max-w-md space-y-2">{notices}</div>
      </div>}
      {hasCards && !showDock && <button type="button" onClick={() => { setPanelsOn(true); setDockAsked(true) }} className="absolute left-4 top-4 z-10 inline-flex h-9 items-center gap-2 rounded-full border border-lw-line-strong bg-lw-surface px-3 text-[13px] text-lw-text shadow-[var(--shadow-lw-panel)] hover:bg-lw-raised">
        {hiddenPill}<span className="text-lw-text-2">· Show panels</span>
      </button>}
      {showDock && (narrow
        ? <div className="absolute inset-x-0 bottom-0 z-10 max-h-[50%] space-y-2 overflow-y-auto rounded-t-2xl border-t border-lw-line bg-lw-rail p-3 shadow-[var(--shadow-lw-panel)]">{cards}</div>
        : <div className="pointer-events-none absolute bottom-20 left-4 top-4 z-10 flex flex-col gap-2 overflow-y-auto [&>*]:pointer-events-auto" style={{ width: dockWidth, maxWidth: "calc(100% - 2rem)" }}>{cards}</div>)}
      {showPanel && <div className="pointer-events-none absolute bottom-4 right-4 top-4 z-10 flex flex-col" style={{ width, maxWidth: "calc(100% - 2rem)" }}>
        <div className="pointer-events-auto flex min-h-0 flex-col">{panel}</div>
      </div>}
    </section>
    {narrow && panel && <NodeSheet title={panelTitle} onClose={onClosePanel}>{panel}</NodeSheet>}
  </>
}
