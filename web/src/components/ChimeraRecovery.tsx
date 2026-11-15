import { memo, useId, useMemo, useState } from "react"
import { ChevronDown, ChevronRight, Download, History } from "lucide-react"
import { recoveryParts } from "../lib/chimeraRecovery"

const focus = "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--theme-focus,var(--color-lw-accent))]"

// Strings stay text: recovered messages must not run HTML, load old remote
// images or recursively render another copy of the conversation's controls.
function RecoveryValue({ value, depth = 0 }: { value: unknown; depth?: number }) {
  if (typeof value === "string") return <div className="whitespace-pre-wrap [overflow-wrap:anywhere]">{value}</div>
  if (value === null || typeof value !== "object") return <span>{String(value)}</span>
  if (depth >= 12) return <pre className="whitespace-pre-wrap [overflow-wrap:anywhere]">{JSON.stringify(value, null, 2)}</pre>
  if (Array.isArray(value)) return <div className="space-y-3">{value.map((part, index) => <RecoveryValue key={index} value={part} depth={depth + 1} />)}</div>
  return <dl className="space-y-2">{Object.entries(value).map(([key, part]) => (
    <div key={key} className="min-w-0">
      <dt className="text-xs font-medium text-lw-text-2 [overflow-wrap:anywhere]">{key}</dt>
      <dd className="mt-0.5 min-w-0"><RecoveryValue value={part} depth={depth + 1} /></dd>
    </div>
  ))}</dl>
}

function RecoveryRecord({ value }: { value: unknown }) {
  if (!value || typeof value !== "object" || Array.isArray(value)) return <RecoveryValue value={value} />
  const { role, content, ...other } = value as Record<string, unknown>
  return <div className="min-w-0 space-y-2">
    {typeof role === "string" && <h3 className="text-xs font-semibold capitalize text-lw-text-2">{role}</h3>}
    {content !== undefined && <RecoveryContent value={content} />}
    {Object.keys(other).length > 0 && <RecoveryValue value={other} />}
  </div>
}

function RecoveryContent({ value }: { value: unknown }) {
  if (!Array.isArray(value)) return <RecoveryValue value={value} />
  return <div className="space-y-3">{value.map((part: unknown, index) => {
    if (!part || typeof part !== "object" || Array.isArray(part)) return <RecoveryValue key={index} value={part} />
    const { type, text, toolName, toolCallId, input, output, ...other } = part as Record<string, unknown>
    if (type === "text" && typeof text === "string") return <div key={index}>
      <RecoveryValue value={text} />
      {Object.keys(other).length > 0 && <RecoveryValue value={other} />}
    </div>
    if (type !== "tool-call" && type !== "tool-result") return <RecoveryValue key={index} value={part} />
    return <div key={index} className="min-w-0 space-y-1">
      <h4 className="text-xs font-medium text-lw-text-2">{type === "tool-call" ? "Tool call" : "Tool result"}{typeof toolName === "string" ? ` · ${toolName}` : ""}</h4>
      {input !== undefined && <RecoveryValue value={input} />}
      {output !== undefined && <RecoveryOutput value={output} />}
      <details className="min-w-0">
        <summary className={`cursor-pointer rounded py-1 text-xs text-lw-text-2 ${focus}`}>Call details</summary>
        <RecoveryValue value={{ toolCallId, ...other }} />
      </details>
    </div>
  })}</div>
}

function RecoveryOutput({ value }: { value: unknown }) {
  if (value && typeof value === "object" && !Array.isArray(value)) {
    const { type, value: text, ...other } = value as Record<string, unknown>
    if (type === "text" && typeof text === "string") return <div>
      <RecoveryValue value={text} />
      {Object.keys(other).length > 0 && <RecoveryValue value={other} />}
    </div>
  }
  return <RecoveryValue value={value} />
}

