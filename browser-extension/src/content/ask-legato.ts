// An "Ask Legato" button in GitHub's inline comment form: it opens the side
// panel's discussion with the file and the lines the comment is on already in
// the message box, for the person to finish the question.
import { parseGitHubPullRequestUrl } from "../review-context"
import { LEGATO_SANS, loadLegatoFonts } from "./fonts"

export type LineRange = { start: number; end: number; side: "left" | "right" }

type SendMessage = (message: unknown) => Promise<{ ok: boolean; error?: string }>

const MARK = "data-legato-ask"

// The lines a comment form is about. GitHub keeps a selected range in the
// address (#diff-<file>R104-R109); the form itself knows only the line it
// ends on, which is what a single-line comment has.
export function lineRange(dialog: HTMLElement, hash: string): LineRange | null {
  const end = Number(dialog.dataset.lineNumber)
  const side = dialog.dataset.diffSide === "left" ? "left" : "right"
  if (!Number.isSafeInteger(end) || end < 1) return null
  const anchor = dialog.closest("table")?.getAttribute("data-diff-anchor")
  const match = anchor ? hash.match(/^#(.+?)([LR])(\d+)(?:-([LR])(\d+))?$/) : null
  if (match && match[1] === anchor) {
    const endSide = (match[4] ?? match[2]) === "L" ? "left" : "right"
    const endLine = Number(match[5] ?? match[3])
    if (endSide === side && endLine === end) return { start: Math.min(Number(match[3]), end), end, side }
  }
  return { start: end, end, side }
}

export function describeLines(path: string, range: LineRange): string {
  const lines = range.start === range.end ? `line ${range.start}` : `lines ${range.start}–${range.end}`
  const base = range.side === "left" ? " of the base (before this PR)" : ""
  return `In \`${path}\`, ${lines}${base}: `
}

function filePath(dialog: HTMLElement): string | null {
  const label = dialog.closest("table")?.getAttribute("aria-label") ?? ""
  const path = label.replace(/^Diff for:\s*/, "").trim()
  return path && path !== label.trim() ? path : null
}

export function reconcileAskButtons(doc: Document, href: string, getHash: () => string, sendMessage: SendMessage): void {
  const context = parseGitHubPullRequestUrl(href)
  if (!context) return
  for (const dialog of doc.querySelectorAll<HTMLElement>("td[role='dialog']")) {
    if (dialog.querySelector(`[${MARK}]`)) continue
    const footer = dialog.querySelector<HTMLElement>("[data-testid='markdown-editor-footer']")
    const cancel = Array.from(footer?.querySelectorAll<HTMLButtonElement>("button") ?? []).find((button) => button.textContent?.trim() === "Cancel")
    if (!cancel || !filePath(dialog) || !lineRange(dialog, getHash())) continue
    const button = askButton(doc)
    button.addEventListener("click", () => {
      // Read when clicked: the selection may have changed since.
      const path = filePath(dialog)
      const range = lineRange(dialog, getHash())
      if (!path || !range) return
      void sendMessage({ type: "openReviewPanel", view: "discussion", context, draft: describeLines(path, range) }).then((response) => {
        if (!response.ok) throw new Error(response.error)
      }).catch((error: unknown) => {
        button.title = error instanceof Error ? error.message : String(error)
      })
    })
    cancel.before(button)
  }
}

function askButton(doc: Document): HTMLButtonElement {
  loadLegatoFonts(doc)
  const button = doc.createElement("button")
  button.type = "button"
  button.setAttribute(MARK, "true")
  button.setAttribute("aria-label", "Ask about these lines in the Legato discussion")
  button.title = "Ask about these lines in the Legato discussion"
  const mark = doc.createElement("span")
  mark.setAttribute("aria-hidden", "true")
  mark.textContent = "♩"
  mark.style.cssText = `display:inline-grid;place-items:center;width:18px;height:18px;margin-right:6px;border-radius:4px;background:#6d5ef6;color:#fff;font:600 12px/1 ${LEGATO_SANS}, system-ui, sans-serif`
  button.append(mark, doc.createTextNode("Ask Legato"))
  button.style.cssText = [
    "display:inline-flex", "align-items:center", "box-sizing:border-box", "height:32px", "margin-right:8px", "padding:0 12px",
    "border:1px solid #31313a", "border-radius:6px", "background-color:#17171b", "color:#e6e5eb",
    `font:500 13px/1 ${LEGATO_SANS}, system-ui, -apple-system, sans-serif`, "cursor:pointer", "vertical-align:middle",
  ].join(";")
  button.addEventListener("mouseenter", () => { button.style.backgroundColor = "#1d1d22" })
  button.addEventListener("mouseleave", () => { button.style.backgroundColor = "#17171b" })
  return button
}
