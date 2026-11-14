import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react"
import { cn } from "../lib/utils"

// TileDeck shows several things that run at once, each as a tile: what is
// running shares the space, what has finished waits on a shelf, and one tile
// can be opened in full. It knows nothing of what a tile stands for; the
// native panel uses it for subagents.

export type TileState = "spawning" | "running" | "waiting" | "done" | "error" | "interrupted"

export interface DeckTile {
  id: string
  title: string
  // A short line under the title, such as the agent type.
  meta?: string
  state: TileState
  // When it started and ended, in milliseconds since the epoch.
  startedAt?: number
  endedAt?: number
  // A small figure in the tile's corner, such as "12 tools".
  count?: string
  // One line, for the shelf and the phone strip.
  summary: ReactNode
  // What the tile shows: newest last, as the tile keeps the end in view.
  body: ReactNode
  // Shown only when the tile is open, above the body.
  detail?: ReactNode
}

export interface TileDeckProps {
  title: string
  tiles: DeckTile[]
  // grid lays tiles out side by side; strip is for a phone, with an opened
  // tile shown as a sheet.
  layout: "grid" | "strip"
  // The open tile, if any.
  focused: string | null
  onFocus: (id: string | null) => void
  // A tile to mark, as when the person points at what it stands for
  // elsewhere, and where the person points within the deck.
  highlighted?: string | null
  onHighlight?: (id: string | null) => void
}

const looks: Record<TileState, { label: string; glyph: string; chip: string; frame: string }> = {
  spawning: { label: "spawning", glyph: "○", chip: "bg-lw-live-soft text-lw-live", frame: "border-[#2d3a33]" },
  interrupted: { label: "interrupted", glyph: "■", chip: "bg-lw-idle-soft text-lw-text-2", frame: "border-lw-line" },
  running: { label: "running", glyph: "●", chip: "bg-lw-live-soft text-lw-live", frame: "border-[#2d3a33]" },
  waiting: { label: "needs you", glyph: "◆", chip: "bg-lw-needs-soft text-lw-needs", frame: "border-[#4a3a1a]" },
  done: { label: "done", glyph: "✓", chip: "bg-lw-done-soft text-lw-done", frame: "border-lw-line" },
  error: { label: "error", glyph: "✕", chip: "bg-lw-error-soft text-lw-error", frame: "border-[#4a2a2e]" },
}

const live = (tile: DeckTile) => tile.state === "spawning" || tile.state === "running" || tile.state === "waiting"

export function TileDeck(props: TileDeckProps) {
  const { tiles, focused, onFocus } = props
  const now = useNow(tiles.some((tile) => live(tile) && tile.startedAt !== undefined))

  // Escape closes the open tile.
  useEffect(() => {
    if (focused === null) return
    const close = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !event.defaultPrevented) onFocus(null)
    }
    document.addEventListener("keydown", close)
    return () => document.removeEventListener("keydown", close)
  }, [focused, onFocus])

  return props.layout === "strip" ? <Strip {...props} now={now} /> : <Grid {...props} now={now} />
}

