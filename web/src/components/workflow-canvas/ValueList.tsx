import { useState } from "react"
import { inputText } from "../../lib/workflowView"
import { cn } from "../../lib/utils"
import { redact } from "./canvasStyles"

function formatValue(value: unknown, secrets: string[]): string {
  return redact(inputText(value), secrets)
}

// A value longer than this, or of more lines, is cut to three lines with
// Show all where a list clamps.
const longValue = (text: string) => text.length > 160 || text.split("\n").length > 3

// ValueList is named values, such as a run's inputs, with secrets hidden;
// clamp cuts a long one to three lines.
export function ValueList({ values, secrets, className, clamp = false }: { values: [string, unknown][]; secrets: string[]; className?: string; clamp?: boolean }) {
  return <dl className={cn("grid grid-cols-[max-content_minmax(0,1fr)] gap-x-4 gap-y-1.5 text-[13px]", className)}>
    {values.map(([key, value]) => {
      const text = formatValue(value, secrets)
      const json = inputText(value).startsWith("{")
      return <div key={key} className="contents">
        <dt className="pt-px font-lw-mono text-xs text-lw-faint">{key.replaceAll("_", " ")}</dt>
        {clamp && !json && longValue(text) ? <dd className="min-w-0"><ClampedValue text={text} /></dd>
          : <dd className={cn("min-w-0 whitespace-pre-wrap break-words", json && "max-h-48 overflow-auto font-lw-mono text-xs")}>{text}</dd>}
      </div>
    })}
  </dl>
}

// ClampedValue is a long text cut to three lines, with Show all.
export function ClampedValue({ text }: { text: string }) {
  const [open, setOpen] = useState(false)
  return <>
    <span className={cn("block whitespace-pre-wrap break-words", !open && "line-clamp-3")}>{text}</span>
    <button type="button" className="mt-0.5 text-xs text-lw-link hover:underline" onClick={() => setOpen(!open)}>{open ? "Show less" : "Show all"}</button>
  </>
}
