import { Dialog, DialogPanel, DialogHeader, DialogTitle, DialogBody, DialogFooter } from "../ui/Dialog"
import { useEffect } from "react"
import { X } from "lucide-react"
import { providerItem, providerLabel } from "../../lib/providers"

interface OpenURLPickerProps {
  open: boolean
  providerURL: string
  // The task's provider ("jira", "burnlite", ...), which names its link.
  provider?: string
  prURL: string
  onSelect: (url: string) => void
  onClose: () => void
}

export function OpenURLPicker({ open, providerURL, provider = "", prURL, onSelect, onClose }: OpenURLPickerProps) {
  useEffect(() => {
    if (!open) return
    const handler = (e: KeyboardEvent) => {
      if (e.key === "j" && providerURL) {
        e.preventDefault()
        onSelect(providerURL)
      } else if (e.key === "g" && prURL) {
        e.preventDefault()
        onSelect(prURL)

      }
    }
    window.addEventListener("keydown", handler)
    return () => window.removeEventListener("keydown", handler)
  }, [open, providerURL, prURL, onSelect, onClose])

  if (!open) return null

  return (
    <Dialog open={open} onClose={onClose}>
      <DialogPanel className="w-full max-w-sm rounded-lg border border-zinc-700 bg-zinc-900 shadow-2xl">
        <DialogHeader className="flex items-center justify-between border-b border-zinc-800 px-5 py-3">
          <DialogTitle className="text-sm font-semibold text-zinc-200">Open Link</DialogTitle>
          <button aria-label="Close" onClick={onClose} className="rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200">
            <X size={16} />
          </button>
        </DialogHeader>
        <DialogBody className="px-5 py-4 space-y-2">
          {providerURL && (
            <button
              onClick={() => onSelect(providerURL)}
              className="w-full rounded border border-zinc-700 px-3 py-2 text-left text-xs text-zinc-300 transition-colors hover:bg-zinc-800 hover:text-zinc-100"
            >
              <kbd className="mr-2 rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] font-bold text-[var(--theme-focus,#AFA9EC)]">j</kbd>
              {provider ? `${providerLabel(provider)} ${providerItem(provider)}` : "Provider URL"}
              <div className="mt-1 truncate text-[10px] text-zinc-600">{providerURL}</div>
            </button>
          )}
          {prURL && (
            <button
              onClick={() => onSelect(prURL)}
              className="w-full rounded border border-zinc-700 px-3 py-2 text-left text-xs text-zinc-300 transition-colors hover:bg-zinc-800 hover:text-zinc-100"
            >
              <kbd className="mr-2 rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] font-bold text-[var(--theme-focus,#AFA9EC)]">g</kbd>
              GitHub PR
              <div className="mt-1 truncate text-[10px] text-zinc-600">{prURL}</div>
            </button>
          )}
        </DialogBody>
        <DialogFooter className="border-t border-zinc-800 px-5 py-2 text-[10px] text-zinc-600">esc = cancel</DialogFooter>
      </DialogPanel>
    </Dialog>
  )
}
