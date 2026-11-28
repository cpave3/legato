import { useEffect, useState, type ReactNode } from "react"
import { Zap } from "lucide-react"
import { useServer } from "../../hooks/useServer"
import { useMacroPicker, type PickerMacro } from "../../hooks/useMacroPicker"
import { fetchMacroList, type Macro, type PickerOptions } from "../../lib/macros"
import { cn } from "../../lib/utils"
import { Dropdown } from "../ui/Dropdown"

// The one macro picker of the web UI (LEG-619): the message box's menu,
// Resume agent… on a waiting merge, and the card menu's macros all list
// their macros through useMacroPicker and draw them with these rows, so
// each shows the same macros in the same order, favourites first with the
// rest behind Show all. A caller says which targets its macros must apply
// to; the message box, which sends to an agent, names none.

interface MacroPickerItemsProps<T extends PickerMacro> {
  shown: T[]
  hidden: number
  onShowAll: () => void
  onPick: (macro: T) => void
  // Number the first nine rows: only where the keys 1–9 pick them (the
  // message box's useMacroHotkeys).
  numbered?: boolean
  // Rows of a role="menu": the Resume agent dropdown and the card menu.
  menuitem?: boolean
  // Rows a finger can press, for a menu of such rows (the card menu).
  touch?: boolean
  // The row's accessible name, when it is not the macro's name.
  label?: (macro: T) => string
  // A second line under the name, such as the agent and tier it starts.
  detail?: (macro: T) => string
  // What shows when there is nothing to list; null shows nothing.
  empty?: ReactNode
}

// MacroPickerItems is a picker's rows: each shown macro, and a Show all row
// while some are hidden.
export function MacroPickerItems<T extends PickerMacro>({ shown, hidden, onShowAll, onPick, numbered = true, menuitem = false, touch = false, label, detail, empty = "(no macros configured)" }: MacroPickerItemsProps<T>) {
  if (shown.length === 0 && hidden === 0) {
    return empty === null ? null : <div className="px-3 py-1.5 text-xs text-lw-text-2">{empty}</div>
  }
  const role = menuitem ? "menuitem" : undefined
  const size = touch ? "min-h-10 rounded py-1 text-sm focus:bg-lw-raised focus:outline-none" : "py-1.5 text-xs"
  return (
    <>
      {shown.map((macro, index) => {
        const shortcut = numbered && index < 9 ? String(index + 1) : undefined
        const line = detail?.(macro)
        return (
          <button
            key={macro.name}
            type="button"
            role={role}
            aria-label={label?.(macro)}
            onClick={() => onPick(macro)}
            aria-keyshortcuts={shortcut}
            className={cn("flex w-full items-center gap-2 px-3 text-left text-lw-text transition-colors hover:bg-lw-raised", size)}
          >
            {shortcut && <kbd className="w-4 rounded border border-lw-line-strong text-center font-mono text-[10px] text-lw-text-2">{shortcut}</kbd>}
            <Zap size={12} className="shrink-0" />
            {line
              ? <span className="min-w-0"><span className="block truncate">{macro.name}</span><span className="block truncate text-[11px] text-lw-text-2">{line}</span></span>
              : macro.name}
          </button>
        )
      })}
      {hidden > 0 && (
        <button
          type="button"
          role={role}
          onClick={onShowAll}
          className={cn("mt-1 flex w-full items-center border-t border-lw-line px-3 pb-1.5 pt-2 text-lw-text-2 transition-colors hover:bg-lw-raised hover:text-lw-text", touch ? "min-h-10 text-sm focus:bg-lw-raised focus:outline-none" : "text-xs")}
        >
          Show all ({hidden} more)
        </button>
      )}
    </>
  )
}

// MacroPickerList is a picker that is open while it is drawn, such as a
// list in the card menu: its own Show all, from the macros and options.
export function MacroPickerList<T extends PickerMacro>({ macros, targets, suggested, ...rows }: { macros: T[] } & PickerOptions & Omit<MacroPickerItemsProps<T>, "shown" | "hidden" | "onShowAll">) {
  const { shown, hidden, showAll } = useMacroPicker(macros, true, { targets, suggested })
  return <MacroPickerItems shown={shown} hidden={hidden} onShowAll={showAll} {...rows} />
}

// MacroPickerButton is a button that opens the picker in a menu and gives
// the macro chosen: Resume agent on a waiting merge, from its needs-you row,
// the Attention page and the run page (LEG-614). The macros are read each
// time it opens: they change without a restart.
export function MacroPickerButton({ label, onPick, targets, suggested, disabled, className }: {
  label: string
  onPick: (macro: Macro) => void
  disabled?: boolean
  className?: string
} & PickerOptions) {
  const { baseUrl } = useServer()
  const [open, setOpen] = useState(false)
  const [macros, setMacros] = useState<Macro[]>([])
  const [loaded, setLoaded] = useState(false)
  const [error, setError] = useState("")
  useEffect(() => {
    if (!open) return
    let current = true
    setError("")
    setLoaded(false)
    fetchMacroList(baseUrl).then(
      list => { if (current) { setMacros(list.macros); setLoaded(true) } },
      reason => { if (current) setError(reason instanceof Error ? reason.message : String(reason)) },
    )
    return () => { current = false }
  }, [baseUrl, open])
  const { shown, hidden, showAll } = useMacroPicker(macros, open, { targets, suggested })
  return (
    <Dropdown open={open} onOpenChange={setOpen} align="left" minWidth={200} panelClassName="z-50"
      trigger={<button type="button" disabled={disabled} className={className} onClick={() => setOpen(!open)}>{label}…</button>}>
      {error && <p role="alert" className="px-3 py-1.5 text-xs text-lw-error">{error}</p>}
      {!error && loaded && (
        <MacroPickerItems shown={shown} hidden={hidden} onShowAll={showAll} numbered={false} menuitem empty="(no macros)"
          onPick={macro => { setOpen(false); onPick(macro) }} />
      )}
    </Dropdown>
  )
}
