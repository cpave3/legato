import { useEffect, useRef } from "react"
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
  listId,
  optionId,
  matches,
  active,
  onHover,
  onPick,
}: {
  listId: string
  optionId: (index: number) => string
  matches: SlashMatch[]
  active: number
  onHover: (index: number) => void
  onPick: (item: SlashItem) => void
}) {
  const listRef = useRef<HTMLDivElement>(null)

  // Keep the highlighted row in view as the arrows move it.
  useEffect(() => {
    const row = listRef.current?.querySelector<HTMLElement>(`[data-index="${active}"]`)
    row?.scrollIntoView?.({ block: "nearest" })
  }, [active, matches])

  return (
    <div className="absolute bottom-full left-3 right-3 z-20 mb-1 flex flex-col overflow-hidden rounded-[10px] border border-lw-line-strong bg-lw-raised shadow-[0_12px_30px_rgba(0,0,0,.45)] max-sm:left-0 max-sm:right-0">
      <div
        ref={listRef}
        id={listId}
        role="listbox"
        aria-label="Commands and skills"
        className="max-h-[min(22rem,45vh)] overflow-y-auto overscroll-contain p-1"
      >
        {matches.length === 0 ? (
          <div className="px-2.5 py-3 text-center text-xs text-lw-faint">No matching commands or skills</div>
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
              onMouseDown={(e) => { e.preventDefault(); onPick(item) }}
              onMouseEnter={() => onHover(i)}
              className={
                "flex cursor-pointer flex-col gap-0.5 rounded-md px-2.5 py-1.5 transition-colors " +
                (selected ? "bg-lw-selected shadow-[inset_2px_0_0] shadow-lw-accent" : "hover:bg-lw-selected/60")
              }
            >
              <div className="flex min-w-0 items-center gap-2">
                <span className="text-lw-faint font-lw-mono text-[13px]">/</span>
                <MatchedName name={item.name} matched={matched} />
                <span className={"ml-auto shrink-0 rounded px-1 font-lw-mono text-[10px] uppercase " + (item.type === "command" ? "bg-lw-accent-soft text-lw-accent-text" : "bg-lw-needs-soft text-lw-needs")}>
                  {item.type === "command" ? "command" : "skill"}
                </span>
              </div>
              {item.description && <div className="truncate pl-3 text-[11px] text-lw-faint">{item.description}</div>}
            </div>
          )
        })}
      </div>
      <div className="flex min-w-0 items-center gap-2 overflow-hidden whitespace-nowrap border-t border-lw-line px-2.5 py-1 font-lw-mono text-[10px] text-lw-faint max-sm:hidden">
        <span>↑↓ to move</span>
        <span>·</span>
        <span>Tab or Enter to complete</span>
        <span>·</span>
        <span>Esc to close</span>
      </div>
    </div>
  )
}
