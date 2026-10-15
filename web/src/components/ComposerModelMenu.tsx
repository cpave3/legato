import { useEffect, useRef, useState, type ReactNode } from "react"
import { Check, ChevronDown } from "lucide-react"
import { cn } from "../lib/utils"
import { shortModelName } from "../lib/composer"

export interface ComposerModel {
  ref: string
  contextWindow?: number
  vision?: boolean
  isDefault: boolean
}

interface ComposerModelMenuProps {
  models: ComposerModel[]
  model: string
  // The context window and the effort in force, as the chip shows them.
  window?: string
  effort?: string
  // The efforts the model takes; none when the session does not say.
  efforts: string[]
  onModel: (ref: string) => void
  onEffort: (effort: string) => void
  // Why the effort cannot change now (a turn is running), if it cannot.
  effortLocked?: string
  // Every other setting of the model, drawn in the menu's place when asked for.
  settings?: (close: () => void) => ReactNode
}

const heading = "px-2 pb-1 pt-1.5 text-[11px] font-semibold uppercase tracking-[1px] text-lw-faint"

// ComposerModelMenu is the chip at the start of the composer's foot (the
// model, its window and its effort) and the menu it opens: the models to
// choose from, the effort, and the session's other settings.
export function ComposerModelMenu({ models, model, window, effort, efforts, onModel, onEffort, effortLocked, settings }: ComposerModelMenuProps) {
  const [view, setView] = useState<"closed" | "menu" | "settings">("closed")
  const box = useRef<HTMLDivElement>(null)
  const chip = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    if (view === "closed") return
    const outside = (event: MouseEvent) => {
      if (box.current && !box.current.contains(event.target as Node)) setView("closed")
    }
    const escape = (event: KeyboardEvent) => {
      if (event.key === "Escape") { setView("closed"); chip.current?.focus() }
    }
    document.addEventListener("mousedown", outside)
    document.addEventListener("keydown", escape)
    return () => {
      document.removeEventListener("mousedown", outside)
      document.removeEventListener("keydown", escape)
    }
  }, [view])

  // Closing the settings gives the focus back to the chip, once it is drawn.
  const [refocus, setRefocus] = useState(false)
  useEffect(() => {
    if (!refocus) return
    chip.current?.focus()
    setRefocus(false)
  }, [refocus])
  const close = () => { setView("closed"); setRefocus(true) }

  return (
    <div ref={box} className="relative min-w-0">
      <button
        ref={chip}
        type="button"
        title="Change model"
        aria-haspopup="menu"
        aria-expanded={view !== "closed"}
        onClick={() => setView((current) => current === "closed" ? "menu" : "closed")}
        className={cn(
          "flex h-7 min-w-0 max-w-full items-center gap-1 overflow-hidden whitespace-nowrap rounded-md px-2 text-xs text-[#b4b3bd] transition-colors hover:bg-lw-raised hover:text-lw-text max-sm:h-9",
          view !== "closed" && "bg-lw-raised text-lw-text",
        )}
      >
        {/* The name gives up its room first, then the effort. */}
        <span className="min-w-0 shrink-[100] truncate font-lw-mono">{shortModelName(model || "model")}</span>
        {window && <><span aria-hidden="true" className="hidden text-lw-faint sm:inline">·</span><span className="hidden shrink-0 text-lw-faint sm:inline">{window}</span></>}
        {effort && <><span aria-hidden="true" className="text-lw-faint">·</span><span className="min-w-0 truncate text-lw-faint">{effort}</span></>}
        <ChevronDown size={12} aria-hidden="true" className="shrink-0 text-lw-faint" />
      </button>
      {view === "menu" && (
        <div role="menu" aria-label="Model" className="absolute bottom-full left-0 z-30 mb-1 flex w-[340px] max-w-[calc(100vw-2rem)] flex-col gap-0.5 rounded-[10px] border border-lw-line-strong bg-lw-raised p-1.5 text-xs shadow-[0_12px_30px_rgba(0,0,0,.45)]">
          <span className={heading}>Model</span>
          <div className="max-h-[40vh] overflow-y-auto">
            {models.map((entry) => (
              <button
                key={entry.ref}
                type="button"
                role="menuitemradio"
                aria-checked={entry.ref === model}
                onClick={() => { setView("closed"); onModel(entry.ref) }}
                className={cn("flex w-full min-w-0 items-center gap-2 rounded-md px-2 py-1.5 text-left", entry.ref === model ? "bg-lw-selected text-lw-text" : "text-[#b4b3bd] hover:bg-lw-selected")}
              >
                <span className="min-w-0 break-all font-lw-mono">{entry.ref}</span>
                {entry.isDefault && <span className="shrink-0 rounded bg-lw-accent-soft px-1 text-[10px] text-lw-accent-text">default</span>}
                {entry.vision && <span className="shrink-0 text-lw-link" title="vision">👁</span>}
                <span className="ml-auto flex shrink-0 items-center gap-2">
                  {entry.contextWindow != null && <span className="text-lw-faint">{formatWindow(entry.contextWindow)}</span>}
                  {entry.ref === model && <Check size={12} aria-hidden="true" className="text-lw-link" />}
                </span>
              </button>
            ))}
          </div>
          {efforts.length > 0 && (
            <>
              <span className={heading}>Effort</span>
              <div className="flex flex-wrap gap-1 px-2 pb-1.5" title={effortLocked}>
                {efforts.map((value) => (
                  <button
                    key={value}
                    type="button"
                    aria-pressed={value === effort}
                    disabled={effortLocked !== undefined}
                    onClick={() => { setView("closed"); onEffort(value) }}
                    className={cn("rounded px-2 py-0.5 disabled:opacity-50", value === effort ? "bg-lw-selected text-lw-text" : "text-lw-text-2 hover:bg-lw-selected hover:text-lw-text")}
                  >
                    {value}
                  </button>
                ))}
              </div>
            </>
          )}
          {settings && (
            <button type="button" onClick={() => setView("settings")} className="border-t border-lw-line-strong px-2 pb-0.5 pt-1.5 text-left text-lw-text-2 hover:text-lw-text">
              More session settings…
            </button>
          )}
        </div>
      )}
      {view === "settings" && settings && (
        <div className="absolute bottom-full left-0 z-30 mb-1 rounded-[10px] border border-lw-line-strong bg-lw-raised shadow-[0_12px_30px_rgba(0,0,0,.45)]">
          {settings(close)}
        </div>
      )}
    </div>
  )
}

function formatWindow(tokens: number): string {
  if (tokens >= 1_000_000) return `${Number((tokens / 1_000_000).toFixed(1))}M`
  return `${Math.round(tokens / 1000)}k`
}
