import { memo } from "react"
import { MarkdownBody } from "./MarkdownBody"

interface ReviewMessageBodyProps {
  body: string
}

export const ReviewMessageBody = memo(function ReviewMessageBody({ body }: ReviewMessageBodyProps) {
  return <MarkdownBody body={body} />
})
