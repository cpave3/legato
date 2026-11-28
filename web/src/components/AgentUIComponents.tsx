import { useMemo, useState } from "react"
import type { ItemImage, UIComponent } from "../lib/agentUi"
import { ImageLightbox, type LightboxImage } from "./ImageLightbox"
import { renderComponent, type CardContext } from "./agentUIRenderers"

interface AgentUIComponentsProps {
  components: UIComponent[]
  // An action button was pressed: its message is for the person's message
  // box. Without a handler, actions are not drawn.
  onProposeMessage?: (text: string) => void
  // Where Legato's copy of each image file is, by the path the agent named.
  images?: ItemImage[]
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
