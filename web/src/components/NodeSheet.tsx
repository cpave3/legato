import { useRef, type ReactNode } from "react"
import { X } from "lucide-react"
import { useModalLayer } from "../hooks/useModalLayer"

// NodeSheet is a workflow graph node's details on a phone: a bottom sheet
// over the page in place of the side panel, touch-sized, closed with its
// button, Escape or a tap outside.
export function NodeSheet({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  const containerRef = useRef<HTMLDivElement>(null)
  useModalLayer({ open: true, onClose, containerRef })
  // It opens above the phone's tab bar, as the Needs you sheet does.
  // The modal layer holds the backdrop too: it makes everything outside its
  // container inert, which would swallow the tap that closes the sheet.
  return <div ref={containerRef} className="fixed inset-x-0 top-0 bottom-[calc(3.5rem+env(safe-area-inset-bottom))] z-40 md:bottom-0">
    <div aria-hidden="true" data-testid="node-sheet-backdrop" className="absolute inset-0 bg-black/45" onClick={onClose} />
    <div role="dialog" aria-modal="true" aria-label={title} tabIndex={-1}
      className="absolute inset-x-0 bottom-0 flex max-h-[80%] flex-col rounded-t-2xl border-t border-lw-line-strong/40 bg-lw-rail font-lw text-lw-text shadow-2xl">
      <div className="flex items-center gap-2 px-3 pt-2">
        <span aria-hidden="true" className="mx-auto h-1 w-10 rounded-full bg-lw-line" />
      </div>
      <div className="flex justify-end px-2">
        <button type="button" onClick={onClose} aria-label="Close" className="flex size-11 items-center justify-center rounded-full text-lw-text-2 hover:text-lw-text"><X size={20} /></button>
      </div>
      <div className="min-h-0 overflow-y-auto px-3 pb-4">{children}</div>
    </div>
  </div>
}
