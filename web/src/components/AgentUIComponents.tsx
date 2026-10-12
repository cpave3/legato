import { useMemo, useState, type ReactNode } from "react"
import type { ItemImage, UIComponent, UIComponentItem, UITone } from "../lib/agentUi"
import { ImageLightbox, type LightboxImage } from "./ImageLightbox"
import { MarkdownBody } from "./MarkdownBody"

interface AgentUIComponentsProps {
  components: UIComponent[]
  // An action button was pressed: its message is for the person's message
  // box. Without a handler, actions are not drawn.
  onProposeMessage?: (text: string) => void
  // Where Legato's copy of each image file is, by the path the agent named.
  images?: ItemImage[]
}

// What drawing a component needs from the card it is part of.
interface CardContext {
  onProposeMessage: AgentUIComponentsProps["onProposeMessage"]
  // The place of an image among all the card's images, by where the image
  // is in the card; undefined when Legato has no copy of its file.
  picture: (slot: string) => number | undefined
  gallery: LightboxImage[]
  openPicture: (index: number) => void
}

const TONE_TEXT: Record<UITone, string> = {
  neutral: "text-zinc-100",
  positive: "text-emerald-300",
  warning: "text-amber-300",
  negative: "text-red-300",
  info: "text-sky-300",
}

const TONE_BADGE: Record<UITone, string> = {
  neutral: "border-zinc-600 bg-zinc-800 text-zinc-300",
  positive: "border-emerald-800 bg-emerald-950/60 text-emerald-300",
  warning: "border-amber-800 bg-amber-950/60 text-amber-300",
  negative: "border-red-800 bg-red-950/60 text-red-300",
  info: "border-sky-800 bg-sky-950/60 text-sky-300",
}

function tone(value: UITone | undefined): UITone {
  return value && value in TONE_TEXT ? value : "neutral"
}

function Stat({ item }: { item: UIComponentItem }) {
  return (
    <div className="min-w-0 rounded-md border border-zinc-800 bg-zinc-950/40 px-3 py-2">
      {item.label && <div className="truncate text-[11px] uppercase tracking-wide text-zinc-500">{item.label}</div>}
      <div className={`break-words text-xl font-semibold leading-7 ${TONE_TEXT[tone(item.tone)]}`}>{item.value}</div>
      {item.detail && <div className="text-xs text-zinc-400">{item.detail}</div>}
    </div>
  )
}

// A card's own "level 1" is the top of the card, not of the page: the
// conversation it sits in already has the page's headings above it.
const HEADING: Record<number, { Tag: "h2" | "h3" | "h4"; className: string }> = {
  1: { Tag: "h2", className: "text-base font-semibold text-zinc-100" },
  2: { Tag: "h3", className: "text-sm font-semibold text-zinc-100" },
  3: { Tag: "h4", className: "text-xs font-semibold uppercase tracking-wide text-zinc-400" },
}

