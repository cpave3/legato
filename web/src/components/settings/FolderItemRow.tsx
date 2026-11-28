import type { ReactNode } from "react"
import { Toggle } from "./SettingsUI"

// FolderItemRow is one thing Legato found in a folder on disk (a skill, a
// managed hook): its name, what it says (children) or its error marked
// error, its path, and a toggle "Give agents <name>" unless it has an error.
export function FolderItemRow({ name, error, path, enabled, busy, onToggle, children }: {
  name: string
  error?: string
  path: string
  enabled: boolean
  busy: boolean
  onToggle: () => void
  children: ReactNode
}) {
  return (
    <li aria-label={name} className="flex items-start gap-3 px-3 py-2.5">
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-lw-mono text-sm text-lw-text">{name}</span>
          {error && <span className="rounded border border-lw-line px-1.5 py-px font-lw-mono text-[10px] uppercase tracking-wide text-lw-error">error</span>}
        </div>
        {error ? <p className="mt-0.5 text-xs text-lw-error">{error}</p> : children}
        <p className="mt-0.5 truncate font-lw-mono text-[11px] text-lw-faint" title={path}>{path}</p>
      </div>
      {!error && <Toggle label={`Give agents ${name}`} on={enabled} disabled={busy} onChange={onToggle} />}
    </li>
  )
}
