import type { ReactNode, RefObject } from "react"
import { addressOf, type Selection } from "../../lib/workflowAddress"
import type { WorkflowGraphModel } from "../../lib/workflow"
import { ArrowLeft } from "lucide-react"
import { Button } from "../ui/Button"
import { DockCard, PanelClose } from "../workflow-canvas/DockCard"
import { cardTitle, floatingPanel } from "../workflow-canvas/canvasStyles"
import { WorkflowCanvas } from "../workflow-canvas/WorkflowCanvas"
import { STALE_WORDS } from "./OutlineMap"
import type { Row } from "./outlineRows"
import type { useCanvasDock } from "./useCanvasDock"

// The ways the outline is laid out: on the canvas, beside the YAML, while
// the YAML doesn't read, and as forms beside the inspector.

// The editor's inspector is wider than a run's side panel: its forms need
// the room (LEG-655).
const INSPECTOR_WIDTH = 480

export type Broken = { line?: number; message: string }
export type Paste = { pasted: string | null; replace: () => void; cancel: () => void }

/** PasteConfirm asks before a whole workflow pasted on the forms replaces
 * this one. */
export function PasteConfirm({ paste, canvas = false }: { paste: Paste; canvas?: boolean }) {
  if (paste.pasted === null) return null
  return <div role="alertdialog" aria-label="Replace the workflow" className={`flex flex-wrap items-center gap-2 rounded-lg border border-lw-needs bg-lw-surface p-3 text-sm${canvas ? " shadow-[var(--shadow-lw-panel)]" : ""}`}>
    <span className="min-w-0 flex-1">You pasted a whole workflow. Replace this one with it? Undo brings this one back.</span>
    <Button size="sm" onClick={paste.replace}>Replace</Button>
    <Button size="sm" variant="ghost" onClick={paste.cancel}>Cancel</Button>
  </div>
}

/** BrokenNote says the YAML can't be read, and shows where. */
export function BrokenNote({ broken, onShow }: { broken?: Broken; onShow: (line?: number) => void }) {
  if (!broken) return null
  return <p role="alert" className="flex flex-wrap items-center gap-2 rounded-lg border border-lw-danger-line bg-lw-error-soft px-3 py-2 text-xs text-lw-danger">
    <span className="min-w-0 flex-1">{broken.line ? `Line ${broken.line}: ` : ""}can't read the YAML. The outline is as it last could be; the forms are off until it reads again.</span>
    <Button size="sm" variant="outline" onClick={() => onShow(broken.line)}>Show</Button>
  </p>
}

/** JumpMenu is the outline beside the YAML on a phone: a menu to jump
 * with. */
export function JumpMenu({ order, shown, broken, note, onChoose }: { order: Row[]; shown: Selection | null; broken: boolean; note: ReactNode; onChoose: (selection: Selection) => void }) {
  return <div className="flex flex-col gap-2 text-sm">
    {note}
    <label className="flex items-center gap-2 text-xs text-lw-text-2">Jump to
      <select aria-label="Jump to" disabled={broken} className="min-h-9 min-w-0 flex-1 rounded-md border border-lw-line-strong bg-lw-ground px-2 text-sm text-lw-text" value={shown ? addressOf(shown) : ""}
        onChange={event => { const row = order.find(r => addressOf(r.selection) === event.target.value); if (row) onChoose(row.selection) }}>
        <option value="">The outline…</option>
        {order.map(row => <option key={addressOf(row.selection)} value={addressOf(row.selection)}>{row.label}</option>)}
      </select>
    </label>
  </div>
}

/** InspectorPane holds the inspector of the row chosen, or says to choose
 * one; on a phone (back) a button returns to the outline. */
export function InspectorPane({ sectionRef, back, onBack, children }: { sectionRef: RefObject<HTMLElement | null>; back: boolean; onBack?: () => void; children: ReactNode }) {
  return <section ref={sectionRef} aria-label="Inspector" className="flex min-w-0 flex-col gap-3">
    {back && <Button type="button" size="sm" variant="ghost" className="self-start" onClick={onBack}><ArrowLeft size={13} />Outline</Button>}
    {children || <p className="text-sm text-lw-text-2">Choose a row of the outline to edit it.</p>}
  </section>
}

/** SplitOutline is the outline as forms: beside the inspector, or on a
 * phone the outline with the inspector a sheet over it. */
export function SplitOutline({ narrow, sheet, selectedRow, navigation, inspector, error, paste, onPaste }: { narrow: boolean; sheet: boolean; selectedRow: Row | null; navigation: ReactNode; inspector: ReactNode; error: string; paste: Paste; onPaste: (event: React.ClipboardEvent) => void }) {
  const phone = sheet && selectedRow
    ? <div role="dialog" aria-label={selectedRow.label.trim()} className="fixed inset-0 z-40 overflow-y-auto bg-lw-ground px-4 pb-20 pt-3">{inspector}</div>
    : navigation
  return <div className="flex flex-col gap-3 text-sm" onPaste={onPaste}>
    {error && <p role="alert" className="text-sm text-lw-danger">{error}</p>}
    <PasteConfirm paste={paste} />
    {narrow
      ? phone
      : <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,24rem)_minmax(0,1fr)] md:grid-cols-[minmax(0,20rem)_minmax(0,1fr)]">
        <div className="rounded-xl border border-lw-line bg-lw-surface p-2">{navigation}</div>
        <div className="rounded-xl border border-lw-line bg-lw-surface p-4">{inspector}</div>
      </div>}
  </div>
}

