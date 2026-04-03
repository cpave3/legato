import { useState, type KeyboardEvent } from "react"
import { KeyRound } from "lucide-react"
import { setToken, authHeaders } from "../lib/auth"

interface TokenPromptProps {
  onAuthenticated: () => void
}

export function TokenPrompt({ onAuthenticated }: TokenPromptProps) {
  const [input, setInput] = useState("")
  const [error, setError] = useState("")
  const [loading, setLoading] = useState(false)

  const handleSubmit = async () => {
    const trimmed = input.trim()
    if (!trimmed) return

    setLoading(true)
    setError("")

    // Temporarily store the token and test it against the API.
    setToken(trimmed)
    try {
      const res = await fetch("/api/settings", { headers: authHeaders() })
      if (res.status === 401) {
        setToken("") // clear invalid token
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

  const handleKeyDown = (e: KeyboardEvent) => {
    if (e.key === "Enter") {
      e.preventDefault()
      handleSubmit()
    }
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
        <button
          onClick={handleSubmit}
          disabled={!input.trim() || loading}
          className="w-full rounded bg-indigo-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-indigo-500 disabled:opacity-40"
        >
          {loading ? "Verifying..." : "Connect"}
        </button>
      </div>
    </div>
  )
}
