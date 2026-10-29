import type { ReactNode } from "react"
import type { LucideIcon } from "lucide-react"

// The parts the Settings page is built from: groups of cards, cards of rows.

// A group: one subject, with a heading the page's section list goes to.
export function SettingsGroup({ id, title, description, children }: { id: string; title: string; description: string; children: ReactNode }) {
  return (
    <section id={id} aria-labelledby={`${id}-title`} className="scroll-mt-6">
      <div className="mb-3">
        <h2 id={`${id}-title`} className="text-base font-semibold text-lw-text">{title}</h2>
        <p className="mt-0.5 text-sm text-lw-text-2">{description}</p>
      </div>
      <div className="flex flex-col gap-4">{children}</div>
    </section>
  )
}

// Marks a setting kept in this browser only, not on the server.
export function BrowserTag() {
  return (
    <span className="rounded border border-lw-line px-1.5 py-px font-lw-mono text-[10px] uppercase tracking-wide text-lw-faint" title="Saved in this browser only">
      this browser
    </span>
  )
}

// A card: a titled block inside a group. With rows, its body is a list.
export function SettingsCard({
  id, icon: Icon, title, description, browser, label, children,
}: {
  id?: string
  icon: LucideIcon
  title: string
  description?: ReactNode
  browser?: boolean
  // The card's accessible name when it is a landmark of its own.
  label?: string
  children?: ReactNode
}) {
  return (
    <section id={id} aria-label={label} className="scroll-mt-6 overflow-hidden rounded-xl border border-lw-line bg-lw-surface">
      <header className="flex items-start gap-3 px-4 pt-4 pb-3">
        <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-lw-raised text-lw-link">
          <Icon size={16} />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-sm font-medium text-lw-text">{title}</h3>
            {browser && <BrowserTag />}
          </div>
          {description && <div className="mt-0.5 text-xs leading-relaxed text-lw-text-2">{description}</div>}
        </div>
      </header>
      {children && <div className="px-4 pb-4">{children}</div>}
    </section>
  )
}

// A list of rows inside a card.
export function SettingsRows({ children }: { children: ReactNode }) {
  return <div className="overflow-hidden rounded-xl border border-lw-line bg-lw-surface divide-y divide-lw-line">{children}</div>
}

// One setting: what it is on the left, its control on the right.
export function SettingsRow({ icon: Icon, title, description, browser, control }: { icon: LucideIcon; title: string; description: string; browser?: boolean; control: ReactNode }) {
  return (
    <div className="flex min-h-16 items-center gap-3 px-4 py-3">
      <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-lw-raised text-lw-link">
        <Icon size={16} />
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <p className="text-sm text-lw-text">{title}</p>
          {browser && <BrowserTag />}
        </div>
        <p className="mt-0.5 text-xs text-lw-text-2">{description}</p>
      </div>
      <div className="shrink-0">{control}</div>
    </div>
  )
}

export function Toggle({ label, on, onChange }: { label: string; on: boolean; onChange: () => void }) {
  return (
    <button
      type="button"
      aria-label={label}
      aria-pressed={on}
      onClick={onChange}
      className={`relative h-6 w-11 rounded-full transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-lw-accent ${on ? "bg-lw-accent" : "bg-lw-line-strong"}`}
    >
      <span className={`absolute top-0.5 left-0.5 h-5 w-5 rounded-full bg-white transition-transform ${on ? "translate-x-5" : "translate-x-0"}`} />
    </button>
  )
}

// The page's primary button style.
export const primaryButton =
  "inline-flex min-h-10 items-center justify-center gap-2 rounded-lg bg-lw-accent px-3.5 text-sm font-medium text-white transition-colors hover:bg-[#7c6ef7] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-lw-accent-ring disabled:opacity-50"
export const quietButton =
  "inline-flex min-h-10 items-center justify-center gap-2 rounded-lg border border-lw-line-strong px-3.5 text-sm text-lw-text transition-colors hover:bg-lw-raised focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-lw-accent-ring disabled:opacity-50"
