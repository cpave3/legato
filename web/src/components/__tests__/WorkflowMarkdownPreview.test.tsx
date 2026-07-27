import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { WorkflowMarkdownPreview } from "../WorkflowMarkdownPreview"

describe("WorkflowMarkdownPreview", () => {
  it("gives review documents readable vertical rhythm", () => {
    render(<WorkflowMarkdownPreview markdown={"# Review\n\nFirst paragraph.\n\n- Finding one\n- Finding two"} />)

    expect(screen.getByRole("heading", { name: "Review" }).className).toContain("mb-4")
    expect(screen.getByText("First paragraph.").className).toContain("my-4")
    expect(screen.getByRole("list").className).toContain("space-y-2")
  })
})
