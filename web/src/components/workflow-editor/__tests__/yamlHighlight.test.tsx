import { cleanup, render, screen, waitFor } from "@testing-library/react"
import { afterEach, expect, it } from "vitest"
import { tags } from "@lezer/highlight"
import WorkflowYamlEditor from "../WorkflowYamlEditor"
import { yamlHighlightSpecs, yamlHighlightStyle } from "../yamlHighlight"

afterEach(cleanup)

// The YAML is drawn in the selected theme's syntax colours, falling back
// to Legato's own roles when the theme has none, never CodeMirror's fixed
// light-theme palette (dark blue keys on a dark editor).
it("colours YAML from the selected theme's syntax roles", () => {
  const colour = (tag: typeof tags.string) => yamlHighlightSpecs.find(spec => [spec.tag].flat().includes(tag))?.color
  expect(colour(tags.definition(tags.propertyName))).toBe("var(--theme-keyword, var(--color-lw-link))")
  expect(colour(tags.string)).toBe("var(--theme-string, var(--color-lw-live))")
  expect(colour(tags.lineComment)).toBe("var(--theme-comment, var(--color-lw-faint))")
  for (const spec of yamlHighlightSpecs) {
    expect(spec.color, String(spec.tag)).toMatch(/^var\(--(theme|color-lw)-[a-z0-9-]+(, var\(--color-lw-[a-z0-9-]+\))?\)$/)
  }
})

it("draws the editor's keys with that style", async () => {
  render(<WorkflowYamlEditor value={"name: news\n"} onChange={() => {}} />)
  const content = await waitFor(() => screen.getByRole("textbox", { name: "Workflow YAML" }))
  const key = yamlHighlightStyle.style([tags.definition(tags.propertyName)])!
  const keyText = () => [...content.querySelectorAll(`.${key.split(" ")[0]}`)].map(span => span.textContent).join("")
  await waitFor(() => expect(keyText()).toBe("name"))
})
