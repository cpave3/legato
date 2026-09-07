import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { useServer } from "../../hooks/useServer"
import { fetchHandoff, type HandoffPacket } from "../../lib/handoff"
import { queryKeys } from "../../lib/query"
import type { ReviewTourView } from "../../lib/review"
import { MarkdownBody } from "../MarkdownBody"
import { HandoffSources as Sources } from "./HandoffSources"
import { HandoffEvidence } from "./HandoffEvidence"
import { HandoffExport } from "./HandoffExport"
import { HandoffPractice } from "./HandoffPractice"
import { HandoffChallenge } from "./HandoffChallenge"
import { Button } from "../ui/Button"

export interface HandoffProps { active: boolean; tourId: string; review: ReviewTourView; onChapter: (id: string) => void }
const fieldClass = "block w-full rounded border border-zinc-700 bg-zinc-950 p-2 text-sm"
const sectionClass = "space-y-3 rounded border border-zinc-800 p-4"

export function HandoffView(props: HandoffProps) {
  const { baseUrl } = useServer()
  const [selected, setSelected] = useState("")
  const query = useQuery({ queryKey: queryKeys.handoff(baseUrl, props.tourId), queryFn: () => fetchHandoff(baseUrl, props.tourId), enabled: props.active })
  if (query.isPending) return <p role="status" className="p-5">Loading handoff…</p>
  if (query.error && !query.data) return <div role="alert" className="p-5">{query.error.message}<Button onClick={() => void query.refetch()}>Retry handoff</Button></div>
  if (!query.data) return null
  const packet = query.data.packets.find((p) => p.id === selected) ?? query.data.packets[0]
  if (!selected && packet) setSelected(packet.id)
  return <main aria-label="Handoff" className="min-h-0 flex-1 overflow-y-auto p-3 md:p-5">
    <div className="mx-auto max-w-5xl space-y-5">
      {query.error && <div role="alert">Refresh failed: {query.error.message}. Showing the cached snapshot.<Button size="sm" variant="outline" onClick={() => void query.refetch()}>Retry handoff</Button></div>}
      {!packet ? <section><h2>No handoff packet</h2><p>Ask the author to prepare a clean, ready review snapshot, then submit with <code>legato review handoff submit --file packet.json</code>. Diff review remains available.</p>{props.review.tour.status === "capturing" && <p>Review is draft; publication requires a clean, ready snapshot.</p>}</section> : <>
        {packet.id !== query.data.packets[0].id && <p role="status">A newer handoff revision is available. Your selected revision and draft remain open.</p>}
        <label className="block">Revision history<select className={fieldClass} value={packet.id} onChange={(e) => setSelected(e.target.value)}>{query.data.packets.map((p) => <option key={p.id} value={p.id}>Revision {p.revision} · {p.input.head_sha} {p.outdated ? "· Outdated" : ""}</option>)}</select></label>
        <p className="text-sm text-zinc-400">Last checked: {query.data.checked_at || "Not yet observed"}. Freshness is only known through the last successful sync, not continuously.</p>
        <Packet key={packet.id} {...props} packet={packet} />
      </>}
    </div>
  </main>
}

function matchingChapters(packet: HandoffPacket, review: ReviewTourView) {
  const active = review.passes?.at(-1)
  if (packet.outdated || active?.pass.id !== packet.input.pass_id || review.tour.base_sha !== packet.input.base_sha || review.tour.head_sha !== packet.input.head_sha) return []
  return active.steps.filter((s) => !s.orphaned_at && s.kind === "chapter" && packet.chapters.some((c) => c.id === s.id))
}

