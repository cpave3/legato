import { useEffect, useId, useRef, useState } from "react"

let wiremarkPromise: Promise<typeof import("@wiremark/core")> | undefined

function loadWiremark() {
  wiremarkPromise ??= import("@wiremark/core")
  return wiremarkPromise
}

function namespaceSVG(svg: string, namespace: string) {
  const document = new DOMParser().parseFromString(svg, "image/svg+xml")
  const root = document.documentElement
  if (root.tagName.toLowerCase() !== "svg" || document.querySelector("parsererror")) throw new Error("Wiremark produced invalid SVG")

  const ids = new Map<string, string>()
  for (const element of root.querySelectorAll("[id]")) {
    const id = element.id
    const namespaced = `${namespace}-${id}`
    ids.set(id, namespaced)
    element.id = namespaced
  }
  for (const element of root.querySelectorAll("*")) {
    for (const attribute of [...element.attributes]) {
      const value = attribute.value.replace(/url\(#([^)]+)\)/g, (match, id: string) => ids.has(id) ? `url(#${ids.get(id)})` : match)
      if (value !== attribute.value) element.setAttribute(attribute.name, value)
    }
  }
  return new XMLSerializer().serializeToString(root)
}

export function WiremarkDiagram({ source }: { source: string }) {
  const namespace = `wiremark-${useId().replace(/[^a-zA-Z0-9_-]/g, "")}`
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
        if (current) container.innerHTML = namespaceSVG(svg, namespace)
      })
      .catch(cause => {
        if (current) setError(cause instanceof Error ? cause.message : String(cause))
      })

    return () => {
      current = false
    }
  }, [namespace, source])

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
