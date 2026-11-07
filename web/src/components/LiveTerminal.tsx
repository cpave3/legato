import { useEffect, useRef, useState } from "react"
import { Terminal } from "@xterm/xterm"
import { FitAddon } from "@xterm/addon-fit"
import { useServer } from "../hooks/useServer"
import { terminalUrl, touchWheel, wheelSequence } from "../lib/terminalSocket"
import "@xterm/xterm/css/xterm.css"

interface LiveTerminalProps {
  agentId: string
}

/**
 * A real terminal on the agent's tmux session: a tmux client on a pty on the
 * server, bytes both ways over one binary WebSocket. tmux draws the screen and
 * keeps the scrollback (the wheel scrolls it), so there is nothing to keep in
 * step here and every key, menu and Ctrl-key goes straight to the session.
 */
export function LiveTerminal({ agentId }: LiveTerminalProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const { baseUrl } = useServer()
  const [status, setStatus] = useState<"connecting" | "open" | "closed">("connecting")

  useEffect(() => {
    const container = containerRef.current
    if (!container) return

    const term = new Terminal({
      theme: { background: "#0a0a0f", foreground: "#e4e4e7", cursor: "#e4e4e7", selectionBackground: "#3f3f46" },
      fontSize: 13,
      fontFamily: "ui-monospace, 'Cascadia Code', 'Source Code Pro', Menlo, Consolas, monospace",
      cursorBlink: true,
      scrollback: 0, // tmux keeps the history
      allowProposedApi: true,
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(container)
    fit.fit()

    const encoder = new TextEncoder()
    let ws: WebSocket | null = null
    let disposed = false
    let attempt = 0
    let retry: ReturnType<typeof setTimeout> | null = null

    const sendResize = () => {
      if (ws?.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }))
      }
    }

    const connect = () => {
      setStatus("connecting")
      const socket = new WebSocket(terminalUrl(baseUrl, agentId, term.cols, term.rows))
      socket.binaryType = "arraybuffer"
      ws = socket
      socket.onopen = () => {
        attempt = 0
        setStatus("open")
        term.reset() // tmux redraws the whole screen on attach
        sendResize()
      }
      socket.onmessage = (e) => {
        if (e.data instanceof ArrayBuffer) term.write(new Uint8Array(e.data))
      }
      socket.onclose = () => {
        if (disposed || ws !== socket) return
        setStatus("closed")
        retry = setTimeout(connect, Math.min(1000 * 2 ** attempt++, 10000))
      }
    }
    connect()

    const send = (bytes: Uint8Array) => {
      if (ws?.readyState === WebSocket.OPEN) ws.send(bytes)
    }
    const data = term.onData((s) => send(encoder.encode(s)))
    const binary = term.onBinary((s) => send(Uint8Array.from(s, (c) => c.charCodeAt(0))))

    const observer = new ResizeObserver(() => {
      fit.fit()
      sendResize()
    })
    observer.observe(container)

    // Touch drags become wheel notches for tmux, which scrolls its own history.
    let lastY = 0
    let accumulated = 0
    const lineHeight = Math.max(10, container.clientHeight / Math.max(term.rows, 1))
    const onTouchStart = (e: TouchEvent) => {
      lastY = e.touches[0].clientY
      accumulated = 0
    }
    const onTouchMove = (e: TouchEvent) => {
      e.preventDefault()
      accumulated += e.touches[0].clientY - lastY
      lastY = e.touches[0].clientY
      const { notches, rest } = touchWheel(accumulated, lineHeight)
      accumulated = rest
      for (let i = 0; i < Math.abs(notches); i++) send(encoder.encode(wheelSequence(notches > 0)))
    }
    container.addEventListener("touchstart", onTouchStart, { passive: true })
    container.addEventListener("touchmove", onTouchMove, { passive: false })

    term.focus()

    return () => {
      disposed = true
      if (retry) clearTimeout(retry)
      container.removeEventListener("touchstart", onTouchStart)
      container.removeEventListener("touchmove", onTouchMove)
      observer.disconnect()
      data.dispose()
      binary.dispose()
      ws?.close()
      term.dispose()
    }
  }, [agentId, baseUrl])

  return (
    <div className="absolute inset-0 bg-[#0a0a0f]">
      <div ref={containerRef} className="absolute inset-0 overflow-hidden p-1" />
      {status !== "open" && (
        <div className="pointer-events-none absolute left-3 top-2 text-xs text-zinc-500">
          {status === "connecting" ? "connecting…" : "disconnected, retrying…"}
        </div>
      )}
    </div>
  )
}
