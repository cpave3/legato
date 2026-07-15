import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { DiffView } from "../DiffView"

const files = [{
  old_path: "src/old.ts",
  new_path: "src/new.ts",
  status: "renamed" as const,
  hunks: [{
    header: "@@ -1,2 +1,2 @@",
    lines: [
      { kind: "del" as const, old_no: 1, new_no: 0, text: "const oldName = true" },
      { kind: "add" as const, old_no: 0, new_no: 1, text: "const newName = true" },
    ],
  }],
}]

describe("DiffView", () => {
  it("renders file metadata, hunk headers, line numbers, and changed content", () => {
    render(<DiffView files={files} />)

    expect(screen.getByText("src/old.ts → src/new.ts")).toBeTruthy()
    expect(screen.getByText("renamed")).toBeTruthy()
    expect(screen.getByText("@@ -1,2 +1,2 @@")).toBeTruthy()
    expect(screen.getByText("const oldName = true")).toBeTruthy()
    expect(screen.getByText("const newName = true")).toBeTruthy()
    expect(screen.getByLabelText("old line 1")).toBeTruthy()
    expect(screen.getByLabelText("new line 1")).toBeTruthy()
  })

  it("shows an empty state when a step has no diff", () => {
    render(<DiffView files={[]} />)
    expect(screen.getByText("No file changes in this step.")).toBeTruthy()
  })
})
