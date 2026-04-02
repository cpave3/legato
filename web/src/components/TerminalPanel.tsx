import { useEffect, useRef, useState } from "react"
import { ArrowDown } from "lucide-react"
import { Terminal } from "@xterm/xterm"
import { FitAddon } from "@xterm/addon-fit"
import { useWebSocket, type WSMessage } from "../hooks/useWebSocket"
import "@xterm/xterm/css/xterm.css"

interface TerminalPanelProps {
  agentId: string
}

export function TerminalPanel({ agentId }: TerminalPanelProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<Terminal | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const { send, subscribe, connected } = useWebSocket()
  const agentIdRef = useRef(agentId)
  agentIdRef.current = agentId
  const [isScrolledUp, setIsScrolledUp] = useState(false)

  useEffect(() => {
    const container = containerRef.current
    if (!container) return

    const term = new Terminal({
      theme: {
        background: "#0a0a0f",
        foreground: "#e4e4e7",
        cursor: "#e4e4e7",
        selectionBackground: "#3f3f46",
      },
      fontSize: 13,
      fontFamily: "ui-monospace, 'Cascadia Code', 'Source Code Pro', Menlo, Consolas, monospace",
      cursorBlink: false,
      disableStdin: true,
      scrollback: 10000,
    })

    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(container)
    fit.fit()

    termRef.current = term
    fitRef.current = fit

    // Track whether the user has scrolled up from the bottom.
    term.onScroll(() => {
      const buf = term.buffer.active
      setIsScrolledUp(buf.viewportY < buf.baseY)
    })

    const sendSize = () => {
      fit.fit()
      send({
        type: "resize",
        agent_id: agentIdRef.current,
        cols: term.cols,
        rows: term.rows,
      })
    }

    const resizeObserver = new ResizeObserver(sendSize)
    resizeObserver.observe(container)

    // Send initial size.
    sendSize()

    // Heartbeat keeps the TTL alive so the server doesn't expire
    // this client's size entry (e.g. phone screen locked).
    const heartbeat = setInterval(sendSize, 5000)

    return () => {
      clearInterval(heartbeat)
      resizeObserver.disconnect()
      term.dispose()
      termRef.current = null
      fitRef.current = null
    }
  }, [send])

  // Subscribe to agent output stream
  useEffect(() => {
    return subscribe((msg: WSMessage) => {
      if (msg.type !== "agent_output" || msg.agent_id !== agentId) return
      const term = termRef.current
      if (!term || !msg.content) return
      term.write(msg.content)
      // New output may have pushed baseY past viewportY.
      const buf = term.buffer.active
      setIsScrolledUp(buf.viewportY < buf.baseY)
    })
  }, [agentId, subscribe])

  // Clear terminal and re-send size when agent changes or WebSocket reconnects.
  // The resize message triggers startPipe on the server — without it, the pipe
  // never starts and no output flows.
  useEffect(() => {
    const term = termRef.current
    if (term && connected) {
      term.reset()
      setIsScrolledUp(false)
      term.write("\x1b[2m[connected]\x1b[0m\r\n")
      send({
        type: "resize",
        agent_id: agentId,
        cols: term.cols,
        rows: term.rows,
      })
    }
  }, [agentId, connected, send])

  // Custom touch-to-scroll: mobile browsers don't reliably deliver touch
  // events to xterm.js's internal viewport. We intercept touches on the
  // container, calculate swipe delta, and call term.scrollLines() directly.
  useEffect(() => {
    const container = containerRef.current
    if (!container) return

    let touchStartY = 0
    let accumulated = 0
    const lineHeight = 17 // approximate pixels per terminal line

    const onTouchStart = (e: TouchEvent) => {
      touchStartY = e.touches[0].clientY
      accumulated = 0
    }

    const onTouchMove = (e: TouchEvent) => {
      const term = termRef.current
      if (!term) return

      e.preventDefault() // prevent page scroll / pull-to-refresh

      const deltaY = touchStartY - e.touches[0].clientY
      touchStartY = e.touches[0].clientY
      accumulated += deltaY

      // Scroll whole lines once we've accumulated enough pixels.
      const lines = Math.trunc(accumulated / lineHeight)
      if (lines !== 0) {
        term.scrollLines(lines)
        accumulated -= lines * lineHeight
      }
    }

    container.addEventListener("touchstart", onTouchStart, { passive: true })
    container.addEventListener("touchmove", onTouchMove, { passive: false })

    return () => {
      container.removeEventListener("touchstart", onTouchStart)
      container.removeEventListener("touchmove", onTouchMove)
    }
  }, [])

  const scrollToBottom = () => {
    const term = termRef.current
    if (term) {
      term.scrollToBottom()
      setIsScrolledUp(false)
    }
  }

  return (
    <div className="absolute inset-0 bg-[#0a0a0f]">
      <div
        ref={containerRef}
        className="absolute inset-0 overflow-hidden"
      />
      {isScrolledUp && (
        <button
          onClick={scrollToBottom}
          className="absolute bottom-3 right-3 rounded-full bg-zinc-800 border border-zinc-700 p-2 text-zinc-400 shadow-lg transition-colors hover:bg-zinc-700 hover:text-zinc-200 active:bg-zinc-600"
          title="Scroll to bottom"
        >
          <ArrowDown size={18} />
        </button>
      )}
    </div>
  )
}
