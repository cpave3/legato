import { useState, type ReactNode } from "react"
import { useApprovalEntries } from "./ApprovalEntries"

// The Legato widget's feed, under the voice: what needs the person while
// the desktop app is not in front. Each kind of item is a source of
// entries (approvals, ApprovalEntries.tsx, are the first); the feed puts them
// newest first, opens one at a time, and counts those still waiting. It is
// the root's one element that scrolls (data-widget-feed, widget.css): the
// capsule or the card above it keeps its size, and the feed gets the
// height that is left under the widget's maximum. Rust keeps the widget
// shown while any item waits.

// One item in the feed: its kind, a key unique in the feed, when it came
// (ms), whether it still waits for the person, and its row, which draws
// its one line and, opened, its detail and actions.
export interface FeedEntry {
  kind: string
  key: string
  at: number
  waiting: boolean
  render: (p: { open: boolean; onToggle: () => void }) => ReactNode
}

// newestFirst orders entries by when they came, newest first; ties keep
// their order.
export function newestFirst(entries: FeedEntry[]): FeedEntry[] {
  return entries.map((e, i) => ({ e, i })).sort((a, b) => b.e.at - a.e.at || a.i - b.i).map(({ e }) => e)
}

export function Feed() {
  // Each kind's entries. A later kind adds its hook here.
  const entries = newestFirst([...useApprovalEntries()])
  const [open, setOpen] = useState<string | null>(null)
  if (!entries.length) return null
  const waiting = entries.filter((e) => e.waiting).length
  return (
    <section
      data-testid="widget-feed"
      data-widget-feed
      data-no-drag
      aria-label="Legato widget feed"
      // A press never moves the focus, so typing elsewhere carries on.
      onMouseDown={(e) => e.preventDefault()}
      className="voice-ui w-[412px] max-w-full rounded-[20px] border border-[#E8B53A]/40 shadow-[0_18px_48px_rgba(0,0,0,0.5)]"
      style={{ background: "var(--voice-surface)" }}
    >
      <header className="sticky top-0 z-10 flex items-center gap-2 rounded-t-[20px] border-b border-[#26262D] bg-[#141319] px-[18px] py-2.5">
        <span className="text-[#E8B53A]" aria-hidden="true">◆</span>
        <span className="voice-kicker !text-[#E8B53A]">{waiting ? `${waiting} ${waiting === 1 ? "needs" : "need"} you` : "done"}</span>
      </header>
      <ul className="m-0 flex list-none flex-col p-1.5">
        {entries.map((e) => (
          <li key={`${e.kind}-${e.key}`} data-testid={`feed-item-${e.kind}-${e.key}`} data-kind={e.kind}>
            {e.render({ open: open === `${e.kind}-${e.key}`, onToggle: () => setOpen((o) => (o === `${e.kind}-${e.key}` ? null : `${e.kind}-${e.key}`)) })}
          </li>
        ))}
      </ul>
    </section>
  )
}
