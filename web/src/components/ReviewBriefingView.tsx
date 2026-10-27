import { useMemo } from "react"
import { useReviewBriefingPage } from "../hooks/useReviewBriefing"
import { briefingPageOf, buildBriefingDocument, type ReviewBriefingPage } from "../lib/reviewBriefing"
import { SandboxedHtml } from "./SandboxedHtml"
import { LoadingState } from "./ui/LoadingState"

interface ReviewBriefingViewProps {
  agentId: string
  page: ReviewBriefingPage
  // A link in the page asked for another page of the briefing.
  onOpenPage: (path: string) => void
}

// ReviewBriefingView draws one page of the review briefing on an agent's
// branch, as the pull request will carry it. The page was written by an
// agent, so it is drawn in the same sandbox as HTML an agent shows, with its
// own theme in place of Legato's.
export function ReviewBriefingView({ agentId, page, onOpenPage }: ReviewBriefingViewProps) {
  const { page: content, loading, error } = useReviewBriefingPage(agentId, page.path, page.version)
  const frameDocument = useMemo(
    () => (content ? buildBriefingDocument({ html: content.html, resourceDomains: content.resource_domains ?? [] }) : ""),
    [content],
  )
  if (error) return <p role="alert" className="p-4 text-sm text-red-300">Could not read {page.path}: {error}</p>
  if (loading || !content) return <LoadingState label={`Reading ${page.path}…`} />
  return (
    <SandboxedHtml
      html=""
      frameDocument={frameDocument}
      title={`Review briefing: ${page.path}`}
      resourceDomains={content.resource_domains ?? []}
      fill
      origin="From .review/ on the branch"
      onOpenPage={(href) => {
        const target = briefingPageOf(href, page.path)
        if (target) onOpenPage(target)
      }}
    />
  )
}
