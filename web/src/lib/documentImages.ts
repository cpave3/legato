import type { ItemImage } from "./agentUi"

// DocumentImage is an image file the Markdown of a plan or of a review
// shows, as the API names it: the path the Markdown wrote, and where the
// server gives out Legato's copy of the file.
export interface DocumentImage {
  name: string
  url: string
}

// Where the server gives out those copies (service.PlanImageRoute,
// service.ReviewImageRoute). They ask for no token: the address of an image
// is the hash of its content.
const IMAGE_ROUTES = ["/plan-images/", "/review-images/"]

// documentImages says where each of a document's images is, at the server
// the page talks to (the page's own when baseUrl is empty). An address that
// is not one of Legato's kept images is left out.
export function documentImages(baseUrl: string, images: DocumentImage[] | undefined): ItemImage[] {
  const origin = baseUrl || window.location.origin
  return (images ?? []).flatMap(({ name, url }) => {
    if (typeof url !== "string") return []
    try {
      const address = new URL(url, origin)
      const kept = address.origin === new URL(origin).origin && IMAGE_ROUTES.some((route) => url.startsWith(route) && address.pathname.startsWith(route))
      return kept ? [{ name, address: address.href }] : []
    } catch {
      return []
    }
  })
}

function decoded(path: string): string {
  try {
    return decodeURIComponent(path)
  } catch {
    return path
  }
}

// findDocumentImage finds the image a Markdown destination names. Markdown
// may write a path with its spaces escaped, and the renderer may escape what
// the Markdown did not, so the two are compared as the paths they mean.
export function findDocumentImage(images: ItemImage[], destination: string | undefined): ItemImage | undefined {
  if (!destination) return undefined
  const wanted = decoded(destination)
  return images.find((image) => image.name === destination || decoded(image.name) === wanted)
}

// isRemoteImage reports whether a Markdown image's destination is an address
// the browser fetches by itself, which is no file of Legato's.
export function isRemoteImage(destination: string | undefined): boolean {
  return /^(https?:|data:|blob:|\/\/)/i.test(destination ?? "")
}
