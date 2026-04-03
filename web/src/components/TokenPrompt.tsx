import { useState, type KeyboardEvent } from "react"
import { KeyRound, QrCode } from "lucide-react"
import { setToken, authHeaders } from "../lib/auth"
import { QRScanner } from "./QRScanner"

interface TokenPromptProps {
  onAuthenticated: () => void
}

export function TokenPrompt({ onAuthenticated }: TokenPromptProps) {
  const [input, setInput] = useState("")
  const [error, setError] = useState("")
  const [loading, setLoading] = useState(false)
  const [showScanner, setShowScanner] = useState(false)

  const verifyToken = async (token: string) => {
    setLoading(true)
    setError("")
    setToken(token)
    try {
      const res = await fetch("/api/settings", { headers: authHeaders() })
      if (res.status === 401) {
        setToken("")
        setError("Invalid token")
        setLoading(false)
        return
      }
      onAuthenticated()
    } catch {
      setError("Unable to reach server")
      setLoading(false)
    }
  }

  const handleSubmit = () => {
    const trimmed = input.trim()
    if (trimmed) verifyToken(trimmed)
  }

  const handleKeyDown = (e: KeyboardEvent) => {
    if (e.key === "Enter") {
      e.preventDefault()
      handleSubmit()
    }
  }

  const handleQRScan = (data: { url: string; token: string }) => {
    setShowScanner(false)
    verifyToken(data.token)
  }

  if (showScanner) {
    return <QRScanner onScan={handleQRScan} onClose={() => setShowScanner(false)} />
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-zinc-950/90 backdrop-blur-sm">
      <div className="w-full max-w-sm rounded-lg border border-zinc-800 bg-zinc-900 p-6">
        <div className="flex items-center gap-3 mb-4">
          <KeyRound size={24} className="text-indigo-400" />
          <h2 className="text-lg font-semibold text-zinc-100">Authentication Required</h2>
        </div>
        <p className="text-sm text-zinc-400 mb-4">
          Enter the access token to connect. Run <code className="text-zinc-300 bg-zinc-800 px-1 rounded">legato auth token</code> or <code className="text-zinc-300 bg-zinc-800 px-1 rounded">legato pair</code> on the server to get it.
        </p>
        <input
          type="password"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="Paste token..."
          autoFocus
          disabled={loading}
          className="w-full rounded bg-zinc-800 border border-zinc-700 px-3 py-2 text-sm text-zinc-200 placeholder:text-zinc-600 outline-none focus:border-indigo-500 disabled:opacity-50 mb-3"
        />
        {error && (
          <p className="text-sm text-red-400 mb-3">{error}</p>
        )}
        <div className="flex gap-2">
          <button
            onClick={handleSubmit}
            disabled={!input.trim() || loading}
            className="flex-1 rounded bg-indigo-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-indigo-500 disabled:opacity-40"
          >
            {loading ? "Verifying..." : "Connect"}
          </button>
          <button
            onClick={() => setShowScanner(true)}
            disabled={loading}
            className="flex items-center gap-1.5 rounded border border-zinc-700 px-3 py-2 text-sm text-zinc-400 transition-colors hover:bg-zinc-800 hover:text-zinc-200 disabled:opacity-40"
            title="Scan QR code from legato pair"
          >
            <QrCode size={16} />
            Scan
          </button>
        </div>
      </div>
    </div>
  )
}
