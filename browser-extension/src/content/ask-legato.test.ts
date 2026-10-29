// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest"
import { describeLines, lineRange, reconcileAskButtons } from "./ask-legato"

const anchor = "diff-536907a2"
const path = "services/agx.crm/Agx.Crm.Vault/Adapters/SyncProperty.cs"

// GitHub's inline comment form, cut down to what the button relies on.
function commentForm(side = "right", line = 109): HTMLElement {
  document.body.innerHTML = `
    <table aria-label="Diff for: ${path}" data-diff-anchor="${anchor}">
      <tbody><tr class="diff-line-row">
        <td role="dialog" data-diff-side="${side}" data-line-number="${line}">
          <div data-marker-id="new-comment">
            <textarea placeholder="Leave a comment"></textarea>
            <div data-testid="markdown-editor-footer">
              <div><button type="button">Paste, drop, or click to add files</button></div>
              <div class="actions"><button type="button">Cancel</button><div><button type="button">Comment</button></div></div>
            </div>
          </div>
        </td>
      </tr></tbody>
    </table>`
  return document.querySelector<HTMLElement>("td[role=dialog]")!
}

const pr = "https://github.com/autopilot-dev/autopilot/pull/1497/changes"

describe("lineRange", () => {
  it("reads a range the person selected from the address", () => {
    expect(lineRange(commentForm(), `#${anchor}R104-R109`)).toEqual({ start: 104, end: 109, side: "right" })
  })

  it("takes the form's own line when the address names no range of this file", () => {
    expect(lineRange(commentForm("right", 42), "#diff-otherfileR1-R3")).toEqual({ start: 42, end: 42, side: "right" })
    expect(lineRange(commentForm("left", 12), "")).toEqual({ start: 12, end: 12, side: "left" })
  })

  it("ignores a range in the address that ends somewhere else", () => {
    expect(lineRange(commentForm("right", 200), `#${anchor}R104-R109`)).toEqual({ start: 200, end: 200, side: "right" })
  })

  it("reads a single selected line", () => {
    expect(lineRange(commentForm("right", 104), `#${anchor}R104`)).toEqual({ start: 104, end: 104, side: "right" })
  })
})

describe("describeLines", () => {
  it("names the file and the lines, and says when they are the base's", () => {
    expect(describeLines(path, { start: 104, end: 109, side: "right" })).toBe(`In \`${path}\`, lines 104–109: `)
    expect(describeLines(path, { start: 7, end: 7, side: "right" })).toBe(`In \`${path}\`, line 7: `)
    expect(describeLines(path, { start: 12, end: 14, side: "left" })).toBe(`In \`${path}\`, lines 12–14 of the base (before this PR): `)
  })
})

describe("reconcileAskButtons", () => {
  beforeEach(() => { document.body.innerHTML = "" })

  it("puts one Ask Legato button before Cancel in each comment form", () => {
    commentForm()
    const send = vi.fn().mockResolvedValue({ ok: true })

    reconcileAskButtons(document, pr, () => `#${anchor}R104-R109`, send)
    reconcileAskButtons(document, pr, () => `#${anchor}R104-R109`, send)

    const buttons = Array.from(document.querySelectorAll<HTMLButtonElement>(".actions button")).map((button) => button.textContent)
    expect(buttons).toEqual(["♩Ask Legato", "Cancel", "Comment"])
  })

  it("opens the discussion with the file and lines ready in the message box", async () => {
    commentForm()
    const send = vi.fn().mockResolvedValue({ ok: true })
    reconcileAskButtons(document, pr, () => `#${anchor}R104-R109`, send)

    document.querySelector<HTMLButtonElement>("[data-legato-ask]")!.click()

    await vi.waitFor(() => expect(send).toHaveBeenCalledWith({
      type: "openReviewPanel",
      view: "discussion",
      context: { repository: "autopilot-dev/autopilot", pullRequestNumber: 1497 },
      draft: `In \`${path}\`, lines 104–109: `,
    }))
  })

  it("adds nothing outside a pull request", () => {
    commentForm()
    reconcileAskButtons(document, "https://github.com/autopilot-dev/autopilot/blob/main/README.md", () => "", vi.fn())

    expect(document.querySelector("[data-legato-ask]")).toBeNull()
  })

  it("leaves a form it cannot place alone", () => {
    commentForm()
    document.querySelector("table")!.removeAttribute("aria-label")

    reconcileAskButtons(document, pr, () => "", vi.fn())

    expect(document.querySelector("[data-legato-ask]")).toBeNull()
  })
})
