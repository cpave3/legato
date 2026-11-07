import { useEffect, useRef, useState } from "react"
import { Terminal } from "@xterm/xterm"
import { FitAddon } from "@xterm/addon-fit"
import { useServer } from "../hooks/useServer"
import { terminalUrl, touchWheel, wheelSequence } from "../lib/terminalSocket"
import { terminalTheme } from "../lib/terminalTheme"
import { cn } from "../lib/utils"
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
  // The theme is read once: Settings is another page, so a terminal opened
  // after a change takes it.
  const [theme] = useState(() => terminalTheme().theme)
  const [size, setSize] = useState({ cols: 0, rows: 0 })
  const [focused, setFocused] = useState(false)
  // When the next reconnect is due, while the connection is down.
  const [retryAt, setRetryAt] = useState<number | null>(null)
  const [now, setNow] = useState(() => Date.now())
  const retryNow = useRef<() => void>(() => {})

  useEffect(() => {
    const container = containerRef.current
    if (!container) return

    const term = new Terminal({
      theme,
      fontSize: 13,
      fontFamily: "'Geist Mono', ui-monospace, 'Cascadia Code', 'Source Code Pro', Menlo, Consolas, monospace",
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
      setSize({ cols: term.cols, rows: term.rows })
      if (ws?.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }))
      }
    }

    const connect = () => {
      if (retry) clearTimeout(retry)
      retry = null
      setRetryAt(null)
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
        const wait = Math.min(1000 * 2 ** attempt++, 10000)
        setRetryAt(Date.now() + wait)
        retry = setTimeout(connect, wait)
      }
    }
    retryNow.current = connect
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
  }, [agentId, baseUrl, theme])

  // Counts down to the next reconnect.
  useEffect(() => {
    if (retryAt === null) return
    setNow(Date.now())
    const tick = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(tick)
  }, [retryAt])

  const lost = status === "closed"
  const retryIn = retryAt === null ? 0 : Math.max(1, Math.ceil((retryAt - now) / 1000))

  return (
    <div className="absolute inset-0 flex bg-lw-ground p-4 max-sm:p-2">
      <section
        aria-label="Terminal"
        onFocus={() => setFocused(true)}
        onBlur={(event) => {
          if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setFocused(false)
        }}
        style={{ background: theme.background }}
        className={cn(
          "relative flex min-w-0 flex-1 flex-col overflow-hidden rounded-[10px] border shadow-[inset_0_2px_10px_rgba(0,0,0,0.65)] transition-[border-color,box-shadow] max-sm:rounded-lg",
          focused && !lost
            ? "border-lw-accent-ring shadow-[0_0_0_3px_rgba(109,94,246,0.14),inset_0_2px_10px_rgba(0,0,0,0.65)]"
            : "border-lw-line-strong",
        )}
      >
        <div className="flex h-8 shrink-0 items-center justify-between gap-3 border-b border-lw-raised bg-[#0e0e12] px-3 font-lw-mono text-[11px] text-lw-faint max-sm:h-[30px]">
          {lost ? (
            <span className="flex items-center gap-1.5 uppercase tracking-wide text-lw-needs">
              <span aria-hidden="true" className="size-1.5 rounded-full border-[1.5px] border-lw-needs" />
              Reconnecting
            </span>
          ) : (
            <span className="min-w-0 truncate">tmux · legato-{agentId}</span>
          )}
          <span className="shrink-0">
            {lost ? `next try in ${retryIn}s` : focused || size.cols === 0 ? `${size.cols}×${size.rows}` : (
              <><span className="max-sm:hidden">Click to type</span><span className="sm:hidden">Tap to type</span></>
            )}
          </span>
        </div>
        <div className={cn("min-h-0 flex-1 px-3.5 py-2.5 transition-opacity max-sm:px-2 max-sm:py-2", lost && "opacity-35")}>
          <div ref={containerRef} className="h-full w-full overflow-hidden" />
        </div>
        {lost && (
          <div className="absolute inset-x-0 bottom-0 top-8 flex items-center justify-center p-4">
            <div className="flex flex-wrap items-center justify-center gap-2.5 rounded-lg border border-lw-line-strong bg-lw-surface px-3 py-2 text-center font-lw text-[13px] text-lw-text-2">
              Lost the connection to tmux. The session keeps running.
              <button
                type="button"
                onClick={() => retryNow.current()}
                className="h-[26px] rounded-md border border-lw-line-strong bg-lw-raised px-2.5 text-xs text-lw-text hover:border-[#4a4955]"
              >
                Retry now
              </button>
            </div>
          </div>
        )}
      </section>
    </div>
  )
}
