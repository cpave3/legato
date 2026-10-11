import { useEffect, useMemo, useRef, useState } from "react"
import { buildSandboxDocument, proposedMessageText } from "../lib/agentUi"
import { cn } from "../lib/utils"

interface SandboxedHtmlProps {
  // HTML an agent wrote. It is never trusted: see the sandbox below.
  html: string
  // The frame's accessible name.
  title: string
  // Hosts the content may load scripts, styles, fonts and images from.
  resourceDomains: string[]
  // fill: take the container's height and let the content scroll inside.
  // Otherwise the frame is as tall as its content, up to maxHeight.
  fill?: boolean
  maxHeight?: number
  // Told whether the content is taller than maxHeight.
  onOverflow?: (overflowing: boolean) => void
  // The content asked to put text in the person's message box.
  onProposeMessage?: (text: string) => void
  className?: string
}

const MIN_HEIGHT = 32
const DEFAULT_MAX_HEIGHT = 480

function webLink(value: unknown): string | null {
  if (typeof value !== "string") return null
  try {
    const url = new URL(value)
    return url.protocol === "https:" || url.protocol === "http:" ? url.href : null
  } catch {
    return null
  }
}

// SandboxedHtml draws HTML an agent wrote. The frame may run scripts and
// nothing else: without allow-same-origin its content has an origin of its
// own, so it cannot read Legato's storage or call Legato's API, and a content
// security policy inside the document allows it no network beyond loading
// from the allowed hosts. What it can do is ask, by posting a message; the
// three requests answered here are its height, a link to open and text for
// the person's message box. Only messages from this frame's own window count.
export function SandboxedHtml({ html, title, resourceDomains, fill = false, maxHeight = DEFAULT_MAX_HEIGHT, onOverflow, onProposeMessage, className }: SandboxedHtmlProps) {
  const frameRef = useRef<HTMLIFrameElement>(null)
  const [height, setHeight] = useState<number | null>(null)
  const frameDocument = useMemo(() => buildSandboxDocument({ html, resourceDomains, fill }), [html, resourceDomains, fill])
  const handlers = useRef({ onOverflow, onProposeMessage })
  useEffect(() => {
    handlers.current = { onOverflow, onProposeMessage }
  })

  useEffect(() => {
    function onMessage(event: MessageEvent) {
      const frame = frameRef.current
      if (!frame || event.source === null || event.source !== frame.contentWindow) return
      const data = event.data as { jsonrpc?: unknown; method?: unknown; params?: unknown } | null
      if (!data || data.jsonrpc !== "2.0" || typeof data.method !== "string") return
      const params = (data.params ?? {}) as Record<string, unknown>
      switch (data.method) {
        case "ui/notifications/size-changed": {
          if (fill || typeof params.height !== "number" || !Number.isFinite(params.height)) return
          const wanted = Math.max(MIN_HEIGHT, Math.ceil(params.height))
          setHeight(Math.min(wanted, maxHeight))
          handlers.current.onOverflow?.(wanted > maxHeight)
          return
        }
        case "ui/open-link": {
          const url = webLink(params.url)
          // The frame cannot open a window. The person decides, and sees
          // where the link really goes.
          if (url && window.confirm(`Open this link from the agent's content?\n\n${url}`)) {
            window.open(url, "_blank", "noopener,noreferrer")
          }
          return
        }
        case "ui/message": {
          const text = proposedMessageText(params)
          if (text) handlers.current.onProposeMessage?.(text)
          return
        }
      }
    }
    window.addEventListener("message", onMessage)
    return () => window.removeEventListener("message", onMessage)
  }, [fill, maxHeight])

  return (
    <div className={cn("relative min-w-0", fill && "flex min-h-0 flex-1 flex-col", className)}>
      <iframe
        ref={frameRef}
        title={title}
        sandbox="allow-scripts"
        referrerPolicy="no-referrer"
        srcDoc={frameDocument}
        className={cn("block w-full border-0 bg-zinc-900", fill ? "h-full min-h-0 flex-1" : "rounded-md")}
        style={fill ? undefined : { height: height === null ? undefined : `${height}px` }}
      />
      <span className="pointer-events-none absolute bottom-1 right-2 rounded bg-zinc-950/80 px-1.5 py-0.5 text-[10px] text-zinc-500">
        Made by the agent
      </span>
    </div>
  )
}
