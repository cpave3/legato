import { useEffect, useRef } from "react"
import { EditorState } from "@codemirror/state"
import { EditorView, drawSelection, highlightActiveLine, hoverTooltip, keymap, lineNumbers } from "@codemirror/view"
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands"
import { bracketMatching, defaultHighlightStyle, indentOnInput, syntaxHighlighting } from "@codemirror/language"
import { autocompletion, completionKeymap, type CompletionContext, type CompletionResult } from "@codemirror/autocomplete"
import { forceLinting, linter, lintGutter, type Diagnostic } from "@codemirror/lint"
import { yaml } from "@codemirror/lang-yaml"
import { completionsAt, contextAt, describe, lenientValues, schemaDiagnostics } from "../../lib/workflowSchema"
import { expressionAt, expressionCompletions, expressionDiagnostics } from "../../lib/workflowExpressions"
import type { WorkflowDiagnostic } from "../../lib/workflowFiles"
import { mergeDiagnostics, toDiagnostics } from "./editorDiagnostics"

export type WorkflowYamlEditorProps = {
  value: string
  onChange: (value: string) => void
  /** serverDiagnostics are the server's check of the text: they take
   * precedence over the schema's on the lines they are on. */
  serverDiagnostics?: WorkflowDiagnostic[]
  readOnly?: boolean
  label?: string
}

function complete(context: CompletionContext): CompletionResult | null {
  const text = context.state.doc.toString()
  // Inside ${{ }}: what can be read there.
  const references = expressionCompletions(text, context.pos)
  if (references) {
    return {
      from: references.from,
      options: references.options.map(ref => ({ label: ref.expression, info: ref.description, type: "variable" })),
      validFor: /^[A-Za-z0-9_.-]*$/,
    }
  }
  const result = completionsAt(text, context.pos)
  if (!result || result.options.length === 0) return null
  return {
    from: result.from,
    options: result.options.map(option => ({ label: option.label, detail: option.detail, info: option.info, apply: option.apply, type: option.type })),
    validFor: /^[A-Za-z0-9_.-]*$/,
  }
}

function tooltip(text: string) {
  return () => {
    const dom = document.createElement("div")
    dom.className = "max-w-xs p-2 text-xs"
    dom.textContent = text
    return { dom }
  }
}

const hover = hoverTooltip((view, pos) => {
  const text = view.state.doc.toString()
  const reference = expressionAt(text, pos)
  if (reference) return { pos: reference.from, end: reference.to, above: true, create: tooltip(reference.description) }
  const line = view.state.doc.lineAt(pos)
  const key = /^(\s*(?:-\s+)*)([A-Za-z0-9_.-]+):/.exec(line.text)
  if (!key) return null
  const start = line.from + key[1].length
  const end = start + key[2].length
  if (pos < start || pos > end) return null
  const context = contextAt(text, end)
  if (!context) return null
  const info = describe([...context.path, key[2]], lenientValues(text))
  if (!info) return null
  return {
    pos: start, end, above: true, create: tooltip(info),
  }
})

const theme = EditorView.theme({
  "&": { height: "100%", fontSize: "13px", backgroundColor: "var(--color-lw-ground)", color: "var(--color-lw-text)" },
  ".cm-content": { fontFamily: "var(--font-lw-mono, ui-monospace, monospace)", caretColor: "var(--color-lw-text)" },
  ".cm-gutters": { backgroundColor: "var(--color-lw-surface)", color: "var(--color-lw-faint)", border: "none" },
  ".cm-activeLine": { backgroundColor: "color-mix(in srgb, var(--color-lw-raised) 60%, transparent)" },
  ".cm-activeLineGutter": { backgroundColor: "var(--color-lw-raised)" },
  ".cm-tooltip": { backgroundColor: "var(--color-lw-surface)", color: "var(--color-lw-text)", border: "1px solid var(--color-lw-line)" },
  "&.cm-focused .cm-selectionBackground, .cm-selectionBackground": { backgroundColor: "var(--color-lw-accent-soft)" },
})

// WorkflowYamlEditor is a workflow file's text in CodeMirror, with the
// schema's completion, hover and checks, and the server's diagnostics.
export default function WorkflowYamlEditor({ value, onChange, serverDiagnostics = [], readOnly = false, label = "Workflow YAML" }: WorkflowYamlEditorProps) {
  const host = useRef<HTMLDivElement>(null)
  const view = useRef<EditorView | null>(null)
  const changed = useRef(onChange)
  const server = useRef(serverDiagnostics)
  changed.current = onChange
  server.current = serverDiagnostics

  useEffect(() => {
    const lint = linter(v => {
      const text = v.state.doc.toString()
      const schema: Diagnostic[] = [...schemaDiagnostics(text), ...expressionDiagnostics(text)].map(d => ({ from: Math.min(d.from, text.length), to: Math.min(d.to, text.length), severity: d.severity, message: d.message, source: "schema" }))
      return mergeDiagnostics(text, toDiagnostics(text, server.current), schema)
    }, { delay: 250 })
    const state = EditorState.create({
      doc: value,
      extensions: [
        lineNumbers(), history(), drawSelection(), highlightActiveLine(), indentOnInput(), bracketMatching(),
        syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
        keymap.of([...defaultKeymap, ...historyKeymap, ...completionKeymap, indentWithTab]),
        yaml(), autocompletion({ override: [complete], activateOnTyping: true }), hover, lint, lintGutter(), theme,
        EditorState.tabSize.of(2), EditorState.readOnly.of(readOnly), EditorView.editable.of(!readOnly),
        EditorView.contentAttributes.of({ "aria-label": label }),
        EditorView.updateListener.of(update => { if (update.docChanged) changed.current(update.state.doc.toString()) }),
      ],
    })
    view.current = new EditorView({ state, parent: host.current! })
    return () => { view.current?.destroy(); view.current = null }
    // The editor is made once; value and diagnostics reach it below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [readOnly])

  // A value from outside (a reload) replaces the text.
  useEffect(() => {
    const current = view.current
    if (current && current.state.doc.toString() !== value) {
      current.dispatch({ changes: { from: 0, to: current.state.doc.length, insert: value } })
    }
  }, [value])

  useEffect(() => {
    if (view.current) forceLinting(view.current)
  }, [serverDiagnostics])

  return <div ref={host} className="h-full min-h-0 overflow-hidden" />
}
