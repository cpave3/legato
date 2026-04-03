import { useState, useEffect } from "react"
import { WifiOff } from "lucide-react"
import { useWebSocket } from "../hooks/useWebSocket"

export function OfflineOverlay() {
  const { connected } = useWebSocket()
  const [visible, setVisible] = useState(false)

  // Delay showing the overlay to avoid a flash on slow initial connections.
  useEffect(() => {
    if (connected) {
      setVisible(false)
      return
    }
    const timer = setTimeout(() => setVisible(true), 2000)
    return () => clearTimeout(timer)
  }, [connected])

  if (!visible) return null

  return (
    <div className="fixed inset-0 z-50 flex flex-col items-center justify-center bg-zinc-950/90 backdrop-blur-sm">
      <WifiOff className="h-12 w-12 text-zinc-500 mb-4" />
      <h2 className="text-xl font-semibold text-zinc-200 mb-2">
        Connection Lost
      </h2>
      <p className="text-zinc-400 text-sm text-center max-w-xs">
        Unable to reach the Legato server. Reconnecting automatically&hellip;
      </p>
      <div className="mt-6 h-1 w-32 overflow-hidden rounded-full bg-zinc-800">
        <div className="h-full w-1/3 animate-[slide_1.2s_linear_infinite] rounded-full bg-indigo-600" />
      </div>
      <button
        onClick={() => window.location.reload()}
        className="mt-4 rounded px-4 py-2 text-sm text-zinc-400 border border-zinc-700 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
      >
        Retry
      </button>
      <style>{`
        @keyframes slide {
          0% { transform: translateX(-100%); }
          100% { transform: translateX(400%); }
        }
      `}</style>
    </div>
  )
}