function Grid({ title, tiles, focused, onFocus, highlighted, onHighlight, now }: TileDeckProps & { now: number }) {
  const running = tiles.filter(live)
  const ended = tiles.filter((tile) => !live(tile))
  const opened = focused ? tiles.find((tile) => tile.id === focused) : undefined

  return (
    <section aria-label={title} className="flex min-h-0 min-w-0 flex-1 flex-col bg-[#0e0e11] font-lw text-[13px] leading-5 text-lw-text">
      <header className="flex shrink-0 flex-wrap items-center justify-between gap-x-4 gap-y-1 border-b border-[#222228] px-4 py-2.5">
        <h2 className="m-0 text-[13px] font-semibold">{title}</h2>
        <Counts tiles={tiles} />
      </header>

      {opened ? (
        <div className="flex min-h-0 flex-1 flex-col gap-2.5 p-3 sm:p-4">
          <div className="flex shrink-0 flex-wrap items-center gap-1.5">
            <button
              type="button"
              onClick={() => onFocus(null)}
              className="h-7 shrink-0 rounded-md border border-lw-line-strong bg-lw-raised px-2.5 text-xs text-lw-text hover:border-[#4a4955]"
            >
              <span aria-hidden="true">← </span>All tiles
            </button>
            {tiles.filter((tile) => tile.id !== opened.id).map((tile) => (
              <button
                key={tile.id}
                type="button"
                onClick={() => onFocus(tile.id)}
                className="flex h-7 max-w-[14rem] items-center gap-1.5 rounded-md border border-lw-line bg-lw-surface px-2.5 text-xs text-lw-text-2 hover:border-lw-accent hover:text-lw-text"
              >
                <span aria-hidden="true" className={cn("rounded px-1 font-lw-mono text-[10px]", looks[tile.state].chip)}>{looks[tile.state].glyph}</span>
                <span className="truncate">{tile.title}</span>
              </button>
            ))}
          </div>
          <TileCard tile={opened} now={now} open />
        </div>
      ) : (
        <>
        <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
          {running.length > 0 ? (
            // Running tiles share the height, down to the least a tile needs;
            // only then do they scroll.
            <ul aria-label="Running" className="@container m-0 grid flex-1 list-none auto-rows-[minmax(13rem,1fr)] gap-2.5 p-3 sm:p-4 @[40rem]:grid-cols-2 @[64rem]:grid-cols-3">
              {running.map((tile, index) => (
                <li
                  key={tile.id}
                  className={cn("flex min-h-0 min-w-0",
                    // An odd one out takes the whole row when there are two columns.
                    running.length % 2 === 1 && running.length > 1 && running.length < 5 && index === running.length - 1 && "@[40rem]:col-span-2")}
                  onMouseEnter={() => onHighlight?.(tile.id)}
                  onMouseLeave={() => onHighlight?.(null)}
                >
                  <TileCard tile={tile} now={now} highlighted={highlighted === tile.id} onOpen={() => onFocus(tile.id)} />
                </li>
              ))}
            </ul>
          ) : (
            <p className="m-0 flex-1 px-4 py-8 text-center text-lw-faint">Nothing running.</p>
          )}
        </div>
        {/* The shelf stays at the foot while the running tiles scroll. */}
        {ended.length > 0 && <Shelf tiles={ended} now={now} onFocus={onFocus} highlighted={highlighted} onHighlight={onHighlight} />}
        </>
      )}
    </section>
  )
}

// How many finished tiles the shelf shows before it asks.
const shelfShown = 3

// Shelf lists finished tiles in a line each, newest first. A session that has
// run many keeps only the newest few in view, and the shelf folds away.
function Shelf({ tiles, now, onFocus, highlighted, onHighlight }: {
  tiles: DeckTile[]
  now: number
  onFocus: (id: string | null) => void
  highlighted?: string | null
  onHighlight?: (id: string | null) => void
}) {
  const [open, setOpen] = useState(true)
  const [all, setAll] = useState(false)
  // Newest first; one with no end time stays where it was, after those that have one.
  const newest = tiles
    .map((tile, index) => ({ tile, index }))
    .sort((a, b) => (b.tile.endedAt ?? -1) - (a.tile.endedAt ?? -1) || a.index - b.index)
    .map(({ tile }) => tile)
  const shown = all ? newest : newest.slice(0, shelfShown)
  const hidden = newest.length - shown.length

  return (
    <div className="shrink-0 border-t border-[#222228] px-3 pb-2.5 pt-1.5 sm:px-4">
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
        className="flex w-full items-center justify-between py-0.5 text-xs text-lw-text-2 hover:text-lw-text"
      >
        <span><span aria-hidden="true" className="inline-block w-3">{open ? "▾" : "▸"}</span>Finished</span>
        <span className="font-lw-mono">{tiles.length}</span>
      </button>
      {open && (
        <>
          <ul aria-label="Finished" className={cn("m-0 mt-1 flex list-none flex-col gap-1 p-0", all && "max-h-48 overflow-y-auto")}>
            {shown.map((tile) => (
              <li key={tile.id} onMouseEnter={() => onHighlight?.(tile.id)} onMouseLeave={() => onHighlight?.(null)}>
                <button
                  type="button"
                  onClick={() => onFocus(tile.id)}
                  className={cn("flex w-full min-w-0 items-center gap-2.5 rounded-md border bg-lw-surface px-2.5 py-1 text-left text-[12.5px] hover:border-lw-accent",
                    highlighted === tile.id ? "border-lw-accent" : "border-lw-line")}
                >
                  <StateChip tile={tile} now={now} />
                  <span className="min-w-0 flex-1 truncate">{tile.title}</span>
                  <span className="min-w-0 max-w-[45%] truncate text-xs text-lw-text-2">{tile.summary}</span>
                </button>
              </li>
            ))}
          </ul>
          {(hidden > 0 || all) && newest.length > shelfShown && (
            <button type="button" onClick={() => setAll(!all)} className="mt-1 text-xs text-lw-text-2 hover:text-lw-text">
              {all ? "Show fewer" : `Show ${hidden} more`}
            </button>
          )}
        </>
      )}
    </div>
  )
}

