import { useMemo } from "react"
import type { ItemImage } from "../lib/agentUi"
import { documentImages, type DocumentImage } from "../lib/documentImages"
import { useServer } from "./useServer"

// useDocumentImages says where a document's images are at the server the
// page talks to. The list keeps its identity while the document does, so
// that Markdown drawn from it is not drawn again for nothing.
export function useDocumentImages(images: DocumentImage[] | undefined): ItemImage[] {
  const { baseUrl } = useServer()
  return useMemo(() => documentImages(baseUrl, images), [baseUrl, images])
}
