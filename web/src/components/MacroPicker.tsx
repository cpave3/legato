import { Zap } from "lucide-react"
import type { PickerMacro } from "../hooks/useMacroPicker"

// MacroPickerItems is the menu's rows: each shown macro, numbered 1–9, and
// a Show all row while some are hidden.
export function MacroPickerItems({ shown, hidden, onShowAll, onPick }: {
  shown: PickerMacro[]
  hidden: number
  onShowAll: () => void
  onPick: (keys: string) => void
}) {
  if (shown.length === 0 && hidden === 0) {
    return <div className="px-3 py-1.5 text-xs text-lw-text-2">(no macros configured)</div>
  }
  return (
    <>
      {shown.map((macro, index) => {
        const shortcut = index < 9 ? String(index + 1) : undefined
        return (
          <button
            key={macro.name}
            type="button"
            onClick={() => onPick(macro.keys)}
            aria-keyshortcuts={shortcut}
            className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-lw-text transition-colors hover:bg-lw-raised"
          >
            {shortcut && <kbd className="w-4 rounded border border-lw-line-strong text-center font-mono text-[10px] text-lw-text-2">{shortcut}</kbd>}
            <Zap size={12} />
            {macro.name}
          </button>
        )
      })}
      {hidden > 0 && (
        <button
          type="button"
          onClick={onShowAll}
          className="mt-1 flex w-full items-center border-t border-lw-line px-3 pb-1.5 pt-2 text-xs text-lw-text-2 transition-colors hover:bg-lw-raised hover:text-lw-text"
        >
          Show all ({hidden} more)
        </button>
      )}
    </>
  )
}
