import { useEffect, useState } from "react"
import { Image as ImageIcon, X } from "lucide-react"
import { useServer } from "../hooks/useServer"
import { fetchAgentImage } from "../lib/agentImage"

interface ReadImageRowProps {
  agentId: string
  // The path the agent read, as it wrote it.
  path: string
  // Whether the row is one of several inside a bordered group.
  grouped: boolean
}

type Shown = { address: string } | { error: string }

// ReadImageRow is the conversation's row for an image file the agent read.
// The agent saw the image; the person sees only its name until they open the
// row, which asks Legato's server for the file. Nothing is fetched before.
export function ReadImageRow({ agentId, path, grouped }: ReadImageRowProps) {
  const { baseUrl } = useServer()
  const [open, setOpen] = useState(false)
  const [large, setLarge] = useState(false)
  const [shown, setShown] = useState<Shown | null>(null)
  const filename = path.split("/").pop() || path

  // The file is read again each time the row opens: a screenshot is often
  // written again under the same name.
  useEffect(() => {
    if (!open) return
    let current = true
    let address = ""
    fetchAgentImage(baseUrl, agentId, path).then(
      (blob) => {
        if (!current) return
        address = URL.createObjectURL(blob)
        setShown({ address })
      },
      (error: unknown) => {
        if (current) setShown({ error: error instanceof Error ? error.message : String(error) })
      },
    )
    return () => {
      current = false
      if (address) URL.revokeObjectURL(address)
      setShown(null)
      setLarge(false)
    }
  }, [open, baseUrl, agentId, path])

  useEffect(() => {
    if (!large) return
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setLarge(false)
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [large])

  return (
    <div className="text-xs text-zinc-400">
      <button
        aria-expanded={open}
        className={`flex w-full items-center gap-2 text-left transition-colors hover:text-zinc-200 ${grouped ? "px-3 py-1" : "px-1 py-0.5"}`}
        onClick={() => setOpen(!open)}
      >
        <span className="rounded bg-blue-700/40 px-1.5 py-0.5 font-mono text-white">read</span>
        <ImageIcon aria-hidden className="h-3.5 w-3.5 shrink-0 text-blue-300" />
        <span className="min-w-0 truncate font-mono">{filename}</span>
        <span className="ml-auto shrink-0 text-zinc-500">{open ? "▼" : "▶"}</span>
      </button>
      {open && (
        <div className={grouped ? "px-3 pb-2" : "px-1 pb-1 pt-1"}>
          {path !== filename && <div className="mb-1 break-all text-zinc-500">{path}</div>}
          {shown === null && <div className="text-zinc-500">Loading image…</div>}
          {shown && "error" in shown && <div role="alert" className="text-red-400">Cannot show this image: {shown.error}</div>}
          {shown && "address" in shown && (
            <button aria-label={`Show ${filename} full size`} className="block max-w-full cursor-zoom-in" onClick={() => setLarge(true)}>
              <img src={shown.address} alt={filename} className="max-h-96 max-w-full rounded border border-zinc-700 bg-black/40 object-contain" />
            </button>
          )}
        </div>
      )}
      {large && shown && "address" in shown && (
        // The global rule that keeps dialogs inside a margin on a phone does
        // not suit an image the person asked to see whole.
        <div
          role="dialog"
          aria-modal="true"
          aria-label={`${filename}, full size`}
          className="fixed inset-0 z-50 flex cursor-zoom-out flex-col bg-black/90"
          style={{ maxWidth: "none", maxHeight: "none" }}
          onClick={() => setLarge(false)}
        >
          <div className="flex shrink-0 items-center gap-3 px-4 py-2 text-xs text-zinc-300">
            <span className="min-w-0 truncate font-mono">{path}</span>
            <button aria-label="Close image" className="ml-auto rounded p-1 text-zinc-400 hover:text-white" onClick={() => setLarge(false)}>
              <X className="h-4 w-4" />
            </button>
          </div>
          <div className="min-h-0 flex-1 overflow-auto p-4">
            <img src={shown.address} alt={`${filename}, full size`} className="mx-auto max-w-none" />
          </div>
        </div>
      )}
    </div>
  )
}
