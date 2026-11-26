import { cleanup, render, screen, waitFor } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"
import { EditorView, runScopeHandlers } from "@codemirror/view"
import WorkflowYamlEditor from "../WorkflowYamlEditor"
import { mergeDiagnostics, toDiagnostics } from "../editorDiagnostics"

afterEach(cleanup)

const text = "schema_version: 1\nname: x\ntitle: X\nsteps:\n  - id: a\n    type: agent.nope\n"

it("turns the server's lines and columns into offsets", () => {
  const [d] = toDiagnostics(text, [{ severity: "error", message: "bad type", line: 6, column: 11, end_line: 6, end_column: 21 }])
  expect(text.slice(d.from, d.to)).toBe("agent.nope")
})

// A diagnostic Legato can fix (the deprecated inline review step, LEG-599)
// carries the fix as the editor's quick fix.
it("offers a fixable diagnostic's fix as a quick fix", () => {
  const fixes: string[] = []
  const [fixable, plain] = toDiagnostics(text, [
    { severity: "warning", message: "deprecated", line: 6, column: 11, end_line: 6, end_column: 21, fix: "upgrade" },
    { severity: "error", message: "bad type", line: 6, column: 11, end_line: 6, end_column: 21 },
  ], fix => fixes.push(fix))
  expect(fixable.actions?.map(a => a.name)).toEqual(["Rewrite to call review-loop"])
  fixable.actions![0].apply({} as EditorView, fixable.from, fixable.to)
  expect(fixes).toEqual(["upgrade"])
  expect(plain.actions).toBeUndefined()
})

it("lets the server's diagnostics win on their lines", () => {
  const server = toDiagnostics(text, [{ severity: "error", message: "server", line: 6, column: 11, end_line: 6, end_column: 21 }])
  const lineSix = text.indexOf("agent.nope")
  const merged = mergeDiagnostics(text, server, [
    { from: lineSix, to: lineSix + 5, severity: "error", message: "schema, same line" },
    { from: 0, to: 3, severity: "warning", message: "schema, line one" },
  ])
  expect(merged.map(d => d.message)).toEqual(["server", "schema, line one"])
})

it("draws the file's text in CodeMirror", async () => {
  render(<WorkflowYamlEditor value={text} onChange={() => {}} />)
  const content = await waitFor(() => screen.getByRole("textbox", { name: "Workflow YAML" }))
  expect(content.textContent).toContain("agent.nope")
})

const viewOf = () => EditorView.findFromDOM(screen.getByRole("textbox", { name: "Workflow YAML" }))!

it("takes a change from outside as small edits, keeping the cursor and the text around it", async () => {
  const before = "# kept\nschema_version: 1\nname: x\ntitle: X\nsteps:\n  - id: a # the step\n    type: agent.run\n"
  const { rerender } = render(<WorkflowYamlEditor value={before} onChange={() => {}} />)
  await waitFor(() => screen.getByRole("textbox", { name: "Workflow YAML" }))
  const view = viewOf()
  const cursor = before.indexOf("agent.run")
  view.dispatch({ selection: { anchor: cursor } })
  const after = before.replace("title: X", "title: A longer title")
  rerender(<WorkflowYamlEditor value={after} onChange={() => {}} />)
  expect(view.state.doc.toString()).toBe(after)
  expect(view.state.selection.main.head).toBe(after.indexOf("agent.run"))
})

it("sends Undo and Redo to the page's history, not its own", async () => {
  const onUndo = vi.fn()
  const onRedo = vi.fn()
  const onChange = vi.fn()
  render(<WorkflowYamlEditor value={text} onChange={onChange} onUndo={onUndo} onRedo={onRedo} />)
  await waitFor(() => screen.getByRole("textbox", { name: "Workflow YAML" }))
  const view = viewOf()
  view.dispatch({ changes: { from: 0, insert: "# typed\n" }, userEvent: "input.type" })
  expect(onChange).toHaveBeenLastCalledWith("# typed\n" + text)
  const key = (init: KeyboardEventInit) => runScopeHandlers(view, new KeyboardEvent("keydown", init), "editor")
  expect(key({ key: "z", ctrlKey: true })).toBe(true)
  expect(onUndo).toHaveBeenCalledTimes(1)
  expect(key({ key: "Z", keyCode: 90, ctrlKey: true, shiftKey: true })).toBe(true)
  expect(key({ key: "y", ctrlKey: true })).toBe(true)
  expect(onRedo).toHaveBeenCalledTimes(2)
  // Its own history would have taken the typing back out.
  expect(view.state.doc.toString()).toBe("# typed\n" + text)
})

it("does not report a change from outside as an edit", async () => {
  const onChange = vi.fn()
  const { rerender } = render(<WorkflowYamlEditor value={text} onChange={onChange} />)
  await waitFor(() => screen.getByRole("textbox", { name: "Workflow YAML" }))
  rerender(<WorkflowYamlEditor value={text.replace("X", "Y")} onChange={onChange} />)
  expect(onChange).not.toHaveBeenCalled()
})

it("moves its cursor to a range it is sent to, and says where its cursor is", async () => {
  const onCursor = vi.fn()
  const { rerender } = render(<WorkflowYamlEditor value={text} onChange={() => {}} onCursor={onCursor} />)
  await waitFor(() => screen.getByRole("textbox", { name: "Workflow YAML" }))
  const view = viewOf()
  const from = text.indexOf("- id: a")
  rerender(<WorkflowYamlEditor value={text} onChange={() => {}} onCursor={onCursor} jumpTo={{ from, to: from + 7, seq: 1 }} />)
  expect(view.state.selection.main.from).toBe(from)
  expect(view.state.selection.main.to).toBe(from + 7)
  view.dispatch({ selection: { anchor: text.indexOf("agent.nope") } })
  expect(onCursor).toHaveBeenLastCalledWith(text.indexOf("agent.nope"))
})
