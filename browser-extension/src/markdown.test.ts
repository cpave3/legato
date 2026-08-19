// @vitest-environment jsdom
import { describe, expect, it } from "vitest"
import { renderSafeMarkdown } from "./markdown"

describe("renderSafeMarkdown", () => {
  it("renders useful Markdown structure", () => {
    const root = document.createElement("div")
    renderSafeMarkdown(root, "# Findings\n\n- one\n- two\n\n| A | B |\n|---|---|\n| 1 | 2 |\n\n```ts\nconst x = 1\n```\n")
    expect(root.querySelector("h1")?.textContent).toBe("Findings")
    expect(root.querySelectorAll("li")).toHaveLength(2)
    expect(root.querySelector("table")).not.toBeNull()
    expect(root.querySelector("code")?.textContent).toContain("const x")
  })

  it("removes executable HTML, remote images, forms, and unsafe links", () => {
    const root = document.createElement("div")
    renderSafeMarkdown(root, `
[bad](javascript:alert(1))

[good](https://example.com/path)

<script>alert(1)</script>

<img src="https://tracker.test/pixel" onerror="alert(1)" alt="tracker">

<form action="https://evil.test"><input autofocus></form>

<iframe src="https://evil.test"></iframe>

<svg><image href="https://tracker.test/svg"></image></svg>

<math><mtext href="https://tracker.test/math">x</mtext></math>
`)
    expect(root.querySelector("script,img,form,input,iframe,svg,math")).toBeNull()
    expect(root.querySelector('a[href^="javascript:"]')).toBeNull()
    expect(root.textContent).toContain("bad")
    const link = root.querySelector<HTMLAnchorElement>('a[href="https://example.com/path"]')!
    expect(link.target).toBe("_blank")
    expect(link.rel).toBe("noopener noreferrer")
  })
})
