import { cleanup, render, screen, waitFor } from "@testing-library/react"
import { afterEach, expect, it } from "vitest"
import WorkflowYamlEditor from "../WorkflowYamlEditor"
import { mergeDiagnostics, toDiagnostics } from "../editorDiagnostics"

afterEach(cleanup)

const text = "schema_version: 1\nname: x\ntitle: X\nsteps:\n  - id: a\n    type: agent.nope\n"

it("turns the server's lines and columns into offsets", () => {
  const [d] = toDiagnostics(text, [{ severity: "error", message: "bad type", line: 6, column: 11, end_line: 6, end_column: 21 }])
  expect(text.slice(d.from, d.to)).toBe("agent.nope")
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