function AdditionalContext({ text }: { text: string }) {
  // Known compactor blocks are small XML-shaped records, not executable markup.
  const lines = text.split("\n").filter((line) => line.trim())
  return <div className="space-y-2">{lines.map((line, index) => {
    const section = line.match(/^<(files|tasks|archived)>$/)
    if (section) return <h3 key={index} className="pt-2 text-xs font-semibold capitalize text-lw-text-2">{section[1]}</h3>
    if (/^<\/(files|tasks|archived)>$/.test(line)) return null
    const entry = line.trim().match(/^<(read|modified|task|entry)(?:\s+([^>]*))?>(.*)<\/\1>$/)
    return entry ? <div key={index} className="min-w-0">
      <div className="text-xs text-lw-text-2">{entry[1]}{entry[2] ? ` · ${entry[2]}` : ""}</div>
      <div className="whitespace-pre-wrap [overflow-wrap:anywhere]">{entry[3]}</div>
    </div> : <div key={index} className="whitespace-pre-wrap [overflow-wrap:anywhere]">{line}</div>
  })}</div>
}

function downloadExcerpt(text: string) {
  const url = URL.createObjectURL(new Blob([text], { type: "text/plain;charset=utf-8" }))
  const link = document.createElement("a")
  link.href = url
  link.download = "chimera-recovery-excerpt.txt"
  link.click()
  window.setTimeout(() => URL.revokeObjectURL(url), 0)
}

export const ChimeraRecovery = memo(function ChimeraRecovery({ text }: { text: string }) {
  const [openText, setOpenText] = useState<string | null>(null)
  const expanded = openText === text
  const id = useId()
  // Parse only after expansion; large recovery text costs nothing while folded.
  const parts = useMemo(() => expanded ? recoveryParts(text) : [], [expanded, text])
  return <section className="mx-4 my-2 min-w-0 rounded-lg border border-lw-line-strong bg-lw-surface text-lw-text">
    <button type="button" aria-label="Compaction recovery" aria-expanded={expanded} aria-controls={id}
      className={`flex min-h-11 w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-[13px] hover:bg-lw-raised ${focus}`}
      onClick={() => setOpenText(expanded ? null : text)}>
      <History size={15} className="shrink-0 text-lw-text-2" aria-hidden="true" />
      <span className="min-w-0 flex-1 font-medium">Compaction recovery</span>
      {expanded ? <ChevronDown size={16} aria-hidden="true" /> : <ChevronRight size={16} aria-hidden="true" />}
    </button>
    {expanded && <div id={id} role="region" aria-label="Recovery excerpt" className="min-w-0 border-t border-lw-line-strong px-3 py-3 text-[13px] leading-5">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <p className="text-xs text-lw-text-2">Recovered context · not a model summary</p>
        <button type="button" className={`inline-flex min-h-11 items-center gap-1.5 rounded px-2 text-xs text-lw-link hover:bg-lw-raised ${focus}`} onClick={() => downloadExcerpt(text)}>
          <Download size={13} aria-hidden="true" />Download original excerpt
        </button>
      </div>
      <div tabIndex={0} role="region" aria-label="Recovered records" className={`max-h-[60vh] space-y-4 divide-y divide-lw-line overflow-y-auto overscroll-contain pr-1 ${focus}`}>{parts.map((part, index) => <div key={index} className="min-w-0 pt-3 first:pt-0">
        {part.kind === "record" && <RecoveryRecord value={part.value} />}
        {part.kind === "gap" && <p className="text-xs text-lw-text-2">Older context was truncated by Chimera.</p>}
        {part.kind === "context" && <AdditionalContext text={part.text} />}
        {part.kind === "partial" && <details className="min-w-0">
          <summary className={`cursor-pointer rounded py-2 text-xs text-lw-text-2 ${focus}`}>Partial record</summary>
          <pre className="max-h-60 overflow-y-auto whitespace-pre-wrap py-2 font-lw-mono text-xs [overflow-wrap:anywhere]">{part.text}</pre>
        </details>}
      </div>)}</div>
    </div>}
  </section>
})
