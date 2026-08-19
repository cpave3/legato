import DOMPurify from "dompurify"
import { marked } from "marked"

const forbiddenTags = ["script", "style", "iframe", "object", "embed", "form", "input", "button", "textarea", "select", "img", "video", "audio", "source", "svg", "math"]

export function renderSafeMarkdown(root: HTMLElement, markdown: string): void {
  const rendered = marked.parse(markdown, { async: false })
  root.innerHTML = DOMPurify.sanitize(rendered, {
    FORBID_TAGS: forbiddenTags,
    FORBID_ATTR: ["style", "src", "srcset", "xlink:href", "onerror", "onload"],
  })
  for (const link of root.querySelectorAll<HTMLAnchorElement>("a")) {
    let url: URL
    try { url = new URL(link.href) } catch { link.removeAttribute("href"); continue }
    if (url.protocol !== "http:" && url.protocol !== "https:") {
      link.removeAttribute("href")
      continue
    }
    link.target = "_blank"
    link.rel = "noopener noreferrer"
  }
}
