// @vitest-environment jsdom
import { createHash } from "node:crypto"
import { beforeEach, describe, expect, it } from "vitest"
import { jumpToFile } from "./pr-jump"

const fast = { patience: 0.02 }
const diffId = (path: string): string => "diff-" + createHash("sha256").update(path).digest("hex")

// A Files changed page as GitHub drew it in 2026: a file tree, and a diff per
// file whose cells carry data-line-number and data-diff-side.
function page(files: Record<string, number[]>): void {
  const tree = Object.keys(files).map((path) => `<li role="treeitem" id="${path}"><a href="#${diffId(path)}">${path.split("/").pop()}</a></li>`).join("")
  const diffs = Object.entries(files).map(([path, lines]) => `
    <div id="${diffId(path)}">
      <h3><a href="#${diffId(path)}">${path}</a></h3>
      <table>${lines.map((line) => `<tr data-line="${line}"><td data-line-number="${line}" data-diff-side="left"></td><td data-line-number="${line}" data-diff-side="right"></td></tr>`).join("")}</table>
    </div>`).join("")
  document.body.innerHTML = `<div id="diff-comparison-viewer-container"><ul role="tree">${tree}</ul>${diffs}</div>`
}

const scrolled: Element[] = []
beforeEach(() => {
  scrolled.length = 0
  Element.prototype.scrollIntoView = function (this: Element) { scrolled.push(this) }
})

describe("jumpToFile", () => {
  it("shows the cited line of a file named by its name alone", async () => {
    page({ "services/Campaigns/UpdateCampaign.cs": [72, 73, 74], "apps/router.tsx": [1] })
    const result = await jumpToFile(window, { path: "UpdateCampaign.cs", line: 73 }, fast)
    expect(result).toEqual({ ok: true })
    const row = document.querySelector('tr[data-line="73"]')!
    expect(scrolled).toContain(row)
    expect(row.hasAttribute("data-legato-flash")).toBe(true)
  })

  it("finds a file cited by its full path in the agent's worktree", async () => {
    page({ "apps/router.tsx": [61] })
    const result = await jumpToFile(window, { path: "/home/me/autopilot.legato-wr-1-pr-9/apps/router.tsx", line: 61 }, fast)
    expect(result).toEqual({ ok: true })
    expect(scrolled).toContain(document.querySelector('tr[data-line="61"]'))
  })

  it("says so when a name matches more than one file", async () => {
    page({ "a/index.ts": [1], "b/index.ts": [1] })
    const result = await jumpToFile(window, { path: "index.ts", line: 1 }, fast)
    expect(result.ok).toBe(false)
    expect(result.message).toContain("a/index.ts")
    expect(result.message).toContain("b/index.ts")
  })

  it("shows the file when the line is outside the diff", async () => {
    page({ "src/a.ts": [1, 2, 3] })
    const result = await jumpToFile(window, { path: "src/a.ts", line: 400 }, fast)
    expect(result).toMatchObject({ ok: true, partial: true })
    expect(result.message).toContain("Line 400")
    expect(scrolled).toContain(document.getElementById(diffId("src/a.ts")))
  })

  it("says so when the pull request does not change the file", async () => {
    page({ "src/a.ts": [1] })
    const result = await jumpToFile(window, { path: "src/elsewhere.ts", line: 1 }, fast)
    expect(result.ok).toBe(false)
    expect(result.message).toContain("src/elsewhere.ts")
  })
})
