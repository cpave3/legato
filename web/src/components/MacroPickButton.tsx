import { useEffect, useState } from "react"
import { Zap } from "lucide-react"
import { useServer } from "../hooks/useServer"
import { useMacroPicker } from "../hooks/useMacroPicker"
import { fetchMacroList, macrosFor, type Macro } from "../lib/macros"
import { Dropdown } from "./ui/Dropdown"

// MacroPickButton is a button that opens the macros, favourites first with
// the rest behind Show all, and calls onPick with the one chosen: Resume
// agent on a waiting merge, from its needs-you row and the run page
// (LEG-614). The macros are read each time it opens: they change without a
// restart.
export function MacroPickButton({ label, onPick, disabled, className }: {
  label: string
  onPick: (name: string) => void
  disabled?: boolean
  className?: string
}) {
  const { baseUrl } = useServer()
  const [open, setOpen] = useState(false)
  const [macros, setMacros] = useState<Macro[]>([])
  const [error, setError] = useState("")
  useEffect(() => {
    if (!open) return
    let current = true
    setError("")
    fetchMacroList(baseUrl).then(
      list => { if (current) setMacros(macrosFor(list.macros, "pr")) },
      reason => { if (current) setError(reason instanceof Error ? reason.message : String(reason)) },
    )
    return () => { current = false }
  }, [baseUrl, open])
  const { shown, hidden, showAll } = useMacroPicker(macros, open)
  const item = "flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs text-lw-text transition-colors hover:bg-lw-raised"
  return (
    <Dropdown open={open} onOpenChange={setOpen} align="left" minWidth={200} panelClassName="z-50"
      trigger={<button type="button" disabled={disabled} className={className} onClick={() => setOpen(!open)}>{label}…</button>}>
      {error && <p role="alert" className="px-3 py-1.5 text-xs text-lw-error">{error}</p>}
      {!error && shown.length === 0 && hidden === 0 && <p className="px-3 py-1.5 text-xs text-lw-text-2">(no macros)</p>}
      {shown.map(macro => (
        <button key={macro.name} type="button" role="menuitem" className={item}
          onClick={() => { setOpen(false); onPick(macro.name) }}>
          <Zap size={12} aria-hidden="true" />{macro.name}
        </button>
      ))}
      {hidden > 0 && (
        <button type="button" role="menuitem" onClick={showAll}
          className="mt-1 flex w-full items-center border-t border-lw-line px-3 pb-1.5 pt-2 text-xs text-lw-text-2 hover:bg-lw-raised hover:text-lw-text">
          Show all ({hidden} more)
        </button>
      )}
    </Dropdown>
  )
}
