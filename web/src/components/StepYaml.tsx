import { useState } from "react"
import { Check, Copy } from "lucide-react"
import { Button } from "./ui/Button"

// StepYaml is a step's own YAML, read-only, with Copy, and the file it is
// in (the YAML is the source of truth: edit it there).
export function StepYaml({ yaml, source }: { yaml: string; source?: string }) {
  const [copied, setCopied] = useState<"" | "yes" | "failed">("")
  const copy = async () => {
    try { await navigator.clipboard.writeText(yaml); setCopied("yes") } catch { setCopied("failed") }
    setTimeout(() => setCopied(""), 2000)
  }
  return <div className="flex min-w-0 flex-col gap-2">
    <div className="flex items-center justify-between gap-2">
      <span className="text-xs font-semibold uppercase tracking-wide text-lw-text-2">YAML</span>
      <Button variant="ghost" size="sm" aria-label="Copy YAML" onClick={() => void copy()}>{copied === "yes" ? <Check size={13} /> : <Copy size={13} />}{copied === "yes" ? "Copied" : copied === "failed" ? "Could not copy" : "Copy"}</Button>
    </div>
    <pre aria-label="Step YAML" className="max-h-80 overflow-auto rounded-lg border border-lw-line bg-lw-ground p-3 font-lw-mono text-xs text-lw-text">{yaml}</pre>
    {source && <p className="break-all font-lw-mono text-[11px] text-lw-faint">{source}</p>}
  </div>
}