function Packet({ packet, review, onChapter, tourId }: HandoffProps & { packet: HandoffPacket }) {
  const [route, setRoute] = useState("owner")
  const input = packet.input
  const chapters = matchingChapters(packet, review)
  const concepts = [...input.concepts].sort((a, b) => Number(b.routes.includes(route)) - Number(a.routes.includes(route)))
  return <>
    <header className="space-y-2 break-words">
      <h2 className="text-lg font-semibold">Author briefing</h2>
      <p>AI-authored context · packet {packet.id} · revision {packet.revision} · pass {input.pass_id}</p>
      <p className="text-xs">{packet.repository || "Repository identity unavailable"} · base {input.base_sha} · head {input.head_sha} · created {packet.created_at}</p>
      {packet.outdated && <p role="alert" className="text-amber-300">Outdated packet: {packet.outdated_reason}. Recheck evidence and read the current full diff.</p>}
      <p className="text-sm text-zinc-400">Read the behavior, trace the highest-risk path, compare the decision with its alternative, then state what the evidence does not establish. This brief does not replace independent review.</p>
    </header>
    <section className={sectionClass} aria-label="Behavior and decisions">
      {Object.entries(input.overview).map(([key, value]) => <div key={key}><h3 className="text-xs font-semibold uppercase text-zinc-400">{({ non_goals: "Non-goals", verification_gaps: "Verification gaps", review_question: "Review question" } as Record<string, string>)[key] ?? key}</h3><MarkdownBody disableImages body={value || (key === "rollback" ? "Rollback not established" : "Not supplied")} /></div>)}
    </section>
    {packet.plan_markdown ? <details className={sectionClass}><summary className="cursor-pointer">Captured plan context · {packet.plan_id} · revision {packet.plan_revision}</summary><MarkdownBody disableImages body={packet.plan_markdown} /></details> : <p>No captured plan context.</p>}
    {packet.comparison.previous_revision > 0 && <section className={sectionClass}><h3>Since revision {packet.comparison.previous_revision}</h3><MarkdownBody disableImages body={input.changes || "No author change summary supplied."} />{!packet.comparison.safe && <p>Full reread required: {packet.comparison.reason}</p>}<p>Changed concepts: {packet.comparison.changed_concepts?.join(", ") || "None reported"}</p><p>Changed files: {packet.comparison.changed_files?.join(", ") || "None reported"}</p><p>Evidence to recheck: {packet.comparison.evidence_to_recheck?.join(", ") || "None reported"}</p></section>}
    <label className="block">Reading route<select className={fieldClass} value={route} onChange={(e) => setRoute(e.target.value)}><option value="owner">Author / owner</option><option value="reviewer">GitHub reviewer</option><option value="maintainer">Maintainer</option><option value="colleague">Colleague</option></select></label>
    <p className="text-sm text-zinc-400">Routes reorder cards only. Risks, checks and full diff coverage remain visible.</p>
    <nav aria-label="Concepts" className="flex flex-wrap gap-3">{concepts.map((c) => <a className="text-indigo-300 underline" key={c.id} href={`#concept-${c.id}`}>{c.title}</a>)}</nav>
    {concepts.map((concept) => <article id={`concept-${concept.id}`} key={concept.id} className={sectionClass}>
      <h3 className="font-semibold">{concept.title}</h3><p className="text-xs text-zinc-400">Agent assertion — not human confirmation</p><MarkdownBody disableImages body={concept.claim} />
      <ol className="list-inside list-decimal">{concept.flow.map((step, i) => <li key={i}>{step}</li>)}</ol>
      <p>Invariant: {concept.invariant}</p><p>Failure boundary: {concept.failure_case}</p><p>Change starting point: {concept.change_guide}</p>
      <Sources packet={packet} ids={concept.source_ids} />
      {concept.evidence_ids.length === 0 && <p>No evidence attached</p>}
      {concept.evidence_ids.map((id) => {
        const evidence = input.evidence.find((e) => e.id === id)
        if (!evidence) return <p key={id}>No evidence attached: {id}</p>
        return <HandoffEvidence key={id} packet={packet} evidence={evidence} shownSourceIDs={concept.source_ids} />
      })}
      {concept.chapter_ids.map((id) => {
        const captured = packet.chapters.find((c) => c.id === id)
        return chapters.some((c) => c.id === id) ? <Button key={id} variant="outline" size="sm" onClick={() => onChapter(id)}>Open chapter: {captured?.title ?? id}</Button> : <details key={id}><summary>Historical chapter unavailable: {captured?.title ?? id}</summary><MarkdownBody disableImages body={captured?.narration || "No captured narration."} /></details>
      })}
      <HandoffChallenge tourId={tourId} packet={packet} concept={concept} chapters={chapters} />
    </article>)}
    {input.evidence.filter((e) => !input.concepts.some((c) => c.evidence_ids.includes(e.id))).map((e) => <HandoffEvidence key={e.id} packet={packet} evidence={e} />)}
    <section className={sectionClass}><h3>Full diff coverage</h3><p>Agent-suggested categories do not establish safety. Inspect every changed file in Diff review.</p><ul>{input.coverage.map((c) => <li key={c.path}><code>{c.path}</code> — {c.category}</li>)}</ul></section>
    <HandoffPractice tourId={tourId} packet={packet} />
    <HandoffExport tourId={tourId} packet={packet} />
  </>
}
