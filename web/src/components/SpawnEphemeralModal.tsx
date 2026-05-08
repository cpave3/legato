import { useState, useEffect } from "react"
import { useServer } from "../hooks/useServer"
import { apiFetch } from "../lib/api"
import { X, Plus } from "lucide-react"

interface AdapterInfo {
  adapters: string[]
  default: string
}

interface SettingsInfo {
  ca_cert_available: boolean
  working_dir: string
}

export function SpawnEphemeralModal({
  open,
  onClose,
  onSpawned,
}: {
  open: boolean
  onClose: () => void
  onSpawned?: () => void
}) {
  const { baseUrl } = useServer()
  const [title, setTitle] = useState("Ephemeral session")
  const [agentKind, setAgentKind] = useState("")
  const [workingDir, setWorkingDir] = useState("")
  const [adapters, setAdapters] = useState<AdapterInfo | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState("")

  useEffect(() => {
    if (!open) return
    setError("")
    setLoading(false)
    setTitle("Ephemeral session")
    setAgentKind("")

    // Fetch adapters and default cwd.
    Promise.all([
      apiFetch(baseUrl, "/api/adapters"),
      apiFetch(baseUrl, "/api/settings"),
    ])
      .then(async ([adapterRes, settingsRes]) => {
        if (adapterRes.ok) {
          const data: AdapterInfo = await adapterRes.json()
          setAdapters(data)
          setAgentKind("") // default
        }
        if (settingsRes.ok) {
          const data: SettingsInfo = await settingsRes.json()
          setWorkingDir(data.working_dir || "")
        }
      })
      .catch(() => {
        // ignore fetch errors
      })
  }, [open, baseUrl])

  const agentOptions = [
    { label: adapters?.default ? `default (${adapters.default})` : "default", value: "" },
    { label: "shell", value: "shell" },
    ...(adapters?.adapters.filter((a) => a !== adapters.default) ?? []).map((a) => ({
      label: a,
      value: a,
    })),
  ]

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setLoading(true)
    setError("")
    try {
      const res = await apiFetch(baseUrl, "/api/agents/spawn", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          title: title || "Ephemeral session",
          agent_kind: agentKind,
          working_dir: workingDir,
        }),
      })
      if (!res.ok) {
        const text = await res.text()
        setError("Failed to spawn agent: " + text)
        return
      }
      onSpawned?.()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to spawn agent")
    } finally {
      setLoading(false)
    }
  }

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div className="w-full max-w-md rounded-lg border border-zinc-700 bg-zinc-900 shadow-2xl">
        <div className="flex items-center justify-between border-b border-zinc-800 px-5 py-3">
          <h2 className="text-sm font-semibold text-zinc-200">Spawn Ephemeral Agent</h2>
          <button
            onClick={onClose}
            className="rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
          >
            <X size={16} />
          </button>
        </div>

        <form onSubmit={handleSubmit} className="px-5 py-4 space-y-4">
          {error && (
            <div className="rounded border border-red-800 bg-red-950/50 px-3 py-2 text-xs text-red-300">
              {error}
            </div>
          )}

          <div className="space-y-1.5">
            <label className="text-xs font-medium text-zinc-400">Title</label>
            <input
              type="text"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="Ephemeral session"
              className="w-full rounded border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm text-zinc-200 outline-none focus:border-indigo-500"
            />
          </div>

          <div className="space-y-1.5">
            <label className="text-xs font-medium text-zinc-400">Agent</label>
            <select
              value={agentKind}
              onChange={(e) => setAgentKind(e.target.value)}
              className="w-full rounded border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm text-zinc-200 outline-none focus:border-indigo-500"
            >
              {agentOptions.map((opt) => (
                <option key={opt.value} value={opt.value}>
                  {opt.label}
                </option>
              ))}
            </select>
          </div>

          <div className="space-y-1.5">
            <label className="text-xs font-medium text-zinc-400">Working Directory</label>
            <input
              type="text"
              value={workingDir}
              onChange={(e) => setWorkingDir(e.target.value)}
              placeholder="/path/to/project"
              className="w-full rounded border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm text-zinc-200 outline-none focus:border-indigo-500 font-mono"
            />
          </div>

          <div className="flex justify-end">
            <button
              type="submit"
              disabled={loading}
              className="flex items-center gap-1.5 rounded bg-indigo-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-indigo-500 disabled:opacity-40"
            >
              <Plus size={14} />
              {loading ? "Spawning..." : "Spawn"}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
