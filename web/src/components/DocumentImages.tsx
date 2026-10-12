import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from "react"
import type { ItemImage } from "../lib/agentUi"
import { findDocumentImage, isRemoteImage } from "../lib/documentImages"
import { ImageLightbox, type LightboxImage } from "./ImageLightbox"

interface DocumentImagesValue {
  images: ItemImage[]
  // Opens the picture an image element shows, with the document's others.
  open: (element: HTMLImageElement) => void
}

const DocumentImagesContext = createContext<DocumentImagesValue | null>(null)

// DocumentImages is the frame around Markdown that shows image files of a
// plan or a review: it knows where Legato's copy of each is, and shows one
// over the whole window when the person opens it.
export function DocumentImages({ images, children }: { images: ItemImage[]; children: ReactNode }) {
  const container = useRef<HTMLDivElement>(null)
  const [shown, setShown] = useState<{ images: LightboxImage[]; index: number } | null>(null)

  const open = useCallback((element: HTMLImageElement) => {
    // Every image of the document, in the order it shows them.
    const drawn = [...(container.current?.querySelectorAll<HTMLImageElement>("img[data-document-image]") ?? [])]
    const index = drawn.indexOf(element)
    if (index >= 0) setShown({ images: drawn.map((image) => ({ src: image.src, alt: image.alt })), index })
  }, [])
  const value = useMemo(() => ({ images, open }), [images, open])

  return (
    <DocumentImagesContext.Provider value={value}>
      <div ref={container}>{children}</div>
      {shown && (
        <ImageLightbox images={shown.images} index={shown.index} onIndex={(index) => setShown({ images: shown.images, index })} onClose={() => setShown(null)} />
      )}
    </DocumentImagesContext.Provider>
  )
}

// DocumentImage draws one image of Markdown inside DocumentImages. A file
// the document names is drawn from Legato's copy and opens full size. An
// image at a web address is left to the browser. A file with no copy is
// named by its alt text: a broken picture says nothing.
export function DocumentImage({ src, alt }: { src?: string; alt?: string }) {
  const context = useContext(DocumentImagesContext)
  const kept = context ? findDocumentImage(context.images, src) : undefined
  if (kept && context) {
    return (
      <button
        type="button"
        aria-label={`Show ${alt || "image"} full size`}
        onClick={(event) => {
          const image = event.currentTarget.querySelector("img")
          if (image) context.open(image)
        }}
        className="my-2 inline-block max-w-full cursor-zoom-in overflow-hidden rounded-md border border-zinc-700 bg-black/20 p-0 align-top focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-400"
      >
        {/* Kept to a size that leaves the text around it in view: a tall
            screenshot would push a chapter's diff off the screen. The whole
            image is one click away. */}
        <img data-document-image src={kept.address} alt={alt ?? ""} loading="lazy" className="block h-auto max-h-96 w-auto max-w-full" />
      </button>
    )
  }
  if (isRemoteImage(src)) return <img src={src} alt={alt ?? ""} className="max-w-full" />
  return <span className="rounded border border-dashed border-zinc-700 px-2 py-0.5 text-xs text-zinc-500">{alt || src || "Image"} (this image is not stored)</span>
}