type Dock = ReturnType<typeof useCanvasDock>
export type Bodies = { triggers: ReactNode; inputs: ReactNode; steps: ReactNode; about: ReactNode }

function Card({ id, label, size, folds, children }: { id: string; label: string; size: number | null; folds: Dock["folds"]; children: ReactNode }) {
  return <DockCard label={label} folded={folds.folded(id)} onFold={() => folds.toggle(id)}
    heading={<h2 className={cardTitle}>{label}{size !== null && <span className="font-medium text-lw-faint">{size}</span>}</h2>}>
    <div className="flex min-w-0 flex-col gap-1 px-1.5 pb-2">{children}</div>
  </DockCard>
}

type CanvasProps = {
  dock: Dock
  bodies: Bodies
  counts: { triggers: number; inputs: number; steps: number }
  outline: RefObject<HTMLElement | null>
  onKeyDown: (event: React.KeyboardEvent) => void
  pageNotices?: ReactNode
  pageCards?: ReactNode
  broken?: Broken
  brokenNote: ReactNode
  graph: { model: WorkflowGraphModel; stale: boolean } | null
  selectedId: string | null
  onGraphSelect: (id: string | null) => void
  error: string
  paste: Paste
  onPaste: (event: React.ClipboardEvent) => void
  selectedRow: Row | null
  selection: Selection | null
  inspector: ReactNode
  onSelect: (selection: Selection | null) => void
}

// CanvasNotices are the notices over the canvas: the page's, the broken
// YAML's, a stale graph's, an edit's error and a pasted workflow's.
function CanvasNotices({ pageNotices, brokenNote, graph, error, paste }: Pick<CanvasProps, "pageNotices" | "brokenNote" | "graph" | "error" | "paste">) {
  const box = "rounded-lg border border-lw-line bg-lw-surface px-3 py-2 text-[13px] shadow-[var(--shadow-lw-panel)]"
  return <>
    {pageNotices}
    {brokenNote}
    {graph?.stale && <p role="status" className={`${box} text-lw-needs`}>{STALE_WORDS}</p>}
    {error && <p role="alert" className={`${box} text-lw-danger`}>{error}</p>}
    <PasteConfirm paste={paste} canvas />
  </>
}

// anyNotice says whether there is a notice to float over the canvas.
const anyNotice = ({ pageNotices, broken, graph, error, paste }: Pick<CanvasProps, "pageNotices" | "broken" | "graph" | "error" | "paste">) =>
  Boolean(pageNotices || broken || graph?.stale || error || paste.pasted !== null)

// InspectorPanel is the inspector docked on the canvas's right, with its
// close.
function InspectorPanel({ selectedRow, inspector, onSelect }: Pick<CanvasProps, "inspector" | "onSelect"> & { selectedRow: Row }) {
  return <aside aria-label={selectedRow.label.trim()} className={`flex min-h-0 flex-col rounded-xl border border-lw-line bg-lw-surface p-4 text-sm ${floatingPanel}`}>
    <div className="flex items-start gap-2">
      <div className="min-w-0 flex-1">{inspector}</div>
      <PanelClose onClose={() => onSelect(null)} />
    </div>
  </aside>
}

/** CanvasOutline draws the Visual side on a wide screen as the graph, with
 * the outline's sections as cards on the left and the inspector on the
 * right (LEG-655); the page's notices float over it, its Problems card
 * heads the left dock. */
export function CanvasOutline(props: CanvasProps) {
  const { dock, bodies, counts, outline, onKeyDown, broken, graph, selectedRow, selection, onSelect } = props
  const { folds } = dock
  // j and k move across every card's rows: the keys are the dock's.
  const nav = <nav ref={outline} aria-label="Outline" onKeyDown={onKeyDown} className="flex min-w-0 flex-col gap-2">
    <Card id="triggers" label="Triggers" size={counts.triggers} folds={folds}>{bodies.triggers}</Card>
    <Card id="inputs" label="Inputs" size={counts.inputs} folds={folds}>{bodies.inputs}</Card>
    <Card id="steps" label="Steps" size={counts.steps} folds={folds}>{bodies.steps}</Card>
    <Card id="about" label="About" size={null} folds={folds}>{bodies.about}</Card>
  </nav>
  // The panel is shown for the row chosen, but not while the YAML doesn't
  // read.
  const panelRow = broken ? null : selectedRow
  const hasNotices = anyNotice(props)
  return <div className="flex min-h-0 flex-1 flex-col text-sm" onPaste={props.onPaste}>
    <WorkflowCanvas label="Workflow graph" narrow={false} panelWidth={INSPECTOR_WIDTH} dockYields
      graph={graph ? { graph: graph.model, selectedId: props.selectedId, onSelect: props.onGraphSelect, centerOn: dock.centerOn, direction: "LR" } : null}
      empty="The graph shows once Legato has read the file."
      cards={<>{props.pageCards}{broken ? <div inert className="opacity-50">{nav}</div> : nav}</>}
      notices={hasNotices && <CanvasNotices {...props} />} panel={panelRow ? <InspectorPanel {...props} selectedRow={panelRow} /> : null} panelTitle={selectedRow?.label.trim()}
      selected={selection !== null} onClosePanel={() => onSelect(null)} hiddenPill="Outline" />
  </div>
}