// A picture the person can open full size. thumbnail crops it to fill its
// cell of a grid; otherwise it is shown whole.
function Picture({ entry, at, card, thumbnail }: { entry: { alt?: string; caption?: string }; at: number | undefined; card: CardContext; thumbnail: boolean }) {
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
        className="block max-w-full cursor-zoom-in overflow-hidden rounded-md border border-zinc-700 bg-black/40 focus-visible:outline focus-visible:outline-2 focus-visible:outline-indigo-400"
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

function renderComponent(component: UIComponent, at: number, card: CardContext): ReactNode {
  const { onProposeMessage } = card
  const items = component.items ?? []
  switch (component.type) {
    case "heading": {
      const { Tag, className } = HEADING[component.level ?? 2] ?? HEADING[2]
      return <Tag className={className}>{component.text}</Tag>
    }
    case "text":
      return (
        <div className={TONE_TEXT[tone(component.tone)] === TONE_TEXT.neutral ? "text-zinc-300" : TONE_TEXT[tone(component.tone)]}>
          <MarkdownBody body={component.text ?? ""} disableImages skipHtml />
        </div>
      )
    case "stat":
      return <Stat item={component} />
    case "stats":
      return (
        <div className="grid gap-2" style={{ gridTemplateColumns: "repeat(auto-fit, minmax(7.5rem, 1fr))" }}>
          {items.map((item, index) => <Stat key={index} item={item} />)}
        </div>
      )
    case "key_value":
      return (
        <dl className="grid grid-cols-[minmax(0,auto)_minmax(0,1fr)] gap-x-4 gap-y-1 text-sm">
          {items.map((item, index) => (
            <div key={index} className="contents">
              <dt className="text-zinc-500">{item.label}</dt>
              <dd className="break-words text-zinc-200">{item.value}</dd>
            </div>
          ))}
        </dl>
      )
    case "table":
      return (
        <div className="overflow-x-auto">
          <table className="w-full border-collapse text-left text-xs">
            <thead>
              <tr>{(component.columns ?? []).map((column, index) => <th key={index} scope="col" className="border-b border-zinc-700 px-2 py-1.5 font-medium text-zinc-400">{column}</th>)}</tr>
            </thead>
            <tbody>
              {(component.rows ?? []).map((row, rowIndex) => (
                <tr key={rowIndex} className="border-b border-zinc-800 last:border-0">
                  {row.map((cell, cellIndex) => <td key={cellIndex} className="px-2 py-1.5 align-top text-zinc-200">{cell}</td>)}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )
    case "list": {
      const List = component.ordered ? "ol" : "ul"
      return (
        <List className={`space-y-1 pl-5 text-sm text-zinc-300 ${component.ordered ? "list-decimal" : "list-disc"}`}>
          {items.map((item, index) => <li key={index}>{item.text}</li>)}
        </List>
      )
    }
    case "badges":
      return (
        <div className="flex flex-wrap gap-1.5">
          {items.map((item, index) => <span key={index} className={`rounded-full border px-2 py-0.5 text-xs ${TONE_BADGE[tone(item.tone)]}`}>{item.text}</span>)}
        </div>
      )
    case "progress": {
      const percent = Math.round(Math.min(1, Math.max(0, component.fraction ?? 0)) * 100)
      return (
        <div>
          <div className="mb-1 flex items-baseline justify-between gap-2 text-xs">
            <span className="text-zinc-300">{component.label}</span>
            <span className="text-zinc-500">{percent}%</span>
          </div>
          <div role="progressbar" aria-label={component.label || "Progress"} aria-valuemin={0} aria-valuemax={100} aria-valuenow={percent} className="h-2 overflow-hidden rounded-full bg-zinc-800">
            <div className="h-full rounded-full bg-indigo-500" style={{ width: `${percent}%` }} />
          </div>
          {component.detail && <div className="mt-1 text-xs text-zinc-500">{component.detail}</div>}
        </div>
      )
    }
    case "bar_chart": {
      const largest = Math.max(0, ...items.map((item) => item.number ?? 0))
      return (
        <div>
          {component.label && <div className="mb-1.5 text-xs text-zinc-400">{component.label}</div>}
          <ul aria-label={component.label || "Bar chart"} className="space-y-1">
            {items.map((item, index) => (
              <li key={index} className="grid grid-cols-[minmax(3rem,25%)_minmax(0,1fr)_auto] items-center gap-2 text-xs">
                <span className="truncate text-zinc-400">{item.label}</span>
                <span aria-hidden="true" className="h-2.5 overflow-hidden rounded-sm bg-zinc-800">
                  <span data-bar className="block h-full rounded-sm bg-indigo-500" style={{ width: `${largest > 0 ? Math.round(((item.number ?? 0) / largest) * 100) : 0}%` }} />
                </span>
                <span className="tabular-nums text-zinc-200">{item.number ?? 0}</span>
              </li>
            ))}
          </ul>
        </div>
      )
    }
    case "code":
      return (
        <pre className="max-w-full overflow-x-auto rounded border border-zinc-700 bg-black/40 p-2 font-mono text-xs leading-5 text-zinc-200">
          <code>{component.text}</code>
        </pre>
      )
    case "image":
      return <Picture entry={component} at={card.picture(`${at}`)} card={card} thumbnail={false} />
    case "image_grid":
      return (
        <ul aria-label="Images" className="grid list-none gap-2 p-0" style={{ gridTemplateColumns: "repeat(auto-fill, minmax(7rem, 1fr))" }}>
          {items.map((item, index) => (
            <li key={index} className="min-w-0">
              <Picture entry={item} at={card.picture(`${at}:${index}`)} card={card} thumbnail />
            </li>
          ))}
        </ul>
      )
    case "divider":
      return <hr className="border-zinc-800" />
    case "actions":
      if (!onProposeMessage) return null
      return (
        <div className="flex flex-wrap gap-2">
          {items.map((item, index) => (
            <button
              key={index}
              type="button"
              title="Puts this in your message box. Nothing is sent until you send it."
              onClick={() => item.message && onProposeMessage(item.message)}
              className="min-h-9 rounded border border-zinc-600 bg-zinc-800 px-3 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:border-zinc-500 hover:bg-zinc-700"
            >
              {item.label}
            </button>
          ))}
        </div>
      )
    default:
      // A newer server may know components this build does not.
      return null
  }
}

// cardPictures lists every image of a card in the order it shows them, and
// where in that list the image of each place in the card is. Stepping from
// one image to the next goes through the whole card, not one grid.
function cardPictures(components: UIComponent[], images: ItemImage[]) {
  const addresses = new Map(images.map((image) => [image.name, image.address]))
  const gallery: LightboxImage[] = []
  const slots = new Map<string, number>()
  const add = (slot: string, entry: { path?: string; alt?: string; caption?: string }) => {
    const address = addresses.get(entry.path ?? "")
    if (!address) return
    slots.set(slot, gallery.length)
    gallery.push({ src: address, alt: entry.alt ?? "", caption: entry.caption })
  }
  components.forEach((component, at) => {
    if (component.type === "image") add(`${at}`, component)
    if (component.type === "image_grid") (component.items ?? []).forEach((item, index) => add(`${at}:${index}`, item))
  })
  return { gallery, slots }
}

// AgentUIComponents draws a component card: what an agent named with
// Legato's fixed component list, drawn with Legato's own elements. Nothing an
// agent wrote runs here; every value is text.
export function AgentUIComponents({ components, onProposeMessage, images }: AgentUIComponentsProps) {
  const { gallery, slots } = useMemo(() => cardPictures(components, images ?? []), [components, images])
  const [open, setOpen] = useState<number | null>(null)
  const card: CardContext = { onProposeMessage, gallery, picture: (slot) => slots.get(slot), openPicture: setOpen }
  return (
    <div className="flex min-w-0 flex-col gap-3">
      {components.map((component, index) => {
        const rendered = renderComponent(component, index, card)
        return rendered === null ? null : <div key={index} className="min-w-0">{rendered}</div>
      })}
      {open !== null && <ImageLightbox images={gallery} index={open} onIndex={setOpen} onClose={() => setOpen(null)} />}
    </div>
  )
}