function TileCard({ tile, now, open = false, highlighted = false, onOpen }: {
  tile: DeckTile
  now: number
  open?: boolean
  highlighted?: boolean
  onOpen?: () => void
}) {
  const look = looks[tile.state]
  const body = useRef<HTMLDivElement>(null)
  useStickToBottom(body, open)

  return (
    <article
      aria-label={`${tile.title}, ${look.label}`}
      onClick={onOpen}
      className={cn("flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden rounded-[10px] border bg-lw-surface transition-colors",
        onOpen && "cursor-pointer hover:border-lw-accent",
        highlighted ? "border-lw-accent" : look.frame)}
    >
      <div className="flex shrink-0 flex-col gap-0.5 border-b border-[#222228] px-3 pb-2 pt-2.5">
        <div className="flex items-center justify-between gap-2">
          <StateChip tile={tile} now={now} />
          {tile.count && <span className="font-lw-mono text-[11px] text-lw-faint">{tile.count}</span>}
        </div>
        {onOpen
          // The tile opens from anywhere on it; the title is the way in by keyboard.
          ? <button type="button" aria-label={`Open ${tile.title}`} className="truncate text-left text-sm font-semibold text-lw-text focus-visible:outline-none focus-visible:underline">{tile.title}</button>
          : <h3 className="m-0 truncate text-sm font-semibold">{tile.title}</h3>}
        {tile.meta && <span className="truncate text-[11.5px] text-lw-text-2">{tile.meta}</span>}
      </div>
      {open && tile.detail && <div className="shrink-0 px-3 pt-2.5">{tile.detail}</div>}
      <div
        ref={body}
        data-tile-body
        className={cn("flex min-h-0 flex-1 flex-col px-3 py-2",
          // A closed tile keeps the newest in view and cuts the rest; an open
          // one scrolls.
          open ? "overflow-y-auto" : "justify-end overflow-hidden [contain:size]")}
      >
        {tile.body}
      </div>
    </article>
  )
}

function Strip({ title, tiles, focused, onFocus, now }: TileDeckProps & { now: number }) {
  const ordered = [...tiles.filter(live), ...tiles.filter((tile) => !live(tile))]
  const index = ordered.findIndex((tile) => tile.id === focused)
  const opened = index >= 0 ? ordered[index] : undefined
  const step = (by: number) => onFocus(ordered[(index + by + ordered.length) % ordered.length].id)

  return (
    <section aria-label={title} className="shrink-0 border-b border-[#222228] bg-[#0e0e11] font-lw text-[13px] leading-5 text-lw-text">
      <ul aria-label={title} className="m-0 flex list-none gap-2 overflow-x-auto px-3 py-2 [scrollbar-width:none]">
        {ordered.map((tile) => (
          <li key={tile.id} className="flex w-[9.75rem] shrink-0">
            <button
              type="button"
              aria-label={`Open ${tile.title}`}
              onClick={() => onFocus(tile.id)}
              className={cn("flex w-full min-w-0 flex-col items-start gap-0.5 overflow-hidden rounded-[9px] border bg-lw-surface px-2.5 py-1.5 text-left", looks[tile.state].frame)}
            >
              <StateChip tile={tile} now={now} />
              <span className="w-full truncate text-[12.5px] font-semibold">{tile.title}</span>
              <span className="w-full truncate text-[11.5px] text-lw-text-2">{tile.summary}</span>
            </button>
          </li>
        ))}
      </ul>
      {opened && <Sheet tile={opened} now={now} onClose={() => onFocus(null)} onStep={ordered.length > 1 ? step : undefined} position={[index, ordered.length]} />}
    </section>
  )
}

