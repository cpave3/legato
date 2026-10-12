import { useEffect, useRef, useState } from "react"
import { createPortal } from "react-dom"
import { ChevronLeft, ChevronRight, X } from "lucide-react"

export interface LightboxImage {
  src: string
  alt: string
  caption?: string
}

interface ImageLightboxProps {
  images: LightboxImage[]
  // Which of the images is shown.
  index: number
  onIndex: (index: number) => void
  onClose: () => void
}

// ImageLightbox shows one image over the whole window, with the others it
// came with to step through. The image is fitted to the window; a click on it
// switches to its own size, which scrolls.
export function ImageLightbox({ images, index, onIndex, onClose }: ImageLightboxProps) {
  const dialogRef = useRef<HTMLDivElement>(null)
  // The size the person chose, and for which image: the next one starts
  // fitted again.
  const [actual, setActual] = useState<number | null>(null)
  const image = images[index]
  const several = images.length > 1
  const size = actual === index ? "actual" : "fit"

  // The keyboard goes to the dialog while it is open and back to where it
  // was afterwards.
  useEffect(() => {
    const before = document.activeElement
    dialogRef.current?.focus()
    return () => {
      if (before instanceof HTMLElement) before.focus()
    }
  }, [])

  if (!image) return null
  const name = image.alt || `Image ${index + 1}`
  const step = (by: number) => onIndex((index + by + images.length) % images.length)
  const stepButton = "absolute top-1/2 z-10 flex h-11 w-11 -translate-y-1/2 items-center justify-center rounded-full bg-zinc-900/80 text-zinc-200 transition-colors hover:bg-zinc-800 hover:text-white"

  // Drawn at the top of the page: inside a card or a panel it could be cut
  // off by what holds it.
  return createPortal(
    <div
      ref={dialogRef}
      role="dialog"
      aria-modal="true"
      aria-label={`${name}, full size`}
      tabIndex={-1}
      className="fixed inset-0 z-[60] flex flex-col bg-black/90 outline-none"
      // index.css keeps every dialog a margin away from a phone's screen
      // edges, which does not suit an image the person asked to see whole.
      style={{ maxWidth: "none", maxHeight: "none", paddingTop: "env(safe-area-inset-top)", paddingBottom: "env(safe-area-inset-bottom)" }}
      onKeyDown={(event) => {
        // Handled here and no further: a sheet behind this would close on
        // the same Escape.
        event.stopPropagation()
        if (event.key === "Escape") onClose()
        else if (several && event.key === "ArrowRight") step(1)
        else if (several && event.key === "ArrowLeft") step(-1)
      }}
    >
      <div className="flex shrink-0 items-center gap-3 px-4 py-2 text-sm text-zinc-200">
        <span className="min-w-0 flex-1 truncate">{image.caption || image.alt}</span>
        {several && <span className="shrink-0 text-xs tabular-nums text-zinc-400">{index + 1} of {images.length}</span>}
        <button type="button" aria-label="Close image" className="flex h-11 w-11 shrink-0 items-center justify-center rounded text-zinc-400 hover:text-white" onClick={onClose}>
          <X className="h-5 w-5" />
        </button>
      </div>
      <div className="relative min-h-0 flex-1">
        <div
          data-testid="lightbox-stage"
          className={`h-full w-full overflow-auto p-4 ${size === "fit" ? "flex items-center justify-center" : ""}`}
          onClick={(event) => { if (event.target === event.currentTarget) onClose() }}
        >
          <img
            src={image.src}
            alt={name}
            data-size={size}
            onClick={() => setActual(size === "fit" ? index : null)}
            className={size === "fit" ? "max-h-full max-w-full cursor-zoom-in object-contain" : "mx-auto max-w-none cursor-zoom-out"}
          />
        </div>
        {several && (
          <>
            <button type="button" aria-label="Previous image" className={`${stepButton} left-2`} onClick={() => step(-1)}>
              <ChevronLeft className="h-6 w-6" />
            </button>
            <button type="button" aria-label="Next image" className={`${stepButton} right-2`} onClick={() => step(1)}>
              <ChevronRight className="h-6 w-6" />
            </button>
          </>
        )}
      </div>
    </div>,
    document.body,
  )
}
