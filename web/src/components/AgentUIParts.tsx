import type { UIComponentItem } from "../lib/agentUi"
import { TONE_TEXT, tone } from "../lib/agentUiStyle"
import type { LightboxImage } from "./ImageLightbox"

// What drawing a component needs from the card it is part of.
export interface CardContext {
  onProposeMessage?: (text: string) => void
  // The place of an image among all the card's images, by where the image
  // is in the card; undefined when Legato has no copy of its file.
  picture: (slot: string) => number | undefined
  gallery: LightboxImage[]
  openPicture: (index: number) => void
}

export function Stat({ item }: { item: UIComponentItem }) {
  return (
    <div className="min-w-0 rounded-md border border-zinc-800 bg-zinc-950/40 px-3 py-2">
      {item.label && <div className="truncate text-[11px] uppercase tracking-wide text-zinc-500">{item.label}</div>}
      <div className={`break-words text-xl font-semibold leading-7 ${TONE_TEXT[tone(item.tone)]}`}>{item.value}</div>
      {item.detail && <div className="text-xs text-zinc-400">{item.detail}</div>}
    </div>
  )
}

// A picture the person can open full size. thumbnail crops it to fill its
// cell of a grid; otherwise it is shown whole.
export function Picture({ entry, at, card, thumbnail }: { entry: { alt?: string; caption?: string }; at: number | undefined; card: CardContext; thumbnail: boolean }) {
  const image = at === undefined ? undefined : card.gallery[at]
  if (at === undefined || !image) {
    return (
      <div className="rounded-md border border-dashed border-zinc-700 px-3 py-2 text-xs text-zinc-500">
        {entry.alt || "Image"}: this image is not stored any more.
      </div>
    )
  }
  return (
    <figure className="m-0 min-w-0">
      <button
        type="button"
        aria-label={`Show ${entry.alt || "image"} full size`}
        onClick={() => card.openPicture(at)}
        className="block max-w-full cursor-zoom-in overflow-hidden rounded-md border border-zinc-700 bg-[var(--theme-canvas,rgba(0,0,0,0.4))] focus-visible:outline focus-visible:outline-2 focus-visible:outline-indigo-400"
      >
        <img
          src={image.src}
          alt={entry.alt ?? ""}
          loading="lazy"
          className={thumbnail ? "aspect-[4/3] w-full object-cover" : "max-h-96 max-w-full object-contain"}
        />
      </button>
      {entry.caption && <figcaption className="mt-1 text-xs text-zinc-400">{entry.caption}</figcaption>}
    </figure>
  )
}