function Sheet({ tile, now, onClose, onStep, position }: {
  tile: DeckTile
  now: number
  onClose: () => void
  onStep?: (by: number) => void
  position: [number, number]
}) {
  const body = useRef<HTMLDivElement>(null)
  const swipe = useRef<{ x: number; y: number } | null>(null)
  useStickToBottom(body, true)

  return (
    <>
      <div aria-hidden="true" className="fixed inset-0 z-40 bg-black/45" onClick={onClose} />
      <div
        role="dialog"
        aria-modal="true"
        aria-label={tile.title}
        // A sideways swipe moves to the next or previous tile.
        onPointerDown={(event) => { swipe.current = { x: event.clientX, y: event.clientY } }}
        onPointerUp={(event) => {
          const start = swipe.current
          swipe.current = null
          if (!start || !onStep) return
          const dx = event.clientX - start.x
          if (Math.abs(dx) > 60 && Math.abs(dx) > 2 * Math.abs(event.clientY - start.y)) onStep(dx < 0 ? 1 : -1)
        }}
        className="fixed inset-x-0 bottom-0 z-50 flex h-[82dvh] flex-col rounded-t-2xl border-t border-lw-line-strong bg-lw-surface pb-[env(safe-area-inset-bottom)] font-lw text-[13px] leading-5 text-lw-text shadow-[0_-12px_30px_rgba(0,0,0,.5)]"
      >
        <div aria-hidden="true" className="mx-auto mb-1 mt-2 h-1 w-9 shrink-0 rounded-full bg-[#3a3a44]" />
        <div className="flex shrink-0 flex-col gap-0.5 border-b border-[#222228] px-3.5 pb-2.5">
          <div className="flex items-center justify-between gap-2">
            <StateChip tile={tile} now={now} />
            {tile.count && <span className="font-lw-mono text-[11px] text-lw-faint">{tile.count}</span>}
          </div>
          <h3 className="m-0 text-[15px] font-semibold">{tile.title}</h3>
          {tile.meta && <span className="text-[11.5px] text-lw-text-2">{tile.meta}</span>}
        </div>
        {tile.detail && <div className="shrink-0 px-3.5 pt-2.5">{tile.detail}</div>}
        <div ref={body} data-tile-body className="flex min-h-0 flex-1 flex-col overflow-y-auto px-3.5 py-2.5">{tile.body}</div>
        <div className="flex shrink-0 items-center justify-between gap-2 border-t border-[#222228] px-3.5 py-2.5">
          <button type="button" onClick={onClose} className="h-8 rounded-md border border-lw-line-strong bg-lw-raised px-3 text-xs text-lw-text">Close</button>
          {onStep && (
            <div className="flex items-center gap-2">
              <button type="button" aria-label="Previous" onClick={() => onStep(-1)} className="h-8 w-8 rounded-md border border-lw-line-strong bg-lw-raised text-lw-text">←</button>
              <span className="font-lw-mono text-[11px] text-lw-faint">{position[0] + 1}/{position[1]}</span>
              <button type="button" aria-label="Next" onClick={() => onStep(1)} className="h-8 w-8 rounded-md border border-lw-line-strong bg-lw-raised text-lw-text">→</button>
            </div>
          )}
        </div>
      </div>
    </>
  )
}

function Counts({ tiles }: { tiles: DeckTile[] }) {
  const count = (state: TileState) => tiles.filter((tile) => tile.state === state).length
  const parts: Array<[TileState, string]> = [["spawning", "text-lw-live"], ["interrupted", "text-lw-text-2"], ["waiting", "text-lw-needs"], ["running", "text-lw-live"], ["done", "text-lw-done"], ["error", "text-lw-error"]]
  return (
    <ul aria-label="By state" className="m-0 flex list-none flex-wrap gap-x-3 p-0 text-xs">
      {parts.map(([state, tone]) => count(state) > 0 && (
        <li key={state} className={tone}><span aria-hidden="true">{looks[state].glyph}</span> {count(state)} {looks[state].label}</li>
      ))}
    </ul>
  )
}

function StateChip({ tile, now }: { tile: DeckTile; now: number }) {
  const look = looks[tile.state]
  const end = live(tile) ? now : tile.endedAt
  const time = tile.startedAt !== undefined && end !== undefined ? formatDuration(end - tile.startedAt) : ""
  return (
    <span className={cn("whitespace-nowrap rounded px-2 font-lw-mono text-[11px] uppercase", look.chip)}>
      <span aria-hidden="true">{look.glyph}</span> {look.label}{time && ` ${time}`}
    </span>
  )
}

// formatDuration says how long something ran: 42s, 1m 12s, 1h 3m.
function formatDuration(ms: number): string {
  const seconds = Math.max(0, Math.floor(ms / 1000))
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ${String(seconds % 60).padStart(2, "0")}s`
  return `${Math.floor(minutes / 60)}h ${minutes % 60}m`
}

// useNow is the time, kept current once a second while something is running.
function useNow(ticking: boolean): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!ticking) return
    setNow(Date.now())
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [ticking])
  return now
}

// useStickToBottom keeps a scrolling box at its end as it grows, unless the
// person has scrolled up to read.
function useStickToBottom(ref: React.RefObject<HTMLDivElement | null>, active: boolean) {
  const atEnd = useRef(true)
  useLayoutEffect(() => {
    const box = ref.current
    if (!active || !box) return
    if (atEnd.current) box.scrollTop = box.scrollHeight
  })
  useEffect(() => {
    const box = ref.current
    if (!active || !box) return
    const follow = () => { atEnd.current = box.scrollHeight - box.scrollTop - box.clientHeight < 40 }
    box.addEventListener("scroll", follow)
    return () => box.removeEventListener("scroll", follow)
  }, [ref, active])
}
