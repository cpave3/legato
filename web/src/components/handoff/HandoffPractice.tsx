import { useState } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { useServer } from "../../hooks/useServer"
import { saveHandoffNote, type HandoffNote, type HandoffPacket, type HandoffView } from "../../lib/handoff"
import { queryKeys } from "../../lib/query"
import { HandoffSources } from "./HandoffSources"
import { MarkdownBody } from "../MarkdownBody"
import { Button } from "../ui/Button"

export function HandoffPractice({ tourId, packet }: { tourId: string; packet: HandoffPacket }) {
  const { baseUrl } = useServer()
  const client = useQueryClient()
  const [note, setNote] = useState<HandoffNote>(() => ({ ...packet.note, answers: packet.note.answers ?? {}, skipped: packet.note.skipped ?? [] }))
  const [revealed, setRevealed] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const [reload, setReload] = useState(false)
  async function save(next: HandoffNote) {
    setBusy(true); setError(""); setNotice("")
    try {
      const saved = await saveHandoffNote(baseUrl, tourId, packet.id, next)
      setNote({ ...saved, answers: saved.answers ?? {}, skipped: saved.skipped ?? [] })
      client.setQueryData<HandoffView>(queryKeys.handoff(baseUrl, tourId), (current) => current && ({ ...current, packets: current.packets.map((p) => p.id === packet.id ? { ...p, note: saved } : p) }))
      setNotice("Note saved. This does not complete or approve the review.")
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) }
    finally { setBusy(false) }
  }
  async function reloadNote() {
    setBusy(true); setError("")
    try {
      await client.refetchQueries({ queryKey: queryKeys.handoff(baseUrl, tourId), exact: true }, { throwOnError: true })
      const fresh = client.getQueryData<HandoffView>(queryKeys.handoff(baseUrl, tourId))!.packets.find((p) => p.id === packet.id)!.note
      setNote({ ...fresh, answers: fresh.answers ?? {}, skipped: fresh.skipped ?? [] }); setReload(false); setNotice("")
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) }
    finally { setBusy(false) }
  }
  return <section className="space-y-4 rounded border border-zinc-800 p-4" aria-label="Optional practice">
    <h3 className="font-semibold">Optional maintenance practice</h3>
    <p className="text-sm text-zinc-400">Open-book, ungraded. Reveal shows an agent-authored explanation, not a correct/pass result. Notes belong to this local instance, not a verified identity, and remain here unless you explicitly export them. Self-assessment is separate from review completion and merge approval.</p>
    <fieldset disabled={busy} className="space-y-4">
      {packet.input.scenarios.map((scenario) => <section key={scenario.id} className="space-y-2 border-t border-zinc-800 pt-3">
        <h4>{scenario.prompt}</h4><a className="text-indigo-300 underline" href={`#concept-${scenario.concept_id}`}>Discuss the linked concept</a>
        <textarea className="w-full rounded border border-zinc-700 bg-zinc-950 p-2" aria-label={`Your answer: ${scenario.prompt}`} value={note.answers[scenario.id] ?? ""} onChange={(e) => setNote({ ...note, answers: { ...note.answers, [scenario.id]: e.target.value }, skipped: note.skipped.filter((id) => id !== scenario.id) })} />
        <div className="flex flex-wrap gap-2"><Button size="sm" variant="outline" onClick={() => setRevealed((ids) => ids.includes(scenario.id) ? ids.filter((id) => id !== scenario.id) : [...ids, scenario.id])}>{revealed.includes(scenario.id) ? "Hide explanation" : "Reveal explanation"}</Button><Button size="sm" variant="ghost" onClick={() => { const answers = { ...note.answers }; delete answers[scenario.id]; void save({ ...note, answers, skipped: [...new Set([...note.skipped, scenario.id])] }) }}>Skip scenario</Button></div>
        {note.skipped.includes(scenario.id) && <p>Skipped — no assessment inferred.</p>}
        {revealed.includes(scenario.id) && <div><p className="text-xs text-zinc-400">Agent-authored explanation — inspect the pinned sources.</p><MarkdownBody disableImages body={scenario.explanation} /><HandoffSources packet={packet} ids={scenario.source_ids} /></div>}
      </section>)}
      <label className="block">Explain it back / human note<textarea className="block w-full rounded border border-zinc-700 bg-zinc-950 p-2" rows={4} value={note.body} onChange={(e) => setNote({ ...note, body: e.target.value })} /></label>
      <label className="block">Local-instance self-assessment<select className="ml-2 rounded border border-zinc-700 bg-zinc-950 p-2" value={note.readiness} onChange={(e) => setNote({ ...note, readiness: e.target.value })}><option value="">Not stated</option><option value="ready">Ready to maintain</option><option value="need_help">Need help</option><option value="deferred">Deferred</option></select></label>
      <Button size="sm" onClick={() => void save(note)}>Save my note</Button>
    </fieldset>
    {packet.note.revision !== note.revision && <p role="status">A different note revision is available. Your unsaved text has not been replaced.</p>}
    {error && <p role="alert">{error}</p>}{notice && <p role="status">{notice}</p>}
    <Button size="sm" variant="outline" disabled={busy} onClick={() => setReload(true)}>Reload saved note</Button>
    {reload && <div role="alert"><p>Discard unsaved answers and load the latest saved note?</p><Button disabled={busy} size="sm" onClick={() => void reloadNote()}>Discard unsaved note and reload</Button><Button size="sm" variant="ghost" onClick={() => setReload(false)}>Keep editing</Button></div>}
  </section>
}
