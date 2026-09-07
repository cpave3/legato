import { useState } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { useServer } from "../../hooks/useServer"
import type { HandoffConcept, HandoffPacket } from "../../lib/handoff"
import { askReviewQuestion, createReviewFinding, type ReviewStep } from "../../lib/review"
import { queryKeys } from "../../lib/query"
import { Button } from "../ui/Button"

export function HandoffChallenge({ tourId, packet, concept, chapters }: { tourId: string; packet: HandoffPacket; concept: HandoffConcept; chapters: ReviewStep[] }) {
  const { baseUrl } = useServer()
  const client = useQueryClient()
  const [text, setText] = useState("")
  const [target, setTarget] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const targets = chapters.filter((c) => concept.chapter_ids.includes(c.id))
  const chapter = targets.find((c) => c.id === target) ?? targets[0]
  async function submit(kind: "question" | "finding") {
    setBusy(true); setError(""); setNotice("")
    const input = packet.input
    const body = `[Handoff packet ${packet.id} · revision ${packet.revision} · pass ${input.pass_id} · base ${input.base_sha} · head ${input.head_sha} · concept ${concept.id}: ${concept.title}]\nClaim: ${concept.claim}\n\n${text.trim()}`
    try {
      if (kind === "question") {
        const warning = await askReviewQuestion(baseUrl, tourId, chapter.id, body)
        setNotice(warning || "Question saved. Open the linked chapter for replies.")
      } else {
        await createReviewFinding(baseUrl, tourId, body, chapter?.id ?? "")
        setNotice(chapter ? "Finding saved. Open Diff review to follow up." : "Finding saved on the current pass with historical packet context.")
      }
      setText("")
      await client.invalidateQueries({ queryKey: queryKeys.reviewTour(baseUrl, tourId) })
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) }
    finally { setBusy(false) }
  }
  return <section className="space-y-2 border-t border-zinc-800 pt-3">
    <h4>Challenge this claim</h4>
    {targets.length > 1 && <label>Question chapter<select value={chapter.id} onChange={(e) => setTarget(e.target.value)}>{targets.map((c) => <option key={c.id} value={c.id}>{c.title}</option>)}</select></label>}
    {!chapter && <p className="text-sm text-amber-300">Question target unavailable for this packet/concept. A finding will attach to the current pass, not a current chapter, and retain the exact historical context.</p>}
    <textarea className="w-full rounded border border-zinc-700 bg-zinc-950 p-2" aria-label={`Challenge ${concept.title}`} value={text} onChange={(e) => setText(e.target.value)} />
    <div className="flex gap-2"><Button size="sm" variant="outline" disabled={busy || !text.trim() || !chapter} onClick={() => void submit("question")}>Ask question</Button><Button size="sm" variant="warning" disabled={busy || !text.trim()} onClick={() => void submit("finding")}>Flag change</Button></div>
    {error && <p role="alert">{error}</p>}{notice && <p role="status">{notice}</p>}
  </section>
}
