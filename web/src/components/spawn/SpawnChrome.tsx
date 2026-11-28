import type { ReactNode, RefObject } from "react"
import { X } from "lucide-react"

interface SpawnSheetProps {
  // The dialog's accessible name.
  name: string
  formRef: RefObject<HTMLFormElement | null>
  // Starts the agent: on submit, and on ctrl+enter or cmd+enter anywhere.
  onStart: () => void
  // Closes it: a press on the backdrop.
  onClose: () => void
  children: ReactNode
}

// The backdrop and the form: a centered dialog on a wide screen, a sheet
// from the bottom on a phone.
export function SpawnSheet({ name, formRef, onStart, onClose, children }: SpawnSheetProps) {
  return (
    <div
      className="fixed inset-0 z-50 flex items-end justify-center bg-black/60 sm:items-center sm:p-4"
      onMouseDown={(event) => { if (event.target === event.currentTarget) onClose() }}
    >
      <form
        ref={formRef}
        tabIndex={-1}
        role="dialog"
        aria-modal="true"
        aria-label={name}
        onSubmit={(event) => { event.preventDefault(); onStart() }}
        onKeyDown={(event) => {
          if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) { event.preventDefault(); onStart() }
        }}
        // index.css keeps every dialog a margin away from a phone's screen
        // edges; a sheet reaches them, so maxWidth sets that aside.
        style={{ maxWidth: "none" }}
        className="flex max-h-[calc(100dvh-1rem)] w-full flex-col rounded-t-2xl border-t border-lw-line-strong bg-lw-surface font-lw text-[15px] outline-none leading-5 text-lw-text shadow-[0_24px_60px_rgba(0,0,0,.5)] sm:w-[580px] sm:rounded-xl sm:border sm:text-[13px]"
      >
        {children}
      </form>
    </div>
  )
}

// The sheet's handle on a phone, the heading, and the close button, which
// waits while an agent is being started.
export function SpawnHeader({ name, description, loading, onClose }: { name: string; description: string; loading: boolean; onClose: () => void }) {
  return (
    <>
      <div aria-hidden="true" className="flex justify-center pb-1 pt-2 sm:hidden">
        <span className="h-1 w-9 rounded-full bg-lw-line-strong" />
      </div>
      <div className="flex shrink-0 items-center justify-between py-1 pl-4 pr-2 sm:px-5 sm:pb-3 sm:pt-4">
        <div className="flex flex-col">
          <h2 className="text-lg font-semibold sm:text-base">{name}</h2>
          <p className="hidden text-xs text-lw-text-2 sm:block">{description}</p>
        </div>
        <button
          type="button"
          aria-label="Close"
          disabled={loading}
          onClick={onClose}
          className="flex h-11 w-11 items-center justify-center rounded-md text-lw-text-2 transition-colors hover:bg-lw-raised hover:text-lw-text disabled:opacity-40 sm:h-8 sm:w-8"
        >
          <X size={18} />
        </button>
      </div>
    </>
  )
}

// A failure to show above the fields; nothing when there is none.
export function ErrorNote({ message }: { message: string }) {
  if (!message) return null
  return (
    <div role="alert" className="rounded-md border border-lw-danger-line bg-lw-error-soft px-3 py-2 text-xs text-lw-danger">
      {message}
    </div>
  )
}

interface SpawnFooterProps {
  // The button's words, before and while the agent is started, and where
  // it starts ("" when it has no name).
  action: string
  busyLabel: string
  where: string
  // True while the agent is being started or the dialog is not ready.
  busy: boolean
  loading: boolean
  onClose: () => void
  // "Save path as preset": its display class, whether a path can be saved,
  // and whether it is ticked.
  saveVisibility: string
  canSave: boolean
  savePath: boolean
  setSavePath: (value: boolean) => void
}

const spawnKeys = () => /Mac|iPhone|iPad/.test(navigator.platform) ? "⌘↵" : "Ctrl ↵"

// The footer: save the typed path as a preset, cancel, and start. The
// start button names where on a phone and its keys on a wide screen.
export function SpawnFooter({ action, busyLabel, where, busy, loading, onClose, saveVisibility, canSave, savePath, setSavePath }: SpawnFooterProps) {
  const named = where ? `${action} in ${where}` : action
  return (
    <div className="flex shrink-0 flex-col gap-3 border-t border-lw-line px-4 pb-[max(0.75rem,env(safe-area-inset-bottom))] pt-3 sm:flex-row sm:items-center sm:gap-2 sm:px-5 sm:pb-3">
      <label className={`${saveVisibility} items-center gap-2 text-xs text-lw-text-2 ${canSave ? "" : "opacity-50"}`}>
        <input
          type="checkbox"
          checked={savePath && canSave}
          disabled={!canSave}
          onChange={(event) => setSavePath(event.target.checked)}
          className="accent-lw-accent"
        />
        Save path as preset
      </label>
      <span className="hidden flex-1 sm:block" />
      <button
        type="button"
        disabled={loading}
        onClick={onClose}
        className="hidden h-[34px] rounded-md px-3.5 text-[13px] text-lw-text-2 transition-colors hover:text-lw-text disabled:opacity-40 sm:block"
      >
        Cancel
      </button>
      <button
        type="submit"
        disabled={busy}
        aria-label={named}
        className="h-[52px] w-full rounded-xl bg-lw-accent px-4 text-base font-semibold text-[var(--theme-canvas,#fff)] transition-opacity hover:opacity-90 disabled:opacity-40 sm:h-[34px] sm:w-auto sm:rounded-md sm:text-[13px] sm:font-medium"
      >
        <span className="sm:hidden">{busy ? busyLabel : named}</span>
        <span className="hidden sm:inline">
          {busy ? busyLabel : <>{action} <span className="text-[var(--theme-canvas,#fff)]/75">{spawnKeys()}</span></>}
        </span>
      </button>
    </div>
  )
}
