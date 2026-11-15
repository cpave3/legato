// Browser-only fixture: real public components, no server, sessions or review capture.
import { useState } from "react"
import { createRoot } from "react-dom/client"
import "../index.css"
import { AppThemePicker } from "../components/settings/AppThemePicker"
import { applyAppTheme, subscribeTheme } from "../lib/appTheme"
import { DiffView } from "../components/DiffView"
import { MarkdownBody } from "../components/MarkdownBody"
import { StatePill } from "../components/WorkflowRunStatus"
import { AgentUIComponents } from "../components/AgentUIComponents"
import type { DiffSelection, FileDiff } from "../lib/review"
import { Button } from "../components/ui/Button"

applyAppTheme()
subscribeTheme(applyAppTheme)
const files: FileDiff[] = [{ old_path: "example.ts", new_path: "example.ts", status: "modified", hunks: [{ anchor: "example", header: "@@ -1,3 +1,3 @@", lines: [
  { kind: "ctx", old_no: 1, new_no: 1, text: "// Comments and unchanged context" },
  { kind: "del", old_no: 2, new_no: 0, text: 'const theme = "old"' },
  { kind: "add", old_no: 0, new_no: 2, text: 'const theme = "ocular"' },
] }] }]
export function Preview() {
  const [selection, setSelection] = useState<DiffSelection | null>(null)
  return <main className="mx-auto w-full max-w-3xl space-y-6 bg-lw-ground p-4 text-lw-text">
    <h1 className="text-xl font-semibold">Theme component verification</h1>
    <AppThemePicker />
    <div className="flex flex-wrap gap-2" data-contrast="status">
      <StatePill tone="live" label="Running" /><StatePill tone="needs" label="Needs you" /><StatePill tone="done" label="Done" /><StatePill tone="error" label="Error" /><StatePill tone="idle" label="Idle" />
    </div>
    <div className="flex flex-wrap gap-2" data-contrast="buttons"><Button>Save</Button><Button variant="destructive">Delete</Button><Button variant="secondary">Cancel</Button></div>
    <div data-contrast="input"><label>Filter tasks<input className="ml-2 rounded border border-lw-line bg-lw-surface px-2 py-1 text-lw-text" placeholder="Type a task title" /></label></div>
    <div data-contrast="markdown"><MarkdownBody body={'A [link](#), `inline code` and body text.\n\n```ts\n// Keep comments readable\nconst count = 42\n```'} /></div>
    <div data-contrast="diff"><DiffView files={files} selection={selection} onSelectionChange={setSelection} /></div>
    <div data-contrast="components"><AgentUIComponents components={[{ type: "text", text: "Error: the task could not be saved", tone: "negative" }, { type: "badges", items: [{ text: "Success", tone: "positive" }, { text: "Warning", tone: "warning" }, { text: "Info", tone: "info" }] }]} /></div>
  </main>
}
createRoot(document.getElementById("root")!).render(<Preview />)
