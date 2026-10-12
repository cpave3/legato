import { useMemo, useState, type MouseEvent } from "react"
import { useServer } from "../hooks/useServer"
import { itemImages, parseUIComponents, resolveImageReferences, type AgentUIItem, type ItemImage } from "../lib/agentUi"
import { AgentUIComponents } from "./AgentUIComponents"
import { ImageLightbox, type LightboxImage } from "./ImageLightbox"
import { MarkdownBody } from "./MarkdownBody"
import { SandboxedHtml } from "./SandboxedHtml"

interface AgentUIItemViewProps {
  item: AgentUIItem
  // fill: take the container's height (the side panel). Otherwise the view
  // is as tall as its content, up to a limit (a card in the conversation).
  fill?: boolean
  onOverflow?: (overflowing: boolean) => void
  onProposeMessage?: (text: string) => void
}

// AgentUIItemView draws one item an agent showed, by its format: HTML in a
// sandboxed frame, Markdown and component cards with Legato's own elements.
// The image files the item uses are drawn from Legato's copies of them.
export function AgentUIItemView({ item, fill = false, onOverflow, onProposeMessage }: AgentUIItemViewProps) {
  const { baseUrl } = useServer()
  const images = useMemo(() => itemImages(baseUrl, item), [baseUrl, item])
  const title = item.title || (item.placement === "panel" ? "Artifact from the agent" : "Card from the agent")
  if (item.format === "html") {
    return (
      <SandboxedHtml
        html={item.content ?? ""}
        title={title}
        resourceDomains={item.resource_domains ?? []}
        fill={fill}
        onOverflow={onOverflow}
        onProposeMessage={onProposeMessage}
        images={images}
      />
    )
  }
  const body = item.format === "markdown"
    ? <MarkdownWithImages body={item.content ?? ""} images={images} />
    : <AgentUIComponents components={parseUIComponents(item.content)} onProposeMessage={onProposeMessage} images={images} />
  return fill
    ? <div className="min-h-0 flex-1 overflow-y-auto p-4 text-zinc-200">{body}</div>
    : <div className="p-3 text-zinc-200">{body}</div>
}

// MarkdownWithImages draws an agent's Markdown with its image files in
// place, and opens one over the whole window when the person clicks it. An
// image from anywhere else is left to be what it is.
function MarkdownWithImages({ body, images }: { body: string; images: ItemImage[] }) {
  const [open, setOpen] = useState<{ shown: LightboxImage[]; index: number } | null>(null)
  const resolved = useMemo(() => resolveImageReferences(body, images), [body, images])

  function onClick(event: MouseEvent<HTMLDivElement>) {
    if (!(event.target instanceof HTMLImageElement)) return
    const own = new Set(images.map((image) => image.address))
    // Every image of the document, in the order it shows them.
    const drawn = [...event.currentTarget.querySelectorAll("img")].filter((image) => own.has(image.src))
    const index = drawn.indexOf(event.target)
    if (index >= 0) setOpen({ shown: drawn.map((image) => ({ src: image.src, alt: image.alt })), index })
  }

  return (
    <div onClick={onClick} className={images.length > 0 ? "[&_img]:max-w-full [&_img]:cursor-zoom-in [&_img]:rounded-md [&_img]:border [&_img]:border-zinc-700" : undefined}>
      <MarkdownBody body={resolved} renderMermaid />
      {open && (
        <ImageLightbox images={open.shown} index={open.index} onIndex={(index) => setOpen({ shown: open.shown, index })} onClose={() => setOpen(null)} />
      )}
    </div>
  )
}
