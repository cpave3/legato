import { useEffect } from "react"
import { X } from "lucide-react"

interface OpenURLPickerProps {
  open: boolean
  providerURL: string
  prURL: string
  onSelect: (url: string) => void
  onClose: () => void
}

export function OpenURLPicker({ open, providerURL, prURL, onSelect, onClose }: OpenURLPickerProps) {
  useEffect(() => {
    if (!open) return
    const handler = (e: KeyboardEvent) => {
      if (e.key === "j" && providerURL) {
        e.preventDefault()
        onSelect(providerURL)
      } else if (e.key === "g" && prURL) {
        e.preventDefault()
        onSelect(prURL)
      } else if (e.key === "Escape") {
        e.preventDefault()
        onClose()
      }
    }
    window.addEventListener("keydown", handler)
    return () => window.removeEventListener("keydown", handler)
  }, [open, providerURL, prURL, onSelect, onClose])

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" onClick={onClose}>
      <div className="w-full max-w-sm rounded-lg border border-zinc-700 bg-zinc-900 shadow-2xl" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between border-b border-zinc-800 px-5 py-3">
          <h2 className="text-sm font-semibold text-zinc-200">Open Link</h2>
          <button onClick={onClose} className="rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200">
            <X size={16} />
          </button>
        </div>
        <div className="px-5 py-4 space-y-2">
          {providerURL && (
            <button
              onClick={() => onSelect(providerURL)}
              className="w-full rounded border border-zinc-700 px-3 py-2 text-left text-xs text-zinc-300 transition-colors hover:bg-zinc-800 hover:text-zinc-100"
            >
              <kbd className="mr-2 rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] font-bold text-[#AFA9EC]">j</kbd>
              Jira / Provider URL
              <div className="mt-1 truncate text-[10px] text-zinc-600">{providerURL}</div>
            </button>
          )}
          {prURL && (
            <button
              onClick={() => onSelect(prURL)}
              className="w-full rounded border border-zinc-700 px-3 py-2 text-left text-xs text-zinc-300 transition-colors hover:bg-zinc-800 hover:text-zinc-100"
            >
              <kbd className="mr-2 rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] font-bold text-[#AFA9EC]">g</kbd>
              GitHub PR
              <div className="mt-1 truncate text-[10px] text-zinc-600">{prURL}</div>
            </button>
          )}
        </div>
        <div className="border-t border-zinc-800 px-5 py-2 text-[10px] text-zinc-600">esc = cancel</div>
      </div>
    </div>
  )
}
