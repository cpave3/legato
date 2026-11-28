import { useEffect, useRef, type RefObject } from "react"
import { createPortal } from "react-dom"
import { useMenuLayer } from "./ui/useMenuLayer"
import type { SlashItem, SlashMatch } from "../lib/slashMenu"

// A plugin's skill is named "plugin:skill"; the plugin is drawn faint.
function MatchedName({ name, matched }: { name: string; matched: number[] }) {
  const plugin = name.indexOf(":")
  const hits = new Set(matched)
  return (
    <span className="min-w-0 truncate font-lw-mono text-[13px]">
      {Array.from(name, (ch, i) => (
        <span
          key={i}
          className={hits.has(i) ? "font-semibold text-lw-accent-text" : i <= plugin ? "text-lw-faint" : "text-lw-text"}
        >
          {ch}
        </span>
      ))}
    </span>
  )
}

export function NativeSlashMenu({
  anchorRef, onClose,
  loading, errors, empty, onRetry,
  browser,
  listId,
  optionId,
  matches,
  active,
  onHover,
  onPick,
}: {
  anchorRef: RefObject<HTMLTextAreaElement | null>
  onClose: () => void
  loading: boolean
  errors: string[]
  empty: boolean
  onRetry: () => void
  browser?: {
    query: string
    onQuery: (query: string) => void
    filter: "all" | "skill" | "command"
    onFilter: (filter: "all" | "skill" | "command") => void
    onKeyDown: (event: React.KeyboardEvent) => void
    onClose: () => void
  }
  listId: string
  optionId: (index: number) => string
  matches: SlashMatch[]
  active: number
  onHover: (index: number) => void
  onPick: (item: SlashItem) => void
}) {
  const listRef = useRef<HTMLDivElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  useMenuLayer({ open: true, onClose, containerRef: panelRef, triggerRef: anchorRef, placement: "above", keyboard: false, dismissOnOutside: true })

  // Keep the highlighted row in view as the arrows move it.
  useEffect(() => {
    const row = listRef.current?.querySelector<HTMLElement>(`[data-index="${active}"]`)
    row?.scrollIntoView?.({ block: "nearest" })
  }, [active, matches])

  return createPortal(
    <div ref={panelRef} onKeyDown={(event) => {
      if (event.key === "Escape" && !event.nativeEvent.isComposing) {
        event.preventDefault()
        event.stopPropagation()
        onClose()
      }
    }} className="fixed z-[60] flex w-[min(48rem,calc(100vw-1rem))] flex-col overflow-hidden rounded-[10px] border border-lw-line-strong bg-lw-raised shadow-[var(--shadow-lw-panel)]">
      {browser && <div className="border-b border-lw-line p-3">
        <div className="flex items-center gap-2">
          <input autoFocus role="combobox" aria-label="Search commands and skills" aria-expanded="true" aria-controls={listId}
            aria-activedescendant={matches.length ? optionId(active) : undefined} aria-autocomplete="list"
            value={browser.query} onChange={(e) => browser.onQuery(e.target.value)} onKeyDown={browser.onKeyDown}
            className="min-h-11 min-w-0 flex-1 rounded-md border border-lw-line-strong bg-lw-surface px-3 text-base text-lw-text focus-visible:outline-2 focus-visible:outline-lw-accent" />
          <button type="button" onClick={browser.onClose} className="min-h-11 px-2 text-sm text-lw-text-2">Close</button>
        </div>
        <div role="group" aria-label="Catalog type" className="mt-2 flex gap-1">
          {([['all', 'All'], ['skill', 'Skills'], ['command', 'Commands']] as const).map(([value, label]) =>
            <button key={value} type="button" aria-pressed={browser.filter === value} onClick={() => browser.onFilter(value)}
              className={`min-h-11 rounded-md px-3 text-sm text-lw-text ${browser.filter === value ? "bg-lw-selected" : "hover:bg-lw-surface"}`}>{label}</button>)}
        </div>
      </div>}
      {loading && <p role="status" className="px-3 py-2 text-sm text-lw-text-2">Loading commands and skills…</p>}
      {errors.length > 0 && <div role="alert" className="px-3 py-2 text-sm text-lw-danger">
        {errors.map((error) => <p key={error}>{error}</p>)}
        <button type="button" onClick={onRetry} disabled={loading} className="min-h-11 underline disabled:opacity-50">Retry</button>
      </div>}
      <div
        ref={listRef}
        id={listId}
        role="listbox"
        aria-label="Commands and skills"
        className="min-h-0 max-h-[min(22rem,45vh)] overflow-y-auto overscroll-contain p-1"
      >
        {matches.length === 0 ? (
          !loading && <div className="px-2.5 py-3 text-sm text-lw-text-2">{empty ? errors.length ? "Catalog unavailable" : "No commands or skills available for this agent" : "No matching commands or skills"}</div>
        ) : matches.map(({ item, matched }, i) => {
          const selected = i === active
          return (
            <div
              key={item.type + ":" + item.name}
              id={optionId(i)}
              role="option"
              aria-selected={selected}
              data-index={i}
              data-name={item.name}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => onPick(item)}
              onMouseEnter={() => onHover(i)}
              className={
                "flex cursor-pointer flex-col gap-0.5 rounded-md px-2.5 py-2 min-h-11 transition-colors " +
                (selected ? "bg-lw-selected" : "hover:bg-lw-selected/60")
              }
            >
              <div className="flex min-w-0 items-center gap-2">
                <span className="text-lw-faint font-lw-mono text-[13px]">/</span>
                <MatchedName name={item.name} matched={matched} />
                <span className={"ml-auto shrink-0 rounded px-1 font-lw-mono text-xs " + (item.type === "command" ? "bg-lw-accent-soft text-lw-accent-text" : "bg-lw-needs-soft text-lw-needs")}>
                  {item.type === "command" ? "command" : "skill"}
                </span>
              </div>
              {selected && <div className="pl-3 text-sm leading-5 text-lw-text-2 whitespace-pre-wrap break-words">
                {item.description && <p>{item.description}</p>}
                {(item.source || item.name.includes(":")) && <p className="mt-1">{item.source ? `Source: ${item.source}` : ""}{item.name.includes(":") ? `${item.source ? " · " : ""}Plugin: ${item.name.split(":")[0]}` : ""}</p>}
              </div>}
            </div>
          )
        })}
      </div>
      <div className="flex min-w-0 items-center gap-2 overflow-hidden whitespace-nowrap border-t border-lw-line px-2.5 py-1 text-xs text-lw-text-2 max-sm:hidden">
        <span>↑↓ to move</span>
        <span>·</span>
        <span>Tab or Enter to complete</span>
        <span>·</span>
        <span>Esc to close</span>
      </div>
    </div>, document.body
  )
}
