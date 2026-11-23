import { useMemo, useState } from "react"
import { ArrowLeft, ClipboardPaste, Copy, FilePlus2 } from "lucide-react"
import { Button } from "../ui/Button"
import { LazyWorkflowYamlEditor } from "./LazyWorkflowYamlEditor"
import WorkflowOutline from "./WorkflowOutline"
import { asTemplate, blankWorkflow, copyOf, startLines, summaryLine } from "./workflowSummary"

// The start page of a new workflow (LEG-514): the templates, each with
// what starts it, what it asks for and what it does, its outline shown
// read-only once chosen; Blank; Paste YAML, summed up as you paste; and
// Copy one of yours. Each opens the editor on its text, unsaved.

export type StartTemplate = { name: string; title: string; description?: string; yaml: string }
export type WorkflowStartProps = {
  templates: StartTemplate[] | null
  yours: { name: string; title: string }[]
  /** taken are the workflow names in use, which a start never takes. */
  taken: string[]
  loadYours: (name: string) => Promise<string>
  onStart: (text: string) => void
  narrow?: boolean
}

const noop = () => {}

export default function WorkflowStart({ templates, yours, taken, loadYours, onStart, narrow = false }: WorkflowStartProps) {
  const [chosen, setChosen] = useState<StartTemplate | null>(null)
  const [pasting, setPasting] = useState<string | null>(null)
  const [error, setError] = useState("")
  const pastedSummary = useMemo(() => pasting === null ? "" : summaryLine(pasting), [pasting])

  if (pasting !== null) {
    return <section aria-label="Paste YAML" className="flex flex-col gap-3">
      <Button type="button" variant="ghost" size="sm" className="self-start" onClick={() => setPasting(null)}><ArrowLeft size={13} />All the ways to start</Button>
      <h2 className="text-base font-semibold">Paste a workflow's YAML</h2>
      <div className="h-80 overflow-hidden rounded-xl border border-lw-line"><LazyWorkflowYamlEditor label="Pasted YAML" value={pasting} onChange={setPasting} /></div>
      <p role="status" aria-label="What it is" className="text-sm text-lw-text-2">{pastedSummary || (pasting.trim() === "" ? "Paste the YAML above." : "Not a workflow yet: the problems are marked in the text.")}</p>
      <Button type="button" className="self-start" disabled={!pastedSummary} onClick={() => onStart(asTemplate(pasting, taken))}>Continue</Button>
    </section>
  }

  const preview = chosen && <section aria-label={`Template ${chosen.title}`} className="flex flex-col gap-3">
    {narrow && <Button type="button" variant="ghost" size="sm" className="self-start" onClick={() => setChosen(null)}><ArrowLeft size={13} />Templates</Button>}
    <h2 className="text-base font-semibold">{chosen.title}</h2>
    {chosen.description && <p className="text-sm text-lw-text-2">{chosen.description}</p>}
    <div inert className="rounded-lg border border-lw-line bg-lw-ground p-2">
      <WorkflowOutline mode="navigate" text={chosen.yaml} onEdit={noop} onEditYaml={noop} selection={null} onSelect={noop} />
    </div>
    <Button type="button" className="self-start" onClick={() => onStart(asTemplate(chosen.yaml, taken))}>Use this template</Button>
  </section>

  const way = (label: string, detail: string, Icon: typeof FilePlus2, onClick: () => void) => <button type="button" className="flex min-h-14 items-start gap-3 rounded-lg border border-lw-line bg-lw-surface p-3 text-left hover:bg-lw-raised" onClick={onClick}>
    <Icon size={16} aria-hidden className="mt-0.5 shrink-0 text-lw-faint" />
    <span className="flex flex-col"><span className="text-sm font-medium">{label}</span><span className="text-xs text-lw-text-2">{detail}</span></span>
  </button>

  const gallery = <div className="flex flex-col gap-4">
    <section aria-label="Start from" className="grid gap-2 sm:grid-cols-2">
      {way("Blank", "Nothing yet: add steps one by one", FilePlus2, () => onStart(blankWorkflow(taken)))}
      {way("Paste YAML", "A workflow you have as text", ClipboardPaste, () => setPasting(""))}
    </section>
    {error && <p role="alert" className="text-sm text-lw-danger">{error}</p>}
    {yours.length > 0 && <section aria-label="Copy one of yours" className="flex flex-col gap-1">
      <h2 className="text-[11px] font-semibold uppercase tracking-wide text-lw-faint">Copy one of yours</h2>
      <ul className="flex flex-wrap gap-1.5">
        {yours.map(w => <li key={w.name}><Button type="button" size="sm" variant="outline" onClick={() => { setError(""); loadYours(w.name).then(text => onStart(copyOf(text, taken)), reason => setError(`Could not copy ${w.title}: ${reason instanceof Error ? reason.message : String(reason)}`)) }}><Copy size={12} />{w.title}</Button></li>)}
      </ul>
    </section>}
    <section aria-label="Templates" className="flex flex-col gap-1">
      <h2 className="text-[11px] font-semibold uppercase tracking-wide text-lw-faint">Templates</h2>
      {templates === null && <p className="text-sm text-lw-text-2">Loading the templates…</p>}
      <ul className="grid gap-2 sm:grid-cols-2">
        {(templates ?? []).map(t => {
          const lines = startLines(t.yaml)
          return <li key={t.name}><button type="button" aria-pressed={chosen?.name === t.name} className={`flex w-full flex-col gap-1 rounded-lg border p-3 text-left hover:bg-lw-raised ${chosen?.name === t.name ? "border-lw-accent bg-lw-raised" : "border-lw-line bg-lw-surface"}`} onClick={() => setChosen(t)}>
            <span className="text-sm font-medium">{t.title}</span>
            <span className="text-xs text-lw-text-2"><span className="text-lw-faint">When</span> {lines.when}</span>
            <span className="text-xs text-lw-text-2"><span className="text-lw-faint">Asks for</span> {lines.asks}</span>
            <span className="truncate text-xs text-lw-text-2"><span className="text-lw-faint">Does</span> {lines.does}</span>
          </button></li>
        })}
      </ul>
    </section>
  </div>

  if (narrow) {
    return chosen ? <div role="dialog" aria-label={chosen.title} className="fixed inset-0 z-40 overflow-y-auto bg-lw-ground px-4 py-3">{preview}</div> : gallery
  }
  return <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,24rem)]">
    {gallery}
    <div className="rounded-xl border border-lw-line bg-lw-surface p-4">{preview ?? <p className="text-sm text-lw-text-2">Choose a template to see it here.</p>}</div>
  </div>
}
