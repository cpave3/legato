import { X } from "lucide-react"
import type { PastedImages } from "../hooks/usePastedImages"

// PastedImageStrip shows, above a message box, the images the person pasted
// into the message: a thumbnail for each path still in it, the ones on
// their way, and why one could not be taken.
export function PastedImageStrip({ pasted }: { pasted: Pick<PastedImages, "attached" | "uploading" | "error" | "dismissError"> }) {
  const { attached, uploading, error, dismissError } = pasted
  if (attached.length === 0 && uploading === 0 && !error) return null
  return (
    <div className="mb-1.5 flex min-w-0 flex-wrap items-center gap-2 text-xs text-zinc-400">
      {attached.map((image) => {
        const name = image.path.split("/").pop() || image.path
        return (
          <span key={image.path} title={image.path} className="flex min-w-0 items-center gap-1.5 rounded border border-zinc-700 bg-zinc-900 py-0.5 pl-0.5 pr-2">
            <img src={image.address} alt={`Pasted image ${name}`} className="h-8 w-8 shrink-0 rounded-sm object-cover" />
            <span className="min-w-0 truncate font-mono text-[11px]">{name}</span>
          </span>
        )
      })}
      {uploading > 0 && <span role="status">Adding {uploading === 1 ? "image" : `${uploading} images`}…</span>}
      {error && (
        <span role="alert" className="flex min-w-0 items-center gap-1 text-red-400">
          <span className="min-w-0">Image not added: {error}</span>
          <button aria-label="Dismiss" className="shrink-0 rounded p-0.5 hover:text-red-200" onClick={dismissError}><X className="h-3 w-3" /></button>
        </span>
      )}
    </div>
  )
}
