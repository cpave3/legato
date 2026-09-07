import { useState } from "react"
import { useServer } from "../../hooks/useServer"
import { exportHandoff, type HandoffExport as ExportResult, type HandoffPacket } from "../../lib/handoff"
import { MarkdownBody } from "../MarkdownBody"
import { Button } from "../ui/Button"

export function HandoffExport({ tourId, packet }: { tourId: string; packet: HandoffPacket }) {
  const { baseUrl } = useServer()
  const [format, setFormat] = useState("github")
  const [choose, setChoose] = useState(false)
  const [concepts, setConcepts] = useState(packet.input.concepts.map((c) => c.id))
  const [sources, setSources] = useState(false)
  const [notes, setNotes] = useState(false)
  const [preview, setPreview] = useState<ExportResult | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const [previewNoteRevision, setPreviewNoteRevision] = useState(0)
  const [previewFreshness, setPreviewFreshness] = useState("")
  const freshness = JSON.stringify([packet.id, packet.outdated, packet.outdated_reason])
  const currentPreview = preview && previewFreshness === freshness && (!notes || previewNoteRevision === packet.note.revision) ? preview : null
  function clearPreview() { setPreview(null); setError(""); setNotice("") }
  async function prepare() {
    clearPreview(); setBusy(true)
    try {
      const result = await exportHandoff(baseUrl, tourId, { packet_id: packet.id, format, concept_ids: choose ? concepts : [], include_sources: sources, include_notes: notes })
      setPreview(result); setPreviewNoteRevision(packet.note.revision); setPreviewFreshness(freshness)
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) }
    finally { setBusy(false) }
  }
  async function copy() {
    setError(""); setNotice("")
    try { await navigator.clipboard.writeText(currentPreview!.markdown); setNotice("Markdown copied. Nothing was posted to GitHub.") }
    catch (cause) { setError(`Copy failed: ${cause instanceof Error ? cause.message : String(cause)}. Use the Markdown text below or download.`) }
  }
  function download() {
    setError("")
    try {
      const url = URL.createObjectURL(new Blob([currentPreview!.markdown], { type: "text/markdown;charset=utf-8" }))
      const link = document.createElement("a")
      link.href = url; link.download = currentPreview!.filename; link.click()
      setTimeout(() => URL.revokeObjectURL(url), 0)
    } catch (cause) { setError(`Download failed: ${cause instanceof Error ? cause.message : String(cause)}`) }
  }
  return <section aria-label="Export handoff brief" className="space-y-3 rounded border border-zinc-800 p-4">
    <h3 className="font-semibold">Export handoff brief</h3>
    <p className="text-sm text-amber-300">Preview before sharing. Automatic secret detection cannot be guaranteed. Inspect code, personal notes, paths and sensitive details. This is an AI-authored snapshot; PR linkage is unverified. Compare base/head with the current PR. Nothing is posted or approved automatically.</p>
    <fieldset disabled={busy} className="space-y-3">
      <label className="block">Export format<select className="ml-2 rounded border border-zinc-700 bg-zinc-950 p-2" value={format} onChange={(e) => { setFormat(e.target.value); clearPreview() }}><option value="github">GitHub review brief</option><option value="maintenance">Maintenance brief</option></select></label>
      <label className="block"><input type="checkbox" checked={choose} onChange={(e) => { setChoose(e.target.checked); clearPreview() }} /> Choose concepts</label>
      {choose && <div className="space-y-2 pl-4">{packet.input.concepts.map((c) => <label key={c.id} className="block"><input type="checkbox" aria-label={`Export concept: ${c.title}`} checked={concepts.includes(c.id)} onChange={(e) => { setConcepts(e.target.checked ? [...concepts, c.id] : concepts.filter((id) => id !== c.id)); clearPreview() }} /> {c.title}</label>)}{concepts.length === 0 && <p>Select at least one concept, or turn off selection to include all.</p>}</div>}
      <label className="block"><input type="checkbox" checked={sources} onChange={(e) => { setSources(e.target.checked); clearPreview() }} /> Include source excerpts</label>
      <label className="block"><input type="checkbox" checked={notes} onChange={(e) => { setNotes(e.target.checked); clearPreview() }} /> Include my local notes and answers</label>
      {notes && <p>Only saved notes are included. Unsaved text is not exported.</p>}
      <Button size="sm" disabled={choose && concepts.length === 0} onClick={() => void prepare()}>{busy ? "Preparing preview…" : "Preview Markdown"}</Button>
    </fieldset>
    {error && <p role="alert">{error}</p>}{notice && <p role="status">{notice}</p>}
    {currentPreview && <div className="space-y-3">
      {currentPreview.warnings?.map((warning, i) => <p role="alert" key={i} className="text-amber-300">{warning}</p>)}
      <h4>Preview — inspect before sharing</h4><MarkdownBody disableImages skipHtml body={currentPreview.markdown} />
      <details><summary>Markdown text</summary><textarea readOnly aria-label="Export Markdown" className="w-full bg-zinc-950 p-2 font-mono text-xs" rows={12} value={currentPreview.markdown} /></details>
      <div className="flex flex-wrap gap-2"><Button size="sm" onClick={() => void copy()}>Copy Markdown</Button><Button size="sm" variant="outline" onClick={download}>Download Markdown</Button></div>
    </div>}
  </section>
}
