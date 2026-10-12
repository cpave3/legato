import { memo } from "react"
import { useDocumentImages } from "../hooks/useDocumentImages"
import type { DocumentImage } from "../lib/documentImages"
import { MarkdownBody } from "./MarkdownBody"

interface ReviewMessageBodyProps {
  body: string
  // The image files the message shows, when it is part of a plan or a
  // review that keeps them.
  images?: DocumentImage[]
}

export const ReviewMessageBody = memo(function ReviewMessageBody({ body, images }: ReviewMessageBodyProps) {
  return images ? <MessageWithImages body={body} images={images} /> : <MarkdownBody body={body} />
})

function MessageWithImages({ body, images }: { body: string; images: DocumentImage[] }) {
  return <MarkdownBody body={body} images={useDocumentImages(images)} />
}
