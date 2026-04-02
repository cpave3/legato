import { useEffect, useRef } from "react"
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
  const { send, subscribe } = useWebSocket()
  const agentIdRef = useRef(agentId)
  agentIdRef.current = agentId

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
    })
  }, [agentId, subscribe])

  // Clear terminal and re-send size when agent changes.
  useEffect(() => {
    const term = termRef.current
    if (term) {
      term.reset()
      term.write("\x1b[2m[connected]\x1b[0m\r\n")
      send({
        type: "resize",
        agent_id: agentId,
        cols: term.cols,
        rows: term.rows,
      })
    }
  }, [agentId, send])

  // Prevent pull-to-refresh on mobile; forward touch scrolls to xterm.
  useEffect(() => {
    const container = containerRef.current
    if (!container) return

    const onTouchMove = (e: TouchEvent) => {
      e.preventDefault()
    }

    // Must be non-passive to allow preventDefault.
    container.addEventListener("touchmove", onTouchMove, { passive: false })
    return () => container.removeEventListener("touchmove", onTouchMove)
  }, [])

  return (
    <div
      ref={containerRef}
      className="absolute inset-0 overflow-hidden bg-[#0a0a0f] touch-none"
    />
  )
}
