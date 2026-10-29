// Turns the files an agent cites (`internal/service/workflow.go:120`) into
// buttons that show that place in the pull request.
export type FileReference = { path: string; line?: number }

const PATH = String.raw`(?:\.{0,2}/)?(?:[\w@~+-][\w.@~+-]*/)*[\w@~+-][\w.@~+-]*\.[A-Za-z0-9]+`
const LINE = String.raw`(?::(\d+)(?:[-–]\d+)?(?::\d+)?|#L(\d+)(?:-L?\d+)?)`
// Inline code that is nothing but a reference; the line may be left out when
// the path names a folder.
const WHOLE = new RegExp(String.raw`^(${PATH})${LINE}?$`)
// In running text a reference needs its line, and must not be part of a
// longer word or address.
const IN_TEXT = new RegExp(String.raw`(?<![\w/.:@~+-])(${PATH})${LINE}`, "g")

function reference(path: string, line?: string): FileReference {
  const clean = path.replace(/^\.\//, "")
  return line ? { path: clean, line: Number(line) } : { path: clean }
}

export function linkFileReferences(root: HTMLElement, onOpen: (reference: FileReference) => void): void {
  const doc = root.ownerDocument
  const button = (target: FileReference, content: Node): HTMLButtonElement => {
    const node = doc.createElement("button")
    node.type = "button"
    node.className = "file-ref"
    node.title = target.line ? `Show ${target.path} line ${target.line} in the pull request` : `Show ${target.path} in the pull request`
    node.append(content)
    node.addEventListener("click", () => onOpen(target))
    return node
  }

  for (const code of root.querySelectorAll("code")) {
    if (code.closest("pre, a, button")) continue
    const match = WHOLE.exec(code.textContent?.trim() ?? "")
    if (!match) continue
    const line = match[2] ?? match[3]
    if (!line && !match[1].includes("/")) continue
    code.replaceWith(button(reference(match[1], line), code.cloneNode(true)))
  }

  const walker = doc.createTreeWalker(root, NodeFilter.SHOW_TEXT)
  const texts: Text[] = []
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    if (!node.parentElement?.closest("pre, code, a, button")) texts.push(node as Text)
  }
  for (const text of texts) {
    const value = text.data
    const parts: Node[] = []
    let last = 0
    for (const match of value.matchAll(IN_TEXT)) {
      const start = match.index
      parts.push(doc.createTextNode(value.slice(last, start)))
      parts.push(button(reference(match[1], match[2] ?? match[3]), doc.createTextNode(match[0])))
      last = start + match[0].length
    }
    if (!parts.length) continue
    parts.push(doc.createTextNode(value.slice(last)))
    text.replaceWith(...parts)
  }
}
