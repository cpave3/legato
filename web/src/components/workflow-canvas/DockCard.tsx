import type { ReactNode } from "react"
import { ChevronDown, ChevronRight, X } from "lucide-react"
import { cn } from "../../lib/utils"
import type { Tone } from "../../lib/workflowView"
import { cardLook } from "./canvasStyles"

// DockCard is one of the cards over a workflow's graph (LEG-651): a title
// row that folds it to that row, and what it holds.
export function DockCard({ label, heading, folded, onFold, tone, children }: { label: string; heading: ReactNode; folded: boolean; onFold: () => void; tone?: Tone; children?: ReactNode }) {
  return <section aria-label={label} className={cn("shrink-0 rounded-xl border shadow-[var(--shadow-lw-panel)]", tone ? cardLook[tone] : "border-lw-line bg-lw-surface")}>
    {/* The row folds the card; its button is the control the keyboard and
        screen readers reach. */}
    <div className="flex min-h-10 cursor-pointer items-center gap-2 py-1.5 pl-4 pr-1.5" onClick={onFold}>
      <div className="min-w-0 flex-1">{heading}</div>
      <button type="button" aria-expanded={!folded} aria-label={label} className="inline-flex size-8 shrink-0 items-center justify-center rounded-md text-lw-text-2 hover:bg-lw-selected">
        {folded ? <ChevronRight size={14} aria-hidden="true" /> : <ChevronDown size={14} aria-hidden="true" />}
      </button>
    </div>
    {!folded && children}
  </section>
}

// PanelClose is the side panel's own close button over a workflow's graph.
export function PanelClose({ onClose }: { onClose?: () => void }) {
  if (!onClose) return null
  return <button type="button" aria-label="Close" onClick={onClose} className="-mr-1.5 -mt-1.5 inline-flex size-8 shrink-0 items-center justify-center rounded-md text-lw-text-2 hover:bg-lw-selected hover:text-lw-text"><X size={16} aria-hidden="true" /></button>
}
