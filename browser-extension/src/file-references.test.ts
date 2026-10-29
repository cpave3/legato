// @vitest-environment jsdom
import { describe, expect, it } from "vitest"
import { linkFileReferences, type FileReference } from "./file-references"
import { renderSafeMarkdown } from "./markdown"

function rendered(markdown: string): { root: HTMLElement; opened: FileReference[] } {
  const root = document.createElement("div")
  renderSafeMarkdown(root, markdown)
  const opened: FileReference[] = []
  linkFileReferences(root, (reference) => opened.push(reference))
  return { root, opened }
}

function clickAll(root: HTMLElement): void {
  for (const button of root.querySelectorAll<HTMLButtonElement>("button.file-ref")) button.click()
}

describe("linkFileReferences", () => {
  it("opens a path and line written as inline code", () => {
    const { root, opened } = rendered("See `internal/service/workflow.go:120` for the check.")
    const button = root.querySelector<HTMLButtonElement>("button.file-ref")!
    expect(button.textContent).toBe("internal/service/workflow.go:120")
    button.click()
    expect(opened).toEqual([{ path: "internal/service/workflow.go", line: 120 }])
  })

  it("takes the first line of a range, with a hyphen or a dash, and GitHub's #L form", () => {
    const { root, opened } = rendered("`a/B.cs:47-53`, `C.cs:59–60` and `d/e.ts#L8-L12`.")
    clickAll(root)
    expect(opened).toEqual([
      { path: "a/B.cs", line: 47 },
      { path: "C.cs", line: 59 },
      { path: "d/e.ts", line: 8 },
    ])
  })

  it("finds a path and line in plain text too", () => {
    const { root, opened } = rendered("The guard in ./src/legato.ts:44 throws, then src/b.ts:3 retries.")
    clickAll(root)
    expect(opened).toEqual([{ path: "src/legato.ts", line: 44 }, { path: "src/b.ts", line: 3 }])
    expect(root.textContent?.trim()).toBe("The guard in ./src/legato.ts:44 throws, then src/b.ts:3 retries.")
  })

  it("opens a file named without a line when it is code with a folder", () => {
    const { root, opened } = rendered("Only `src/content/github.ts` changed.")
    clickAll(root)
    expect(opened).toEqual([{ path: "src/content/github.ts" }])
  })

  it("leaves code blocks, links, URLs and words that only look like files alone", () => {
    const { root } = rendered([
      "Node.js and e.g. `config.yaml` stay text; so does https://github.com/a/b/blob/main/x.ts:3.",
      "",
      "[the file](https://example.com/x.ts:4)",
      "",
      "```",
      "src/a.ts:1",
      "```",
    ].join("\n"))
    expect(root.querySelector("button.file-ref")).toBeNull()
  })
})
