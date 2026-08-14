import { isValidElement, useEffect, useRef, useState, type ReactNode } from "react"
import { MermaidMarkdownPre } from "./MermaidDiagram"

let wiremarkPromise: Promise<typeof import("@wiremark/core")> | undefined

function loadWiremark() {
  wiremarkPromise ??= import("@wiremark/core")
  return wiremarkPromise
}

function wiremarkCodeSource(children: ReactNode) {
  if (!isValidElement<{ className?: string; children?: ReactNode }>(children)) return null
  if (!/^language-wireframe(?:\s|$)/i.test(children.props.className ?? "")) return null
  const source = children.props.children
  return typeof source === "string" ? source : null
}

export function VisualMarkdownPre({ children, fallbackClassName }: { children?: ReactNode; fallbackClassName?: string }) {
  const source = wiremarkCodeSource(children)
  return source === null
    ? <MermaidMarkdownPre fallbackClassName={fallbackClassName}>{children}</MermaidMarkdownPre>
    : <WiremarkDiagram source={source}/>
}

export function WiremarkDiagram({ source }: { source: string }) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [error, setError] = useState<string | null>(null)
  const [expanded, setExpanded] = useState(false)

  useEffect(() => {
    const container = containerRef.current
    if (!container) return

    let current = true
    container.replaceChildren()
    setError(null)
    setExpanded(false)

    void loadWiremark()
      .then(({ render }) => render(source, { theme: "dark", interactive: true }))
      .then(({ svg }) => {
        if (current) container.innerHTML = svg
      })
      .catch(cause => {
        if (current) setError(cause instanceof Error ? cause.message : String(cause))
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

  if (error) return <div role="alert" className="my-3 rounded bg-black/60 p-3 text-sm text-amber-200">
    <p className="mb-1 font-medium">Mockup could not be rendered. Wiremark source:</p>
    <p className="mb-2 text-xs">{error}</p>
    <pre className="overflow-x-auto whitespace-pre font-mono text-xs text-zinc-300"><code>{source}</code></pre>
  </div>

  return <div
    role={expanded ? "dialog" : undefined}
    aria-label={expanded ? "Expanded Wiremark mockup" : undefined}
    aria-modal={expanded || undefined}
    className={expanded ? "fixed inset-0 z-50 flex items-center justify-center bg-black/85 p-4 md:p-10" : "my-3 min-w-0 max-w-full"}
  >
    <button
      type="button"
      aria-label={expanded ? "Collapse Wiremark mockup" : "Expand Wiremark mockup"}
      aria-pressed={expanded}
      onClick={() => setExpanded(current => !current)}
      className={`block max-h-full max-w-full cursor-zoom-in overflow-auto rounded bg-black/60 p-3 text-left ${expanded ? "h-full w-full cursor-zoom-out" : "w-full"}`}
    >
      <div ref={containerRef} role="img" aria-label="Wiremark mockup" className={`[&_svg]:mx-auto [&_svg]:h-auto [&_svg]:max-w-none ${expanded ? "min-h-full min-w-full [&_svg]:min-w-full" : "min-w-max"}`} />
    </button>
  </div>
}
