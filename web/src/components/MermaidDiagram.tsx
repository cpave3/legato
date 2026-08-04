import { isValidElement, useEffect, useRef, useState, type ReactNode } from "react"

let nextDiagramID = 0
let mermaidPromise: Promise<typeof import("mermaid").default> | undefined

function loadMermaid() {
  mermaidPromise ??= import("mermaid").then(({ default: mermaid }) => {
    mermaid.initialize({
      startOnLoad: false,
      theme: "dark",
      securityLevel: "strict",
    })
    return mermaid
  })
  return mermaidPromise
}

function mermaidCodeSource(children: ReactNode) {
  if (!isValidElement<{ className?: string; children?: ReactNode }>(children)) return null
  if (!/^language-mermaid(?:\s|$)/i.test(children.props.className ?? "")) return null
  const source = children.props.children
  return typeof source === "string" ? source : null
}

export function MermaidMarkdownPre({ children }: { children?: ReactNode }) {
  const source = mermaidCodeSource(children)
  return source === null
    ? <pre className="my-3 overflow-x-auto rounded bg-black/40 p-3">{children}</pre>
    : <MermaidDiagram source={source}/>
}

export function MermaidDiagram({ source }: { source: string }) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [failed, setFailed] = useState(false)
  const [expanded, setExpanded] = useState(false)

  useEffect(() => {
    const container = containerRef.current
    if (!container) return

    const renderID = `mermaid-diagram-${++nextDiagramID}`
    let current = true
    container.replaceChildren()
    setFailed(false)
    setExpanded(false)

    void loadMermaid()
      .then(mermaid => mermaid.render(renderID, source))
      .then(({ svg, bindFunctions }) => {
        if (!current) return
        container.innerHTML = svg
        bindFunctions?.(container)
      })
      .catch(() => {
        if (current) setFailed(true)
      })

    return () => {
      current = false
    }
  }, [source])

  useEffect(() => {
    if (!expanded) return
    const collapse = (event: globalThis.KeyboardEvent) => {
      if (event.key === "Escape") setExpanded(false)
    }
    document.addEventListener("keydown", collapse)
    return () => document.removeEventListener("keydown", collapse)
  }, [expanded])

  return <div
    role={expanded ? "dialog" : undefined}
    aria-label={expanded ? "Expanded Mermaid diagram" : undefined}
    aria-modal={expanded || undefined}
    className={expanded ? "fixed inset-0 z-50 flex items-center justify-center bg-black/85 p-4 md:p-10" : "my-3 min-w-0 max-w-full"}
  >
    <button
      type="button"
      aria-label={expanded ? "Collapse Mermaid diagram" : "Expand Mermaid diagram"}
      aria-pressed={expanded}
      onClick={() => setExpanded(current => !current)}
      className={`block max-h-full max-w-full cursor-zoom-in overflow-auto rounded bg-black/60 p-3 text-left ${expanded ? "h-full w-full cursor-zoom-out" : "w-full"}`}
    >
      <div ref={containerRef} role="img" aria-label="Mermaid diagram" className={`[&_svg]:mx-auto [&_svg]:max-w-none ${expanded ? "min-h-full min-w-full [&_svg]:h-auto [&_svg]:min-w-full" : "min-w-max"}`} />
    </button>
    {failed && <div role="alert" className="text-sm text-amber-200">
      <p className="mb-2 font-medium">Diagram could not be rendered. Mermaid source:</p>
      <pre className="overflow-x-auto whitespace-pre font-mono text-xs text-zinc-300"><code>{source}</code></pre>
    </div>}
  </div>
}
